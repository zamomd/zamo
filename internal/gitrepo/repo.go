// Package gitrepo is the git adapter for a directory's history: the
// host.DirHistory implementation that commits a directory into the local
// repository owning it, by running the machine's own git. It knows nothing
// of the base or of what the directory holds.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/zamomd/zamo/internal/host"
)

// Repo commits directories into local git repositories, stamping each commit
// with its clock.
type Repo struct {
	now func() time.Time
}

var _ host.DirHistory = Repo{}

// New returns a Repo that reads commit times from now.
func New(now func() time.Time) Repo { return Repo{now: now} }

// Root implements host.DirHistory. dir must exist.
func (Repo) Root(ctx context.Context, dir string) (string, error) {
	g, err := gitIn(dir)
	if err != nil {
		return "", err
	}
	return g.root(ctx)
}

// Commit implements host.DirHistory. dir must exist.
func (r Repo) Commit(ctx context.Context, dir, message string) (host.Commit, error) {
	g, err := gitIn(dir)
	if err != nil {
		return host.Commit{}, err
	}
	root, err := g.root(ctx)
	if err != nil {
		return host.Commit{}, err
	}
	if root == "" {
		if _, err := g.run(ctx, nil, "init", "--quiet"); err != nil {
			return host.Commit{}, err
		}
	} else if ignored, err := g.ignored(ctx); err != nil {
		return host.Commit{}, err
	} else if ignored {
		// Staging an ignored directory stages nothing, which would read as
		// an export with no changes for ever.
		return host.Commit{}, fmt.Errorf("the repository at %s ignores %s", root, dir)
	}

	if _, err := g.run(ctx, nil, "add", "--all", "--", "."); err != nil {
		return host.Commit{}, err
	}
	changed, err := g.staged(ctx)
	if err != nil || !changed {
		return host.Commit{}, err
	}
	when := r.now()
	if err := g.commit(ctx, message, when); err != nil {
		// Unstaged again, so a failed run leaves no half-made commit for the
		// owner's next one to sweep up.
		_, _ = g.run(context.WithoutCancel(ctx), nil, "reset", "--quiet", "--", ".")
		return host.Commit{}, err
	}
	hash, err := g.run(ctx, nil, "rev-parse", "HEAD")
	if err != nil {
		return host.Commit{}, err
	}
	return host.Commit{Hash: hash, Message: message, At: when}, nil
}

// fallbackName and fallbackEmail sign a commit when git has no user
// configured, which a machine that never ran git does not.
const (
	fallbackName  = "Zamo"
	fallbackEmail = "zamo@localhost"
)

// located are the variables that point git at a repository other than the
// one owning its working directory. A process started from a git hook
// inherits them.
var located = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
}

// git runs the machine's git in one directory.
type git struct {
	bin string
	dir string
}

func gitIn(dir string) (git, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return git{}, fmt.Errorf("%w: %w", host.ErrNoGit, err)
	}
	return git{bin: bin, dir: dir}, nil
}

// run answers git's trimmed output. A failure carries what git said.
func (g git) run(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.bin, args...)
	cmd.Dir = g.dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(located, name)
	})
	cmd.Env = append(cmd.Env, "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &gitError{verb: args[0], said: strings.TrimSpace(stderr.String()), err: err}
	}
	return strings.TrimSpace(string(out)), nil
}

// gitError is a git command that failed, with what it printed.
type gitError struct {
	verb string
	said string
	err  error
}

func (e *gitError) Error() string {
	if e.said == "" {
		return fmt.Sprintf("git %s: %v", e.verb, e.err)
	}
	return fmt.Sprintf("git %s: %s", e.verb, e.said)
}

func (e *gitError) Unwrap() error { return e.err }

// exited reports whether err is git exiting with code.
func exited(err error, code int) bool {
	exit, ok := errors.AsType[*exec.ExitError](err)
	return ok && exit.ExitCode() == code
}

// root is the worktree root of the repository owning the directory, or ""
// when no repository does.
func (g git) root(ctx context.Context) (string, error) {
	root, err := g.run(ctx, nil, "rev-parse", "--show-toplevel")
	if failed, ok := errors.AsType[*gitError](err); ok && strings.Contains(failed.said, "not a git repository") {
		return "", nil
	}
	return root, err
}

// ignored reports whether the repository ignores the directory itself.
func (g git) ignored(ctx context.Context) (bool, error) {
	prefix, err := g.run(ctx, nil, "rev-parse", "--show-prefix")
	if err != nil || prefix == "" {
		return false, err
	}
	_, err = g.run(ctx, nil, "check-ignore", "--quiet", ".")
	if exited(err, 1) {
		return false, nil
	}
	return err == nil, err
}

// staged reports whether the index differs from HEAD inside the directory.
func (g git) staged(ctx context.Context) (bool, error) {
	_, err := g.run(ctx, nil, "diff", "--cached", "--quiet", "--", ".")
	if exited(err, 1) {
		return true, nil
	}
	return false, err
}

// commit commits the directory and nothing else: --only leaves whatever the
// owner staged elsewhere in the repository staged and out of the commit.
// Hooks and signing are skipped, since a scheduled run has nobody to answer
// a failed check or a passphrase prompt.
func (g git) commit(ctx context.Context, message string, when time.Time) error {
	var args []string
	if !g.identified(ctx) {
		args = append(args, "-c", "user.name="+fallbackName, "-c", "user.email="+fallbackEmail)
	}
	stamp := when.Format(time.RFC3339)
	args = append(args,
		"commit", "--quiet", "--only", "--no-verify", "--no-gpg-sign", "--cleanup=verbatim",
		"--date", stamp, "--message", message, "--", ".",
	)
	_, err := g.run(ctx, []string{"GIT_COMMITTER_DATE=" + stamp}, args...)
	return err
}

// identified reports whether git is configured with a user to commit as, in
// the repository or globally.
func (g git) identified(ctx context.Context) bool {
	for _, key := range []string{"user.name", "user.email"} {
		if v, err := g.run(ctx, nil, "config", "--get", key); err != nil || v == "" {
			return false
		}
	}
	return true
}
