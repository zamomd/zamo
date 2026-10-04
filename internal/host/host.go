// Package host declares the capabilities a feature module may receive from
// the application: narrow interfaces over the core, each covering one kind of
// access. A module names the capabilities it needs in its constructor and
// gets implementations from the composition root, never the service or the
// storage adapter behind them.
//
// Capabilities narrow what a module can call; they are not a security
// boundary, because a module runs in the process with full authority.
package host

import (
	"context"
	"encoding/json/jsontext"
	"time"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/scheduler"
)

// BaseReader is read access to the shape of the base: its topics and the
// full subtree under any block.
type BaseReader interface {
	// TopicMap returns every topic root with its first-level children.
	TopicMap(ctx context.Context) ([]knowledge.Block, error)
	// GetSubtree returns a block and all of its descendants in pre-order.
	GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error)
}

// FileWriter persists rendered files into a directory the user chose.
type FileWriter interface {
	// WriteFiles writes each file into dir. Names are unique within files,
	// so order does not matter.
	WriteFiles(ctx context.Context, dir string, files []knowledge.ExportFile) error
}

// FilePicker asks the user for a path through the platform's own dialog. An
// empty path and no error means the user cancelled.
type FilePicker interface {
	SaveFile(title, suggestedName string) (string, error)
	OpenFile(title string) (string, error)
	OpenDirectory(title string) (string, error)
}

// BaseArchive captures and restores the whole base as one archive.
type BaseArchive interface {
	// Backup writes a snapshot of the base to dest.
	Backup(ctx context.Context, dest string) error
	// StageRestore accepts an archive and leaves the replacement for the
	// next startup, when nothing holds the base open.
	StageRestore(ctx context.Context, archive string) error
	// PendingRestore reports the archive waiting for the next startup, or "".
	PendingRestore() string
	// CancelRestore drops a pending restore.
	CancelRestore() error
}

// BatchWriter applies one write batch through the single write path: every
// touched block gains a revision in one group, reviewed or awaiting review
// as the batch's WriteMeta decides.
type BatchWriter interface {
	ApplyOps(ctx context.Context, meta knowledge.WriteMeta, ops []knowledge.Op) (knowledge.Applied, error)
}

// AIEditor runs an AI edit command over a scope through the one edit path the
// app itself uses: the model's answer becomes an AI-authored batch awaiting
// review. A module running without the owner watching sets AIEdit.Unattended,
// and the edit then refuses to land over content still awaiting review.
type AIEditor interface {
	EditWithAI(ctx context.Context, e knowledge.AIEdit) (knowledge.Applied, error)
}

// Schedule is a module's view of one of its scheduled records.
type Schedule = scheduler.Schedule

// ScheduleHandler runs one record when its time comes.
type ScheduleHandler = scheduler.Handler

// ScheduleOptions is the policy a handler is registered with.
type ScheduleOptions = scheduler.HandlerOptions

// RunResult is a handler's report of one run.
type RunResult = scheduler.RunResult

// Outcome is how a run ended.
type Outcome = scheduler.Outcome

// The outcomes a handler reports.
const (
	OutcomeOK       = scheduler.OutcomeOK
	OutcomeNoChange = scheduler.OutcomeNoChange
	OutcomeSkipped  = scheduler.OutcomeSkipped
	OutcomeFailed   = scheduler.OutcomeFailed
)

// The scheduler's errors a module acts on.
var (
	ErrScheduleNotFound = scheduler.ErrNotFound
	ErrInvalidSpec      = scheduler.ErrInvalidSpec
	ErrScheduleBusy     = scheduler.ErrBusy
)

// Scheduler is timed work, bound to the module it was handed to: the module
// registers one handler and manages its own records by key; it sees nobody
// else's. The scheduler persists the records, runs them while the app is
// running, collapses the slots a sleeping machine missed into one run, drops
// those that passed while the app was not running at all, and keeps two
// processes on one base from running the same record.
type Scheduler interface {
	Handle(h ScheduleHandler, opts ScheduleOptions)
	// Put creates or replaces the record for key; the next run counts from
	// now, so saving never runs it. Disabling cancels a run in flight.
	Put(ctx context.Context, key, spec string, enabled bool, payload jsontext.Value) (Schedule, error)
	Get(ctx context.Context, key string) (Schedule, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]Schedule, error)
	// RunNow runs the record at once through the same path as a timed run.
	RunNow(ctx context.Context, key string) error
	Cancel(ctx context.Context, key string) bool
	// Preview validates spec and returns its next n firing times.
	Preview(spec string, n int) ([]time.Time, error)
}

// Coded is an error that carries a stable wire code, so the frontend can
// switch on it without matching text. A module gives its own failures codes
// this way; the domain's sentinels are mapped by the entry points.
type Coded interface {
	error
	Code() string
}
