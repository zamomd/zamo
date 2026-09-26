package backup_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/backup"
)

// A restore cannot replace the database while this process — or the MCP
// server, which is a separate process entirely — still holds it open. So it
// is staged and applied at the next startup, before anything opens the base.

func TestStageRefusesABadArchiveImmediately(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "zamo.db")
	seed(ctx, t, dbPath, "the live claim")
	junk := filepath.Join(dir, "junk.zamo-backup")
	writeFile(t, junk, []byte("not a zip"))

	require.ErrorIs(t, backup.Stage(ctx, junk, dbPath), backup.ErrNotAnArchive,
		"a bad archive must be refused while the user is watching")
	assert.Empty(t, backup.StagedRestore(dbPath), "a refused archive was staged anyway")
}

func TestApplyStagedReplacesTheBaseAtStartup(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "zamo.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(t.TempDir(), "base.zamo-backup")
	createFrom(ctx, t, dbPath, archive)
	seed(ctx, t, dbPath, "the live claim")

	require.NoError(t, backup.Stage(ctx, archive, dbPath))
	// Staging alone must not touch the base: the app is still running on it.
	require.Equal(t, "the live claim", readClaim(ctx, t, dbPath), "base after staging")
	assert.Equal(t, archive, backup.StagedRestore(dbPath))

	safety, err := backup.ApplyStaged(ctx, dbPath)
	require.NoError(t, err)
	assert.Equal(t, "the archived claim", readClaim(ctx, t, dbPath), "base after ApplyStaged")
	assert.Equal(t, "the live claim", readClaim(ctx, t, safety), "safety copy")
	assert.Empty(t, backup.StagedRestore(dbPath), "the marker survived the restore, so the next startup would repeat it")
}

func TestApplyStagedIsANoOpWithoutAMarker(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "zamo.db")
	seed(ctx, t, dbPath, "the live claim")

	safety, err := backup.ApplyStaged(ctx, dbPath)
	require.NoError(t, err)
	assert.Empty(t, safety, "nothing kept when there was nothing to apply")
	assert.Equal(t, "the live claim", readClaim(ctx, t, dbPath))
}

// TestApplyStagedRefusesAnArchiveGoneBad covers the gap between staging and
// the restart: the file the marker points at can be edited, replaced or
// deleted in between, so it is checked again rather than trusted.
func TestApplyStagedRefusesAnArchiveGoneBad(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "zamo.db")

	seed(ctx, t, dbPath, "the archived claim")
	archive := filepath.Join(dir, "base.zamo-backup")
	createFrom(ctx, t, dbPath, archive)
	seed(ctx, t, dbPath, "the live claim")
	require.NoError(t, backup.Stage(ctx, archive, dbPath))

	writeFile(t, archive, []byte("truncated between staging and startup"))

	_, err := backup.ApplyStaged(ctx, dbPath)
	require.ErrorIs(t, err, backup.ErrNotAnArchive)
	assert.Equal(t, "the live claim", readClaim(ctx, t, dbPath), "a refused restore must leave the base alone")
	assert.Empty(t, backup.StagedRestore(dbPath), "the marker survived, so every later startup would fail the same way")
}

func TestStageIsUndoable(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "zamo.db")
	seed(ctx, t, dbPath, "the live claim")
	archive := filepath.Join(t.TempDir(), "base.zamo-backup")
	createFrom(ctx, t, dbPath, archive)

	require.NoError(t, backup.Stage(ctx, archive, dbPath))
	require.NoError(t, backup.CancelStaged(dbPath))
	assert.Empty(t, backup.StagedRestore(dbPath), "the marker survived CancelStaged")
	assert.FileExists(t, dbPath, "the live base is gone after cancelling")
}
