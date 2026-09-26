package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zamomd/zamo/internal/sqlite"
)

// Restore refuses an archive it cannot vouch for, and says which way.
var (
	// ErrNotAnArchive reports a file that is not a Zamo backup at all.
	ErrNotAnArchive = errors.New("not a Zamo backup archive")
	// ErrCorrupt reports an archive whose database does not match the
	// checksum its own manifest carries.
	ErrCorrupt = errors.New("the backup archive is damaged")
	// ErrUnsupported reports an archive written by a later release: a
	// layout or a database schema this build cannot read.
	ErrUnsupported = errors.New("the backup archive comes from a newer version of Zamo")
)

// Restore replaces the database at dbPath with the one inside archive and
// reports where the base it replaced was kept.
//
// Nothing about the live base is touched until the archive has been read
// whole and checked, so a refusal leaves the base exactly as it was. The
// caller is responsible for holding off GUI, MCP and scheduled access for
// the duration: this replaces the file underneath anything still holding it
// open.
func Restore(ctx context.Context, archive, dbPath string) (safety string, err error) {
	m, err := verified(ctx, archive)
	if err != nil {
		return "", err
	}

	staged := dbPath + ".incoming"
	if err := extractDatabase(ctx, archive, staged, m.SHA256); err != nil {
		os.Remove(staged)
		return "", err
	}
	defer os.Remove(staged)

	safety, err = keepCurrent(dbPath)
	if err != nil {
		return "", err
	}

	if err := os.Rename(staged, dbPath); err != nil {
		return "", fmt.Errorf("put the restored base in place: %w", err)
	}
	// SQLite pairs a database with whatever -wal and -shm sit beside it, so
	// the log of the base just replaced would be replayed onto the restored
	// one. The archived database is a settled file and needs neither.
	if err := clearSidecars(dbPath); err != nil {
		return "", err
	}
	return safety, nil
}

// verified reads the manifest and decides whether this build may read the
// archive at all. It runs before anything is extracted, so a refusal costs
// no disk and touches no live file.
func verified(ctx context.Context, archive string) (Manifest, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotAnArchive, filepath.Base(archive))
	}
	defer r.Close()

	var m Manifest
	var found bool
	for _, f := range r.File {
		if f.Name != manifestEntry {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s", ErrNotAnArchive, filepath.Base(archive))
		}
		err = json.UnmarshalRead(rc, &m)
		rc.Close()
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: its manifest is unreadable", ErrNotAnArchive)
		}
		found = true
	}
	if !found || !hasEntry(r, databaseEntry) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotAnArchive, filepath.Base(archive))
	}
	if m.Format > Format {
		return Manifest{}, fmt.Errorf("%w (archive layout %d, this build reads %d)", ErrUnsupported, m.Format, Format)
	}
	// A database carrying a later schema was written by a later release, which
	// may have migrated it in ways this one cannot read.
	if current := sqlite.SchemaVersion(); m.Schema > current {
		return Manifest{}, fmt.Errorf("%w (database schema %d, this build reads %d)", ErrUnsupported, m.Schema, current)
	}
	return m, ctx.Err()
}

func hasEntry(r *zip.ReadCloser, name string) bool {
	for _, f := range r.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

// extractDatabase writes the archived database to dest and refuses it unless
// what landed hashes to what the manifest promised. Hashing what was written
// rather than what was read is what makes this a check on the restored file.
func extractDatabase(ctx context.Context, archive, dest, want string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotAnArchive, filepath.Base(archive))
	}
	defer r.Close()

	var src io.ReadCloser
	for _, f := range r.File {
		if f.Name == databaseEntry {
			if src, err = f.Open(); err != nil {
				return fmt.Errorf("%w: its database is unreadable", ErrCorrupt)
			}
			break
		}
	}
	if src == nil {
		return fmt.Errorf("%w: %s", ErrNotAnArchive, filepath.Base(archive))
	}
	defer src.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("stage the restored base: %w", err)
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), src)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("stage the restored base: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%w: the database inside does not match its checksum", ErrCorrupt)
	}
	return ctx.Err()
}

// keepCurrent moves the live base aside and reports where it went, so a
// restore that turns out to be the wrong one is not the end of the base it
// replaced. A missing base is not an error: restoring into a fresh install
// is a legitimate way to arrive at one.
func keepCurrent(dbPath string) (string, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	safety := fmt.Sprintf("%s.replaced-%s", dbPath, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(dbPath, safety); err != nil {
		return "", fmt.Errorf("keep a copy of the base being replaced: %w", err)
	}
	return safety, nil
}

func clearSidecars(dbPath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear %s: %w", filepath.Base(dbPath+suffix), err)
		}
	}
	return nil
}
