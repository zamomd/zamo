package backup_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/backup"
	"github.com/cominddev/comind/internal/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoreReplacesTheBaseAndKeepsTheOldOne(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(t.TempDir(), "base.comind-backup")
	createFrom(ctx, t, dbPath, archive)
	seed(ctx, t, dbPath, "the live claim")

	safety, err := backup.Restore(ctx, archive, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "the archived claim", readClaim(ctx, t, dbPath), "restored base")
	require.NotEmpty(t, safety, "Restore reported no safety copy, want the previous base kept")
	assert.Equal(t, "the live claim", readClaim(ctx, t, safety), "safety copy")
}

// TestRestoreClearsTheWriteAheadLog pins the failure that silently corrupts a
// restored base: SQLite pairs a database file with whatever -wal sits beside
// it, so a stale log left over from the replaced base would be replayed on top
// of the archived one.
func TestRestoreClearsTheWriteAheadLog(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(t.TempDir(), "base.comind-backup")
	createFrom(ctx, t, dbPath, archive)

	// A live base under WAL, left with its sidecars in place.
	db, err := sqlite.Open(ctx, dbPath)
	require.NoError(t, err)
	insert(ctx, t, db, "b2", "a claim only the log knows")
	require.NoError(t, db.Close())
	require.NoError(t, os.WriteFile(dbPath+"-wal", []byte("stale log"), 0o600))

	_, err = backup.Restore(ctx, archive, dbPath)
	require.NoError(t, err)
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		_, err := os.Stat(sidecar)
		assert.ErrorIs(t, err, os.ErrNotExist, "%s survived the restore", filepath.Base(sidecar))
	}
}

func TestRestoreRefusesAnUnusableArchive(t *testing.T) {
	ctx := t.Context()
	good := goodManifest()

	tests := []struct {
		name    string
		build   func(t *testing.T, path string)
		wantErr error
	}{
		{
			name:    "not an archive at all",
			build:   func(t *testing.T, path string) { writeFile(t, path, []byte("not a zip")) },
			wantErr: backup.ErrNotAnArchive,
		},
		{
			name: "no database inside",
			build: func(t *testing.T, path string) {
				buildArchive(t, path, good, nil)
			},
			wantErr: backup.ErrNotAnArchive,
		},
		{
			name: "the database does not match its checksum",
			build: func(t *testing.T, path string) {
				m := good
				m.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
				buildArchive(t, path, m, []byte("a database"))
			},
			wantErr: backup.ErrCorrupt,
		},
		{
			name: "a layout this release does not know",
			build: func(t *testing.T, path string) {
				m := good
				m.Format = backup.Format + 1
				buildArchive(t, path, m, []byte("a database"))
			},
			wantErr: backup.ErrUnsupported,
		},
		{
			name: "a schema newer than this release",
			build: func(t *testing.T, path string) {
				m := good
				m.Schema = 99
				buildArchive(t, path, m, []byte("a database"))
			},
			wantErr: backup.ErrUnsupported,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "comind.db")
			seed(ctx, t, dbPath, "the live claim")
			archive := filepath.Join(dir, "broken.comind-backup")
			tc.build(t, archive)

			_, err := backup.Restore(ctx, archive, dbPath)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, "the live claim", readClaim(ctx, t, dbPath), "a refused restore must not touch the live base")
		})
	}
}

func goodManifest() backup.Manifest {
	return backup.Manifest{
		Format:    backup.Format,
		App:       "v0.1.0",
		Schema:    1,
		CreatedAt: time.Now().UTC(),
		// sha256 of "a database"
		SHA256: "e152d5ae22936201ab8402b3f72750be9e967a7de108fcf5c77a8dbc0e43e6ee",
	}
}

// buildArchive writes an archive by hand so a test can state exactly which
// part of it is wrong. A nil database omits the entry.
func buildArchive(t *testing.T, path string, m backup.Manifest, database []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	z := zip.NewWriter(f)
	if database != nil {
		w, err := z.Create("comind.db")
		require.NoError(t, err)
		_, err = w.Write(database)
		require.NoError(t, err)
	}
	w, err := z.Create("manifest.json")
	require.NoError(t, err)
	require.NoError(t, json.MarshalWrite(w, m))
	require.NoError(t, z.Close())
}

func seed(ctx context.Context, t *testing.T, dbPath, content string) {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM blocks`)
	require.NoError(t, err)
	insert(ctx, t, db, "b1", content)
}

func insert(ctx context.Context, t *testing.T, db *sql.DB, id, content string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		 VALUES (?, NULL, 1, ?, '2026-08-12T00:00:00Z', '2026-08-12T00:00:00Z')`, id, content)
	require.NoError(t, err, "insert %s", id)
}

func createFrom(ctx context.Context, t *testing.T, dbPath, dest string) {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, backup.Create(ctx, db, dest, "v0.1.0"))
}

func readClaim(ctx context.Context, t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	require.NoError(t, err)
	defer db.Close()
	var content string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT content FROM blocks WHERE id = 'b1'`).Scan(&content),
		"read the claim from %s", dbPath)
	return content
}
