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
	"errors"
	"time"

	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
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
	// WriteFiles makes dir hold files: it writes each one and removes those
	// an earlier call wrote there that are no longer among them. Nothing it
	// did not write is removed. Names are unique within files, so order does
	// not matter.
	WriteFiles(ctx context.Context, dir string, files []knowledge.ExportFile) error
}

// Commit is one commit in a directory's history. The zero value means no
// commit was made.
type Commit struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	At      time.Time `json:"at,omitzero"`
}

// ErrNoGit is DirHistory's failure on a machine with no git to run.
var ErrNoGit = errors.New("git is not installed")

// DirHistory keeps the history of a directory in a local git repository.
type DirHistory interface {
	// Root answers the root of the repository that owns dir, or "" when
	// none does and Commit would start one at dir. It changes nothing.
	Root(ctx context.Context, dir string) (string, error)
	// Commit records what changed in dir since its last commit, in the
	// repository that owns dir or a new one started at dir. Only dir is
	// committed: changes elsewhere in the repository stay as they are,
	// staged ones included. A dir that has not changed yields the zero
	// Commit. Nothing is ever pushed.
	Commit(ctx context.Context, dir, message string) (Commit, error)
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

// Agent runs the AI over the base: the model reads through the base's tools
// and writes only what the run's policy allows, as an AI-authored batch
// awaiting review. Every AI feature of a module goes through it.
type Agent interface {
	Run(ctx context.Context, run AgentRun) (AgentResult, error)
}

// AgentRun is one request to the agent.
type AgentRun = agent.Run

// AgentResult is how a run ended.
type AgentResult = agent.Result

// WritePolicy is what a run may change.
type WritePolicy = agent.WritePolicy

// Persona is who the model is asked to be in a run.
type Persona = llm.Persona

// The personas a module may run.
const (
	PersonaAssistant = llm.PersonaAssistant
	PersonaEditor    = llm.PersonaEditor
)

// Message is one turn of an agent conversation. A module keeps the turns a
// run returns and hands them back as the next run's history; it does not
// build or read them.
type Message = llm.Message

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
