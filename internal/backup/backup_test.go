package backup_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/backup"
	"github.com/zamomd/zamo/internal/sqlite"
)

func TestCreateCarriesTheDatabaseAndAManifest(t *testing.T) {
	ctx := t.Context()
	db := liveBase(ctx, t)
	dest := filepath.Join(t.TempDir(), "base.zamo-backup")

	require.NoError(t, backup.Create(ctx, db, dest, "v0.1.0"))

	m, dbBytes := readArchive(t, dest)
	assert.Equal(t, 1, m.Schema, "want the live PRAGMA user_version")
	assert.Equal(t, "v0.1.0", m.App)
	assert.False(t, m.CreatedAt.IsZero(), "manifest CreatedAt is zero, want the moment the archive was written")
	sum := sha256.Sum256(dbBytes)
	assert.Equal(t, hex.EncodeToString(sum[:]), m.SHA256, "the checksum must cover the stored database")

	restored := filepath.Join(t.TempDir(), "restored.db")
	writeFile(t, restored, dbBytes)
	reopened, err := sqlite.Open(ctx, restored)
	require.NoError(t, err, "open the database out of the archive")
	defer reopened.Close()
	var content string
	require.NoError(t, reopened.QueryRowContext(ctx, `SELECT content FROM blocks WHERE id = 'b1'`).Scan(&content))
	assert.Equal(t, "a claim", content)
}

// liveBase opens a database the way the application does and leaves it open,
// so Create is exercised against a base under WAL with a connection on it
// rather than a quiescent file.
func liveBase(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		 VALUES ('b1', NULL, 1, 'a claim', '2026-08-12T00:00:00Z', '2026-08-12T00:00:00Z')`)
	require.NoError(t, err, "seed the live base")
	return db
}

func readArchive(t *testing.T, path string) (backup.Manifest, []byte) {
	t.Helper()
	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()

	var m backup.Manifest
	var dbBytes []byte
	for _, f := range r.File {
		b := readEntry(t, f)
		switch f.Name {
		case "manifest.json":
			require.NoError(t, json.Unmarshal(b, &m), "decode the manifest")
		case "zamo.db":
			dbBytes = b
		}
	}
	require.NotNil(t, dbBytes, "the archive carries no zamo.db entry")
	return m, dbBytes
}

func readEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	require.NoError(t, err, f.Name)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	require.NoError(t, err, f.Name)
	return b
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, b, 0o600))
}
