package backup_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/backup"
)

// A restore cannot replace the database while this process — or the MCP
// server, which is a separate process entirely — still holds it open. So it
// is staged and applied at the next startup, before anything opens the base.

func TestStageRefusesABadArchiveImmediately(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")
	seed(ctx, t, dbPath, "the live claim")
	junk := filepath.Join(dir, "junk.comind-backup")
	writeFile(t, junk, []byte("not a zip"))

	if err := backup.Stage(ctx, junk, dbPath); !errors.Is(err, backup.ErrNotAnArchive) {
		t.Fatalf("error = %v, want ErrNotAnArchive — a bad archive must be refused while the user is watching", err)
	}
	if backup.StagedRestore(dbPath) != "" {
		t.Error("a refused archive was staged anyway")
	}
}

func TestApplyStagedReplacesTheBaseAtStartup(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(t.TempDir(), "base.comind-backup")
	createFrom(ctx, t, dbPath, archive)
	seed(ctx, t, dbPath, "the live claim")

	if err := backup.Stage(ctx, archive, dbPath); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// Staging alone must not touch the base: the app is still running on it.
	if got := readClaim(ctx, t, dbPath); got != "the live claim" {
		t.Fatalf("base reads %q after staging, want it untouched until startup", got)
	}
	if backup.StagedRestore(dbPath) != archive {
		t.Errorf("StagedRestore = %q, want %q", backup.StagedRestore(dbPath), archive)
	}

	safety, err := backup.ApplyStaged(ctx, dbPath)
	if err != nil {
		t.Fatalf("ApplyStaged: %v", err)
	}
	if got := readClaim(ctx, t, dbPath); got != "the archived claim" {
		t.Errorf("base reads %q after ApplyStaged, want the archived content", got)
	}
	if got := readClaim(ctx, t, safety); got != "the live claim" {
		t.Errorf("safety copy reads %q, want what the base held before", got)
	}
	if backup.StagedRestore(dbPath) != "" {
		t.Error("the marker survived the restore, so the next startup would repeat it")
	}
}

func TestApplyStagedIsANoOpWithoutAMarker(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "comind.db")
	seed(ctx, t, dbPath, "the live claim")

	safety, err := backup.ApplyStaged(ctx, dbPath)
	if err != nil {
		t.Fatalf("ApplyStaged on an ordinary startup: %v", err)
	}
	if safety != "" {
		t.Errorf("safety = %q, want nothing kept when there was nothing to apply", safety)
	}
	if got := readClaim(ctx, t, dbPath); got != "the live claim" {
		t.Errorf("base reads %q, want it untouched", got)
	}
}

// TestApplyStagedRefusesAnArchiveGoneBad covers the gap between staging and
// the restart: the file the marker points at can be edited, replaced or
// deleted in between, so it is checked again rather than trusted.
func TestApplyStagedRefusesAnArchiveGoneBad(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(dir, "base.comind-backup")
	createFrom(ctx, t, dbPath, archive)
	seed(ctx, t, dbPath, "the live claim")
	if err := backup.Stage(ctx, archive, dbPath); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	writeFile(t, archive, []byte("truncated between staging and startup"))

	if _, err := backup.ApplyStaged(ctx, dbPath); !errors.Is(err, backup.ErrNotAnArchive) {
		t.Fatalf("error = %v, want ErrNotAnArchive", err)
	}
	if got := readClaim(ctx, t, dbPath); got != "the live claim" {
		t.Errorf("base reads %q — a refused restore must leave it alone", got)
	}
	if backup.StagedRestore(dbPath) != "" {
		t.Error("the marker survived, so every later startup would fail the same way")
	}
}

func TestStageIsUndoable(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comind.db")
	seed(ctx, t, dbPath, "the live claim")
	archive := filepath.Join(t.TempDir(), "base.comind-backup")
	createFrom(ctx, t, dbPath, archive)

	if err := backup.Stage(ctx, archive, dbPath); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if err := backup.CancelStaged(dbPath); err != nil {
		t.Fatalf("CancelStaged: %v", err)
	}
	if backup.StagedRestore(dbPath) != "" {
		t.Error("the marker survived CancelStaged")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("the live base is gone after cancelling: %v", err)
	}
}
