package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedClock(when time.Time) Clock {
	return func() time.Time { return when }
}

func writeOneFile(dir, name, content string) func(ctx context.Context, d string) error {
	return func(_ context.Context, d string) error {
		return os.WriteFile(filepath.Join(d, name), []byte(content), 0o644)
	}
}

func TestStatusWithoutAManagedDirectorySet(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	base := NewBase(settingsPath, fixedClock(time.Now()))

	status, err := base.Status()

	require.NoError(t, err)
	assert.Empty(t, status.Dir)
	assert.Nil(t, status.LatestCommit)
}

func TestExportAndCommitRefusesWithoutAManagedDirectory(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	base := NewBase(settingsPath, fixedClock(time.Now()))

	_, err := base.ExportAndCommit(t.Context(), func(context.Context, string) error {
		t.Fatal("export must not run without a managed directory")
		return nil
	})

	assert.ErrorIs(t, err, ErrDirNotSet)
}

func TestExportAndCommitWritesAndCommitsIntoTheManagedDirectory(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	dir := filepath.Join(t.TempDir(), "snapshot")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	base := NewBase(settingsPath, fixedClock(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)))
	require.NoError(t, base.SetDirectory(dir))

	result, err := base.ExportAndCommit(t.Context(), writeOneFile(dir, "topic.md", "# Topic"))

	require.NoError(t, err)
	assert.True(t, result.Committed)
	require.FileExists(t, filepath.Join(dir, "topic.md"))
}

func TestStatusReportsTheLatestCommitAfterARestart(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	dir := filepath.Join(t.TempDir(), "snapshot")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	first := NewBase(settingsPath, fixedClock(time.Now()))
	require.NoError(t, first.SetDirectory(dir))
	result, err := first.ExportAndCommit(t.Context(), writeOneFile(dir, "topic.md", "# Topic"))
	require.NoError(t, err)
	require.True(t, result.Committed)

	// A fresh Base, as a restart would construct: nothing is cached between
	// the two, so this proves the commit is read back from the repository
	// itself, not from any in-memory state.
	second := NewBase(settingsPath, fixedClock(time.Now()))
	status, err := second.Status()

	require.NoError(t, err)
	assert.Equal(t, dir, status.Dir)
	require.NotNil(t, status.LatestCommit)
	assert.Equal(t, result.Commit.Hash, status.LatestCommit.Hash)
}

func TestSetCommitTemplateRejectsAnInvalidTemplate(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	base := NewBase(settingsPath, fixedClock(time.Now()))

	err := base.SetCommitTemplate("{{.Unclosed")

	require.Error(t, err)
	status, statusErr := base.Status()
	require.NoError(t, statusErr)
	assert.Equal(t, DefaultCommitTemplate, status.CommitTemplate, "a rejected template must not become the effective one")
}

func TestSetDirectoryRejectsAnEmptyPath(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.toml")
	base := NewBase(settingsPath, fixedClock(time.Now()))

	err := base.SetDirectory("")

	assert.ErrorIs(t, err, errEmptyDirectory)
}
