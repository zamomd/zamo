package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/host"
)

// isolate keeps the machine's own git configuration out of a test, leaving
// git with no user unless gitconfig names one.
func isolate(t *testing.T, gitconfig string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	writeFile(t, home, ".gitconfig", gitconfig)
}

const ada = "[user]\n\tname = Ada\n\temail = ada@example.com\n"

// tempDir is a temporary directory by the path git reports it under.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// gitOut runs git in dir the way the owner would, outside the adapter.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// seeded is a repository with one commit holding existing.txt.
func seeded(t *testing.T) string {
	t.Helper()
	root := tempDir(t)
	gitOut(t, root, "init", "--quiet")
	writeFile(t, root, "existing.txt", "already committed")
	gitOut(t, root, "add", "existing.txt")
	gitOut(t, root, "commit", "--quiet", "--message", "seed")
	return root
}

func exportDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "zamo-export")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, dir, "topic.md", "# Topic")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func TestRoot(t *testing.T) {
	isolate(t, ada)

	t.Run("no repository owns the directory", func(t *testing.T) {
		dir := tempDir(t)

		root, err := New(time.Now).Root(t.Context(), dir)

		require.NoError(t, err)
		assert.Empty(t, root)
		assert.NoDirExists(t, filepath.Join(dir, ".git"), "asking must not start a repository")
	})

	t.Run("the directory is inside a repository", func(t *testing.T) {
		repo := seeded(t)

		root, err := New(time.Now).Root(t.Context(), exportDir(t, repo))

		require.NoError(t, err)
		assert.Equal(t, repo, root)
	})
}

func TestCommitStartsARepositoryWhereThereIsNone(t *testing.T) {
	isolate(t, ada)
	dir := tempDir(t)
	writeFile(t, dir, "topic.md", "# Topic")

	got, err := New(time.Now).Commit(t.Context(), dir, "export")

	require.NoError(t, err)
	assert.Equal(t, got.Hash, gitOut(t, dir, "rev-parse", "HEAD")[:40])
	assert.Equal(t, "topic.md\n", gitOut(t, dir, "ls-tree", "-r", "--name-only", "HEAD"))
}

func TestCommitTouchesOnlyTheDirectory(t *testing.T) {
	isolate(t, ada)
	repo := seeded(t)
	writeFile(t, repo, "existing.txt", "edited by the owner")
	writeFile(t, repo, "theirs.txt", "staged by the owner")
	gitOut(t, repo, "add", "theirs.txt")
	dir := exportDir(t, repo)

	got, err := New(time.Now).Commit(t.Context(), dir, "export")

	require.NoError(t, err)
	require.NotEmpty(t, got.Hash)
	assert.Equal(t, "zamo-export/topic.md\n", gitOut(t, repo, "show", "--format=", "--name-only", "HEAD"))
	assert.Equal(t, " M existing.txt\nA  theirs.txt\n", gitOut(t, repo, "status", "--short"),
		"the owner's own changes must stay as they were, staged or not")
}

func TestCommitIsANoOpWhenNothingChanged(t *testing.T) {
	isolate(t, ada)
	dir := exportDir(t, seeded(t))
	history := New(time.Now)
	first, err := history.Commit(t.Context(), dir, "first")
	require.NoError(t, err)
	require.NotEmpty(t, first.Hash)

	second, err := history.Commit(t.Context(), dir, "second")

	require.NoError(t, err)
	assert.Zero(t, second)
	assert.Equal(t, first.Hash, gitOut(t, dir, "rev-parse", "HEAD")[:40])
}

func TestCommitRecordsARemovedFile(t *testing.T) {
	isolate(t, ada)
	dir := exportDir(t, seeded(t))
	writeFile(t, dir, "gone.md", "# Gone")
	history := New(time.Now)
	_, err := history.Commit(t.Context(), dir, "first")
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	second, err := history.Commit(t.Context(), dir, "second")

	require.NoError(t, err)
	require.NotEmpty(t, second.Hash)
	assert.Equal(t, "topic.md\n", gitOut(t, dir, "ls-tree", "--name-only", "HEAD", "./"))
}

