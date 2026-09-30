package gitrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/zamomd/zamo/internal/host"
)

// identity is the fixed author and committer every snapshot commit carries.
// go-git requires one explicitly when it cannot read user.name/user.email
// from git config, which a freshly initialized repository has none of.
var identity = object.Signature{Name: "Zamo", Email: "zamo@localhost"}

// openOrInitRepo finds the repository that owns dir by walking up from it,
// the equivalent of `git rev-parse --show-toplevel`, or creates one rooted
// at dir itself when none is found. It reports dir's path relative to the
// repository's worktree root — the only path commit is ever allowed to
// touch, even when the repository root is an ancestor of dir.
func openOrInitRepo(dir string) (repo *git.Repository, relDir string, err error) {
	// Resolved up front: go-git reports the worktree root through the
	// filesystem's real path (e.g. macOS resolves /var to /private/var), so
	// comparing an unresolved dir against it would compute a garbage
	// relative path and mis-scope every later operation.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve %s: %w", dir, err)
	}

	repo, err = git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	switch {
	case errors.Is(err, git.ErrRepositoryNotExists):
		repo, err = git.PlainInit(dir, false)
		if err != nil {
			return nil, "", fmt.Errorf("start a repository at %s: %w", dir, err)
		}
	case err != nil:
		return nil, "", fmt.Errorf("open the repository owning %s: %w", dir, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, "", fmt.Errorf("open the worktree: %w", err)
	}
	rel, err := filepath.Rel(wt.Filesystem.Root(), dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve %s relative to the repository root: %w", dir, err)
	}
	return repo, filepath.ToSlash(rel), nil
}

// isUnder reports whether path lies at or inside directory — the same rule
// go-git's own Worktree.Add uses to scope a directory add, reimplemented
// here because it is unexported.
func isUnder(path, directory string) bool {
	return directory == "." || path == directory || strings.HasPrefix(path, directory+"/")
}

// changedUnder reports whether anything at or inside relDir differs from
// HEAD, in the working tree or the index. It never inspects paths outside
// relDir, which is what keeps a no-op run from touching staging state at
// all: changedUnder runs before anything is staged.
func changedUnder(status git.Status, relDir string) bool {
	for path, fs := range status {
		if !isUnder(path, relDir) {
			continue
		}
		if fs.Worktree != git.Unmodified || fs.Staging != git.Unmodified {
			return true
		}
	}
	return false
}

// stagedOutside lists paths outside relDir that are already staged. A
// non-empty result means the index is not clean going in — committing now
// would sweep someone else's separate staged work into our commit, so the
// caller refuses instead.
func stagedOutside(status git.Status, relDir string) []string {
	var paths []string
	for path, fs := range status {
		if isUnder(path, relDir) {
			continue
		}
		if fs.Staging != git.Unmodified {
			paths = append(paths, path)
		}
	}
	return paths
}

// commit stages only relDir and commits it when — and only when —
// changedUnder reports a difference there, refusing outright if the index
// already holds unrelated staged changes. The index is snapshotted before
// Add and restored verbatim if anything after it fails, so a failed run
// leaves the repository exactly as it found it.
func commit(repo *git.Repository, relDir, message string, when time.Time) (host.GitSnapshotResult, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("open the worktree: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("read the worktree status: %w", err)
	}
	if outside := stagedOutside(status, relDir); len(outside) > 0 {
		return host.GitSnapshotResult{}, fmt.Errorf(
			"refusing to commit: %d file(s) already staged outside the managed directory", len(outside),
		)
	}
	if !changedUnder(status, relDir) {
		return host.GitSnapshotResult{}, nil
	}

	idx, err := repo.Storer.Index()
	if err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("read the index: %w", err)
	}

	result, err := stageAndCommit(repo, wt, relDir, message, when)
	if err != nil {
		if restoreErr := repo.Storer.SetIndex(idx); restoreErr != nil {
			return host.GitSnapshotResult{}, fmt.Errorf("%w (and the index could not be restored: %w)", err, restoreErr)
		}
		return host.GitSnapshotResult{}, err
	}
	return result, nil
}

func stageAndCommit(
	repo *git.Repository, wt *git.Worktree, relDir, message string, when time.Time,
) (host.GitSnapshotResult, error) {
	if _, err := wt.Add(relDir); err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("stage %s: %w", relDir, err)
	}
	author := identity
	author.When = when
	hash, err := wt.Commit(message, &git.CommitOptions{Author: &author})
	if err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("commit: %w", err)
	}
	return host.GitSnapshotResult{
		Committed: true,
		Commit:    &host.GitCommitInfo{Hash: hash.String(), Message: message, At: when},
	}, nil
}
