package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/zamomd/zamo/internal/host"
)

// errEmptyDirectory reports an attempt to set the managed directory to "".
var errEmptyDirectory = errors.New("the git snapshot directory must not be empty")

// Clock reports the current time. Injected so commit timestamps are
// testable and never called directly outside this package.
type Clock func() time.Time

// Base ties the git logic, the persisted preferences, and the commit clock
// together. It satisfies host.GitSnapshot.
type Base struct {
	settingsPath string
	clock        Clock
}

var _ host.GitSnapshot = Base{}

// NewBase binds the preferences stored at settingsPath to clock.
func NewBase(settingsPath string, clock Clock) Base {
	return Base{settingsPath: settingsPath, clock: clock}
}

// Status reports the current preferences and, when a directory is set, the
// latest commit under it — read fresh from the repository every call, never
// cached, so a restart never loses it.
func (b Base) Status() (host.GitSnapshotStatus, error) {
	p, err := loadPreferences(b.settingsPath)
	if err != nil {
		return host.GitSnapshotStatus{}, err
	}
	tmpl := p.CommitTemplate
	if tmpl == "" {
		tmpl = DefaultCommitTemplate
	}
	status := host.GitSnapshotStatus{Dir: p.Dir, CommitTemplate: tmpl}
	if p.Dir == "" {
		return status, nil
	}

	repo, _, err := openOrInitRepo(p.Dir)
	if err != nil {
		return host.GitSnapshotStatus{}, err
	}
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return status, nil
	}
	if err != nil {
		return host.GitSnapshotStatus{}, fmt.Errorf("read the latest commit: %w", err)
	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		return host.GitSnapshotStatus{}, fmt.Errorf("read the latest commit: %w", err)
	}
	status.LatestCommit = &host.GitCommitInfo{
		Hash:    c.Hash.String(),
		Message: c.Message,
		At:      c.Author.When,
	}
	return status, nil
}

// ExportAndCommit renders the base into the managed directory via export
// and commits it if that render actually changed something there.
func (b Base) ExportAndCommit(ctx context.Context, export func(ctx context.Context, dir string) error) (host.GitSnapshotResult, error) {
	p, err := loadPreferences(b.settingsPath)
	if err != nil {
		return host.GitSnapshotResult{}, err
	}
	if p.Dir == "" {
		return host.GitSnapshotResult{}, ErrDirNotSet
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return host.GitSnapshotResult{}, fmt.Errorf("create the git snapshot directory: %w", err)
	}
	if err := export(ctx, p.Dir); err != nil {
		return host.GitSnapshotResult{}, err
	}

	repo, relDir, err := openOrInitRepo(p.Dir)
	if err != nil {
		return host.GitSnapshotResult{}, err
	}
	tmpl := p.CommitTemplate
	if tmpl == "" {
		tmpl = DefaultCommitTemplate
	}
	when := b.clock()
	message, err := renderCommitMessage(tmpl, when)
	if err != nil {
		return host.GitSnapshotResult{}, err
	}
	return commit(repo, relDir, message, when)
}

// SetDirectory persists dir as the managed directory. Real repository
// problems surface later, at ExportAndCommit time — this only rejects what
// can never be valid.
func (b Base) SetDirectory(dir string) error {
	if dir == "" {
		return errEmptyDirectory
	}
	p, err := loadPreferences(b.settingsPath)
	if err != nil {
		return err
	}
	p.Dir = dir
	return savePreferences(b.settingsPath, p)
}

// SetCommitTemplate validates tmpl and persists it. A rejected template
// never reaches settings.toml, so the previous one stays effective.
func (b Base) SetCommitTemplate(tmpl string) error {
	if err := validateCommitTemplate(tmpl); err != nil {
		return fmt.Errorf("invalid commit message template: %w", err)
	}
	p, err := loadPreferences(b.settingsPath)
	if err != nil {
		return err
	}
	p.CommitTemplate = tmpl
	return savePreferences(b.settingsPath, p)
}
