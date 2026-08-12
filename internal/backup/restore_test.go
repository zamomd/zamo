package backup_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/backup"
	"github.com/cominddev/comind/internal/sqlite"
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
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if got := readClaim(ctx, t, dbPath); got != "the archived claim" {
		t.Errorf("restored base holds %q, want the archived content", got)
	}
	if safety == "" {
		t.Fatal("Restore reported no safety copy, want the previous base kept")
	}
	if got := readClaim(ctx, t, safety); got != "the live claim" {
		t.Errorf("safety copy holds %q, want what the base held before the restore", got)
	}
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
	if err != nil {
		t.Fatalf("open the live base: %v", err)
	}
	insert(ctx, t, db, "b2", "a claim only the log knows")
	if err := db.Close(); err != nil {
		t.Fatalf("close the live base: %v", err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("stale log"), 0o600); err != nil {
		t.Fatalf("plant a stale log: %v", err)
	}

	if _, err := backup.Restore(ctx, archive, dbPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(sidecar); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived the restore, want it cleared", filepath.Base(sidecar))
		}
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

			if _, err := backup.Restore(ctx, archive, dbPath); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got := readClaim(ctx, t, dbPath); got != "the live claim" {
				t.Errorf("the live base reads %q — a refused restore must not touch it", got)
			}
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
	if err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	if database != nil {
		w, err := z.Create("comind.db")
		if err != nil {
			t.Fatalf("write the database entry: %v", err)
		}
		if _, err := w.Write(database); err != nil {
			t.Fatalf("write the database entry: %v", err)
		}
	}
	w, err := z.Create("manifest.json")
	if err != nil {
		t.Fatalf("write the manifest entry: %v", err)
	}
	if err := json.NewEncoder(w).Encode(m); err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}
	if err := z.Close(); err != nil {
		t.Fatalf("finish the archive: %v", err)
	}
}

func seed(ctx context.Context, t *testing.T, dbPath, content string) {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM blocks`); err != nil {
		t.Fatalf("clear the base: %v", err)
	}
	insert(ctx, t, db, "b1", content)
}

func insert(ctx context.Context, t *testing.T, db *sql.DB, id, content string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		 VALUES (?, NULL, 1, ?, '2026-08-12T00:00:00Z', '2026-08-12T00:00:00Z')`, id, content); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func createFrom(ctx context.Context, t *testing.T, dbPath, dest string) {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open the base to archive: %v", err)
	}
	defer db.Close()
	if err := backup.Create(ctx, db, dest, "v0.1.0"); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func readClaim(ctx context.Context, t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	var content string
	if err := db.QueryRowContext(ctx, `SELECT content FROM blocks WHERE id = 'b1'`).Scan(&content); err != nil {
		t.Fatalf("read the claim from %s: %v", dbPath, err)
	}
	return content
}
