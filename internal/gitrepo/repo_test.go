package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenOrInitRepoResolvesTheRepositoryAndScopesToTheExportDirectory(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, root string) (dir, wantRoot, wantRel string)
		wantCreated bool
	}{
		{
			name: "creates a repository when none exists in the directory's ancestry",
			setup: func(t *testing.T, root string) (string, string, string) {
				dir := filepath.Join(root, "export")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, dir, "."
			},
			wantCreated: true,
		},
		{
			name: "uses the existing repository rooted at the chosen directory",
			setup: func(t *testing.T, root string) (string, string, string) {
				dir := filepath.Join(root, "vault")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				_, err := git.PlainInit(dir, false)
				require.NoError(t, err)
				return dir, dir, "."
			},
		},
		{
			name: "uses the existing repository when the chosen directory is a subdirectory",
			setup: func(t *testing.T, root string) (string, string, string) {
				repoRoot := filepath.Join(root, "vault")
				_, err := git.PlainInit(repoRoot, false)
				require.NoError(t, err)
				dir := filepath.Join(repoRoot, "zamo-export")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, repoRoot, "zamo-export"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir, wantRoot, wantRel := tc.setup(t, root)

			repo, relDir, err := openOrInitRepo(dir)

			require.NoError(t, err)
			assert.Equal(t, wantRel, relDir)
			wt, err := repo.Worktree()
			require.NoError(t, err)
			resolvedWantRoot, err := filepath.EvalSymlinks(wantRoot)
			require.NoError(t, err)
			assert.Equal(t, resolvedWantRoot, wt.Filesystem.Root())
		})
	}
}

func TestCommitStagesOnlyTheExportDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	repo, err := git.PlainInit(repoRoot, false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)

	// Seed the repository with a settled first commit so the unrelated file
	// below is a genuine working-tree change, not an untracked stray.
	writeFile(t, repoRoot, "existing.txt", "already committed")
	_, err = wt.Add("existing.txt")
	require.NoError(t, err)
	_, err = wt.Commit("seed", &git.CommitOptions{Author: &fallbackIdentity})
	require.NoError(t, err)

	// An unrelated change elsewhere in the same repository, made by the
	// user's own separate git usage — this must survive untouched.
	writeFile(t, repoRoot, "existing.txt", "edited outside the dir directory")

	dir := filepath.Join(repoRoot, "zamo-export")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, dir, "topic.md", "# Topic")

	_, relDir, err := openOrInitRepo(dir)
	require.NoError(t, err)
	result, err := commit(repo, relDir, "snapshot", time.Now())
	require.NoError(t, err)

	require.NotEmpty(t, result.Hash)

	head, err := repo.Head()
	require.NoError(t, err)
	commitObj, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	assertOnlyPathCommitted(t, commitObj, "zamo-export/topic.md")
	assertFileContent(t, commitObj, "existing.txt", "already committed")

	status, err := wt.Status()
	require.NoError(t, err)
	assert.Equal(t, git.Modified, status.File("existing.txt").Worktree,
		"the unrelated edit must stay an uncommitted working-tree change")
}

func TestCommitIsANoOpWhenNothingChangedUnderTheExportDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "export")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, dir, "topic.md", "# Topic")

	repo, relDir, err := openOrInitRepo(dir)
	require.NoError(t, err)
	first, err := commit(repo, relDir, "first", time.Now())
	require.NoError(t, err)
	require.NotEmpty(t, first.Hash)

	second, err := commit(repo, relDir, "second", time.Now())
	require.NoError(t, err)

	assert.Zero(t, second)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, first.Hash, head.Hash().String(), "a no-op run must not move HEAD")
}

