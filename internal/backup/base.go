package backup

import (
	"context"
	"database/sql"
)

// Base ties the archive operations to one open database and the file it
// lives in, so callers name neither.
type Base struct {
	db      *sql.DB
	path    string
	version string
}

// NewBase binds the archive operations to db, the file at path it was opened
// from, and the application version stamped into every archive.
func NewBase(db *sql.DB, path, version string) Base {
	return Base{db: db, path: path, version: version}
}

// Backup writes a snapshot of the base to dest.
func (b Base) Backup(ctx context.Context, dest string) error {
	return Create(ctx, b.db, dest, b.version)
}

// StageRestore accepts an archive and leaves the replacement for the next
// startup; see Stage for why it cannot happen now.
func (b Base) StageRestore(ctx context.Context, archive string) error {
	return Stage(ctx, archive, b.path)
}

// PendingRestore reports the archive waiting for the next startup, or "".
func (b Base) PendingRestore() string {
	return StagedRestore(b.path)
}

// CancelRestore drops a pending restore.
func (b Base) CancelRestore() error {
	return CancelStaged(b.path)
}
