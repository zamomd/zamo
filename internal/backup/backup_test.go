package backup_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/backup"
	"github.com/cominddev/comind/internal/sqlite"
)

func TestCreateCarriesTheDatabaseAndAManifest(t *testing.T) {
	ctx := t.Context()
	db := liveBase(ctx, t)
	dest := filepath.Join(t.TempDir(), "base.comind-backup")

	if err := backup.Create(ctx, db, dest, "v0.1.0"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	m, dbBytes := readArchive(t, dest)
	if m.Schema != 1 {
		t.Errorf("manifest schema = %d, want the live PRAGMA user_version 1", m.Schema)
	}
	if m.App != "v0.1.0" {
		t.Errorf("manifest app version = %q, want v0.1.0", m.App)
	}
	if m.CreatedAt.IsZero() {
		t.Error("manifest CreatedAt is zero, want the moment the archive was written")
	}
	sum := sha256.Sum256(dbBytes)
	if got := hex.EncodeToString(sum[:]); got != m.SHA256 {
		t.Errorf("manifest sha256 = %q, want %q — the checksum must cover the stored database", m.SHA256, got)
	}

	restored := filepath.Join(t.TempDir(), "restored.db")
	writeFile(t, restored, dbBytes)
	reopened, err := sqlite.Open(ctx, restored)
	if err != nil {
		t.Fatalf("open the database out of the archive: %v", err)
	}
	defer reopened.Close()
	var content string
	if err := reopened.QueryRowContext(ctx, `SELECT content FROM blocks WHERE id = 'b1'`).Scan(&content); err != nil {
		t.Fatalf("read the block back: %v", err)
	}
	if content != "a claim" {
		t.Errorf("content = %q, want %q", content, "a claim")
	}
}

// liveBase opens a database the way the application does and leaves it open,
// so Create is exercised against a base under WAL with a connection on it
// rather than a quiescent file.
func liveBase(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatalf("open the live base: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content) VALUES ('b1', '', 1, 'a claim')`); err != nil {
		t.Fatalf("seed the live base: %v", err)
	}
	return db
}

func readArchive(t *testing.T, path string) (backup.Manifest, []byte) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}
	defer r.Close()

	var m backup.Manifest
	var dbBytes []byte
	for _, f := range r.File {
		b := readEntry(t, f)
		switch f.Name {
		case "manifest.json":
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("decode the manifest: %v", err)
			}
		case "comind.db":
			dbBytes = b
		}
	}
	if dbBytes == nil {
		t.Fatal("the archive carries no comind.db entry")
	}
	return m, dbBytes
}

func readEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open %s: %v", f.Name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", f.Name, err)
	}
	return b
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
