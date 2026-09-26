// Package backup captures and restores the complete Zamo base — the
// stable ids, revisions, review state and references that portable Markdown
// export deliberately leaves behind.
package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Format is the archive layout this package writes. A reader refuses a
// higher number rather than guessing at a layout it does not know.
const Format = 1

const (
	manifestEntry = "manifest.json"
	databaseEntry = "zamo.db"
)

// Manifest describes one archive: what wrote it, what schema the database
// inside carries, and a checksum of that database.
type Manifest struct {
	Format    int       `json:"format"`
	App       string    `json:"app"`
	Schema    int       `json:"schema"`
	CreatedAt time.Time `json:"created_at"`
	SHA256    string    `json:"sha256"`
}

// Create writes a snapshot of db to dest. The snapshot is taken with
// VACUUM INTO, which reads the base inside a transaction: readers in this
// process and in the MCP server keep working, and the result is one settled
// file with no WAL sidecar to carry alongside it.
//
// dest appears complete or not at all — the archive is built beside it and
// renamed into place.
func Create(ctx context.Context, db *sql.DB, dest string, appVersion string) error {
	schema, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}

	staging, err := os.MkdirTemp(filepath.Dir(dest), ".zamo-backup-")
	if err != nil {
		return fmt.Errorf("stage the snapshot: %w", err)
	}
	defer os.RemoveAll(staging)

	// VACUUM INTO refuses an existing file, so it names one inside the
	// empty staging directory rather than a path it would have to clear.
	snapshot := filepath.Join(staging, databaseEntry)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return fmt.Errorf("snapshot the database: %w", err)
	}

	part := dest + ".part"
	if err := writeArchive(part, snapshot, Manifest{
		Format:    Format,
		App:       appVersion,
		Schema:    schema,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return fmt.Errorf("place the archive: %w", err)
	}
	return nil
}

// writeArchive packs snapshot and its manifest into path. The database is
// streamed rather than read into memory, and hashed in that same pass, which
// is why the manifest is written last.
func writeArchive(path, snapshot string, m Manifest) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create the archive: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close the archive: %w", cerr)
		}
	}()

	z := zip.NewWriter(f)
	sum, err := copyDatabase(z, snapshot)
	if err != nil {
		return err
	}
	m.SHA256 = sum

	w, err := z.Create(manifestEntry)
	if err != nil {
		return fmt.Errorf("write the manifest: %w", err)
	}
	if err := json.MarshalWrite(w, m); err != nil {
		return fmt.Errorf("write the manifest: %w", err)
	}
	if err := z.Close(); err != nil {
		return fmt.Errorf("finish the archive: %w", err)
	}
	// The archive is worthless if it is still in the page cache when the
	// machine loses power, and a backup exists precisely for that case.
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flush the archive: %w", err)
	}
	return nil
}

// copyDatabase streams snapshot into the archive and reports its checksum.
func copyDatabase(z *zip.Writer, snapshot string) (string, error) {
	src, err := os.Open(snapshot)
	if err != nil {
		return "", fmt.Errorf("read the snapshot: %w", err)
	}
	defer src.Close()

	w, err := z.Create(databaseEntry)
	if err != nil {
		return "", fmt.Errorf("write the database: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), src); err != nil {
		return "", fmt.Errorf("write the database: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read the schema version: %w", err)
	}
	return v, nil
}
