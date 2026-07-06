// Package sqlite is the storage adapter: the knowledge.Repository
// implementation on top of SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sqlite3 "modernc.org/sqlite"

	"github.com/cominddev/comind/internal/knowledge"
)

// Open opens (creating if needed) the database file and applies
// migrations. Modes are set for concurrent multi-process access
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := initialize(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database %s: %w", path, err)
	}
	return db, nil
}

// initialize brings a connection up (running the DSN pragmas) and applies
// migrations, retrying on SQLITE_BUSY.
func initialize(ctx context.Context, db *sql.DB) error {
	var err error
	for attempt := range 20 {
		if err = db.PingContext(ctx); err == nil {
			err = Migrate(ctx, db)
		}
		if err == nil {
			return nil
		}
		if !isBusy(err) {
			return err
		}
		select {
		case <-time.After(time.Duration(25+attempt*25) * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// sqliteBusy is the SQLITE_BUSY primary result code.
const sqliteBusy = 5

func isBusy(err error) bool {
	if e, ok := errors.AsType[*sqlite3.Error](err); ok {
		return e.Code()&0xFF == sqliteBusy
	}
	return false
}

// Repo implements knowledge.Repository.
type Repo struct {
	db *sql.DB
}

var _ knowledge.Repository = (*Repo)(nil)

// NewRepo wraps an opened database (see Open) in the Repository
// implementation. Method semantics are documented on the interface.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// GetTopics implements knowledge.Repository.
func (r *Repo) GetTopics(ctx context.Context) ([]knowledge.Block, error) {
	return nil, knowledge.ErrNotImplemented
}

// GetSubtree implements knowledge.Repository.
func (r *Repo) GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	return nil, knowledge.ErrNotImplemented
}

// EditBlockContent implements knowledge.Repository.
func (r *Repo) EditBlockContent(ctx context.Context, id knowledge.ID, content string) error {
	return knowledge.ErrNotImplemented
}

// SaveProposal implements knowledge.Repository.
func (r *Repo) SaveProposal(ctx context.Context, p knowledge.Proposal) error {
	return knowledge.ErrNotImplemented
}

// ListProposals implements knowledge.Repository.
func (r *Repo) ListProposals(ctx context.Context, status knowledge.Status) ([]knowledge.Proposal, error) {
	return nil, knowledge.ErrNotImplemented
}

// ApplyProposal implements knowledge.Repository.
func (r *Repo) ApplyProposal(ctx context.Context, id knowledge.ID, ops []knowledge.Op) error {
	return knowledge.ErrNotImplemented
}

// RejectProposal implements knowledge.Repository.
func (r *Repo) RejectProposal(ctx context.Context, id knowledge.ID) error {
	return knowledge.ErrNotImplemented
}

// Search implements knowledge.Repository.
func (r *Repo) Search(ctx context.Context, query string) ([]knowledge.Block, error) {
	return nil, knowledge.ErrNotImplemented
}