// TestCommitLeavesTheRepositoryUnchangedWhenStagingFails forces the
// underlying object write to fail (a read-only objects directory) and
// checks the repository comes back exactly as it went in: no new commit,
// the on-disk index untouched, because a failing Add never reaches the
// SetIndex call that would persist it, and the same restore branch also
// covers a Commit failure after a successful Add.
func TestCommitLeavesTheRepositoryUnchangedWhenStagingFails(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "export")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, dir, "topic.md", "# Topic")

	repo, relDir, err := openOrInitRepo(dir)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)
	objects := filepath.Join(wt.Filesystem.Root(), ".git", "objects")
	require.NoError(t, os.Chmod(objects, 0o555))
	t.Cleanup(func() { _ = os.Chmod(objects, 0o755) })

	_, err = commit(repo, relDir, "snapshot", time.Now())
	require.Error(t, err)

	_, err = repo.Head()
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound, "no commit means no HEAD yet")
	idx, err := repo.Storer.Index()
	require.NoError(t, err)
	assert.Empty(t, idx.Entries, "a failed Add must never reach SetIndex")
}

func TestCommitRefusesWhenSomethingElseIsAlreadyStaged(t *testing.T) {
	repoRoot := t.TempDir()
	repo, err := git.PlainInit(repoRoot, false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	writeFile(t, repoRoot, "theirs.txt", "staged by the owner")
	_, err = wt.Add("theirs.txt")
	require.NoError(t, err)

	dir := filepath.Join(repoRoot, "zamo-export")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, dir, "topic.md", "# Topic")

	_, err = New(time.Now).Commit(t.Context(), dir, "export")

	require.ErrorContains(t, err, "already staged")
	_, err = repo.Head()
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestCommitRecordsARemovedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "kept.md", "# Kept")
	writeFile(t, dir, "gone.md", "# Gone")
	history := New(time.Now)
	_, err := history.Commit(t.Context(), dir, "first")
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	second, err := history.Commit(t.Context(), dir, "second")

	require.NoError(t, err)
	require.NotEmpty(t, second.Hash)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	c, err := repo.CommitObject(plumbing.NewHash(second.Hash))
	require.NoError(t, err)
	_, err = c.File("gone.md")
	require.ErrorIs(t, err, object.ErrFileNotFound)
	_, err = c.File("kept.md")
	assert.NoError(t, err)
}

func TestCommitIsStampedWithTheClockAndSignedByTheConfiguredUser(t *testing.T) {
	when := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		gitconfig string
		wantName  string
		wantEmail string
	}{
		{name: "no git user", wantName: "Zamo", wantEmail: "zamo@localhost"},
		{
			name:      "a global git user",
			gitconfig: "[user]\n\tname = Ada\n\temail = ada@example.com\n",
			wantName:  "Ada", wantEmail: "ada@example.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			if tc.gitconfig != "" {
				writeFile(t, home, ".gitconfig", tc.gitconfig)
			}
			dir := t.TempDir()
			writeFile(t, dir, "topic.md", "# Topic")

			got, err := New(func() time.Time { return when }).Commit(t.Context(), dir, "export")

			require.NoError(t, err)
			assert.Equal(t, "export", got.Message)
			assert.Equal(t, when, got.At)
			repo, err := git.PlainOpen(dir)
			require.NoError(t, err)
			c, err := repo.CommitObject(plumbing.NewHash(got.Hash))
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, c.Author.Name)
			assert.Equal(t, tc.wantEmail, c.Author.Email)
			assert.True(t, when.Equal(c.Author.When))
		})
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func assertFileContent(t *testing.T, c *object.Commit, path, want string) {
	t.Helper()
	f, err := c.File(path)
	require.NoError(t, err)
	got, err := f.Contents()
	require.NoError(t, err)
	assert.Equal(t, want, got, "the committed blob for %s must be the pre-edit version: the edit was never staged", path)
}

func assertOnlyPathCommitted(t *testing.T, c *object.Commit, want string) {
	t.Helper()
	tree, err := c.Tree()
	require.NoError(t, err)
	var got []string
	require.NoError(t, tree.Files().ForEach(func(f *object.File) error {
		got = append(got, f.Name)
		return nil
	}))
	assert.Contains(t, got, want)
	for _, name := range got {
		assert.True(t, name == want || name == "existing.txt", "unexpected file in tree: %s", name)
	}
}