func TestCommitThatFailsLeavesNothingStaged(t *testing.T) {
	isolate(t, ada)
	repo := seeded(t)
	dir := exportDir(t, repo)
	head := gitOut(t, repo, "rev-parse", "HEAD")
	// A stale lock on the branch lets staging through and stops the commit.
	branch := gitOut(t, repo, "symbolic-ref", "HEAD")
	lock := filepath.Join(repo, ".git", branch[:len(branch)-1]+".lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o644))

	_, err := New(time.Now).Commit(t.Context(), dir, "export")

	require.Error(t, err)
	require.NoError(t, os.Remove(lock))
	assert.Equal(t, head, gitOut(t, repo, "rev-parse", "HEAD"))
	assert.Equal(t, "?? zamo-export/\n", gitOut(t, repo, "status", "--short"))
}

func TestCommitRefusesADirectoryTheRepositoryIgnores(t *testing.T) {
	isolate(t, ada)
	repo := seeded(t)
	writeFile(t, repo, ".gitignore", "zamo-export/\n")
	dir := exportDir(t, repo)

	_, err := New(time.Now).Commit(t.Context(), dir, "export")

	assert.ErrorContains(t, err, "ignores")
}

func TestCommitWorksInALinkedWorktree(t *testing.T) {
	isolate(t, ada)
	repo := seeded(t)
	linked := filepath.Join(tempDir(t), "linked")
	gitOut(t, repo, "worktree", "add", "--quiet", "-b", "side", linked)
	dir := exportDir(t, linked)

	got, err := New(time.Now).Commit(t.Context(), dir, "export")

	require.NoError(t, err)
	assert.Equal(t, got.Hash, gitOut(t, repo, "rev-parse", "side")[:40])
	assert.NoFileExists(t, filepath.Join(dir, ".git"))
}

func TestCommitIgnoresARepositoryNamedByTheEnvironment(t *testing.T) {
	isolate(t, ada)
	elsewhere := seeded(t)
	head := gitOut(t, elsewhere, "rev-parse", "HEAD")
	dir := tempDir(t)
	writeFile(t, dir, "topic.md", "# Topic")
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(elsewhere, ".git", "index"))

	got, err := New(time.Now).Commit(t.Context(), dir, "export")

	require.NoError(t, err)
	require.NoError(t, os.Unsetenv("GIT_DIR"))
	require.NoError(t, os.Unsetenv("GIT_INDEX_FILE"))
	assert.Equal(t, head, gitOut(t, elsewhere, "rev-parse", "HEAD"))
	assert.Equal(t, got.Hash, gitOut(t, dir, "rev-parse", "HEAD")[:40])
}

func TestCommitIsStampedWithTheClockAndSignedByTheConfiguredUser(t *testing.T) {
	when := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		gitconfig string
		want      string
	}{
		{name: "no git user", want: "Zamo <zamo@localhost> 2026-10-05T12:00:00Z 2026-10-05T12:00:00Z\n"},
		{
			name: "a global git user", gitconfig: ada,
			want: "Ada <ada@example.com> 2026-10-05T12:00:00Z 2026-10-05T12:00:00Z\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t, tc.gitconfig)
			dir := tempDir(t)
			writeFile(t, dir, "topic.md", "# Topic")

			got, err := New(func() time.Time { return when }).Commit(t.Context(), dir, "# export")

			require.NoError(t, err)
			assert.Equal(t, "# export", got.Message)
			assert.Equal(t, when, got.At)
			t.Setenv("TZ", "UTC")
			assert.Equal(t, tc.want, gitOut(t, dir, "log", "-1", "--date=iso-strict-local", "--format=%an <%ae> %ad %cd"))
			assert.Equal(t, "# export\n", gitOut(t, dir, "log", "-1", "--format=%B")[:9])
		})
	}
}

func TestCommitWithoutGitSaysSo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := New(time.Now).Commit(t.Context(), t.TempDir(), "export")

	assert.ErrorIs(t, err, host.ErrNoGit)
}
