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

func TestOpenOrInitRepoResolvesTheRepositoryAndScopesToTheManagedDirectory(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, root string) (managed, wantRoot, wantRel string)
		wantCreated bool
	}{
		{
			name: "creates a repository when none exists in the directory's ancestry",
			setup: func(t *testing.T, root string) (string, string, string) {
				dir := filepath.Join(root, "snapshots")
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
				managed := filepath.Join(repoRoot, "zamo-snapshot")
				require.NoError(t, os.MkdirAll(managed, 0o755))
				return managed, repoRoot, "zamo-snapshot"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			managed, wantRoot, wantRel := tc.setup(t, root)

			repo, relDir, err := openOrInitRepo(managed)

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

func TestCommitStagesOnlyTheManagedDirectory(t *testing.T) {
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
	_, err = wt.Commit("seed", &git.CommitOptions{Author: &identity})
	require.NoError(t, err)

	// An unrelated change elsewhere in the same repository, made by the
	// user's own separate git usage — this must survive untouched.
	writeFile(t, repoRoot, "existing.txt", "edited outside the managed directory")

	managed := filepath.Join(repoRoot, "zamo-snapshot")
	require.NoError(t, os.MkdirAll(managed, 0o755))
	writeFile(t, managed, "topic.md", "# Topic")

	_, relDir, err := openOrInitRepo(managed)
	require.NoError(t, err)
	result, err := commit(repo, relDir, "snapshot", time.Now())
	require.NoError(t, err)

	assert.True(t, result.Committed)
	require.NotNil(t, result.Commit)

	head, err := repo.Head()
	require.NoError(t, err)
	commitObj, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	assertOnlyPathCommitted(t, commitObj, "zamo-snapshot/topic.md")
	assertFileContent(t, commitObj, "existing.txt", "already committed")

	status, err := wt.Status()
	require.NoError(t, err)
	assert.Equal(t, git.Modified, status.File("existing.txt").Worktree, "the unrelated edit must stay an uncommitted working-tree change")
}

func TestCommitIsANoOpWhenNothingChangedUnderTheManagedDirectory(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "snapshots")
	require.NoError(t, os.MkdirAll(managed, 0o755))
	writeFile(t, managed, "topic.md", "# Topic")

	repo, relDir, err := openOrInitRepo(managed)
	require.NoError(t, err)
	first, err := commit(repo, relDir, "first", time.Now())
	require.NoError(t, err)
	require.True(t, first.Committed)

	second, err := commit(repo, relDir, "second", time.Now())
	require.NoError(t, err)

	assert.False(t, second.Committed)
	assert.Nil(t, second.Commit)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, first.Commit.Hash, head.Hash().String(), "a no-op run must not move HEAD")
}

// TestCommitLeavesTheRepositoryUnchangedWhenStagingFails forces the
// underlying object write to fail (a read-only objects directory) and
// checks the repository comes back exactly as it went in: no new commit,
// the on-disk index untouched, because a failing Add never reaches the
// SetIndex call that would persist it, and the same restore branch also
// covers a Commit failure after a successful Add.
func TestCommitLeavesTheRepositoryUnchangedWhenStagingFails(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "snapshots")
	require.NoError(t, os.MkdirAll(managed, 0o755))
	writeFile(t, managed, "topic.md", "# Topic")

	repo, relDir, err := openOrInitRepo(managed)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)
	objects := filepath.Join(wt.Filesystem.Root(), ".git", "objects")
	require.NoError(t, os.Chmod(objects, 0o555))
	t.Cleanup(func() { _ = os.Chmod(objects, 0o755) })

	_, err = commit(repo, relDir, "snapshot", time.Now())
	require.Error(t, err)

	_, err = repo.Head()
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound, "no commit means no HEAD yet")
	idx, err := repo.Storer.Index()
	require.NoError(t, err)
	assert.Empty(t, idx.Entries, "a failed Add must never reach SetIndex")
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
