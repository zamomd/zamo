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

	"github.com/cominddev/comind/internal/knowledge"
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

// Coded is an error that carries a stable wire code, so the frontend can
// switch on it without matching text. A module gives its own failures codes
// this way; the domain's sentinels are mapped by the entry points.
type Coded interface {
	error
	Code() string
}
