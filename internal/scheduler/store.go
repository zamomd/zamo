package scheduler

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound reports a schedule that does not exist.
var ErrNotFound = errors.New("schedule not found")

// Record is one persisted schedule. A zero NextRun means the record is not
// waiting to run (it is disabled); a zero LastRun means it never ran.
type Record struct {
	ID      string
	Module  string
	Key     string
	Spec    string
	Enabled bool
	Payload []byte

	NextRun time.Time
	LastRun time.Time
	Outcome string
	Detail  string

	ClaimedAt time.Time
	ClaimedBy string
}

// Completion is what a finished run writes back.
type Completion struct {
	At      time.Time
	Outcome string
	Detail  string
	NextRun time.Time
}

// Store is the persistence the scheduler needs. Records are addressed by
// (module, key); a claim is what keeps two processes on one database from
// running the same record, so Claim and Finish must be atomic.
type Store interface {
	// Put inserts the record or, when (Module, Key) exists, replaces its spec,
	// enabled flag, payload and next run. The last run, its outcome and any
	// claim are left alone. It returns the stored record.
	Put(ctx context.Context, rec Record) (Record, error)
	Get(ctx context.Context, module, key string) (Record, error)
	Delete(ctx context.Context, module, key string) error
	// List returns the module's records, or every record for an empty module.
	List(ctx context.Context, module string) ([]Record, error)
	// Due returns enabled records whose next run is not after now and that no
	// live claim holds, earliest first.
	Due(ctx context.Context, now time.Time, lease time.Duration) ([]Record, error)
	// Claim takes the record for instance unless a claim younger than lease
	// holds it. It reports whether the claim succeeded.
	Claim(ctx context.Context, id, instance string, now time.Time, lease time.Duration) (bool, error)
	// Finish records a completed run and releases the claim, but only while
	// instance still holds it. It reports whether anything was written.
	Finish(ctx context.Context, id, instance string, done Completion) (bool, error)
	// Release drops instance's claim and changes nothing else.
	Release(ctx context.Context, id, instance string) error
	// Reschedule moves an enabled record's next run and changes nothing else.
	Reschedule(ctx context.Context, id string, next time.Time) error
}
