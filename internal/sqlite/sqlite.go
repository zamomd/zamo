// Package sqlite is the storage adapter: the knowledge.Repository
// implementation on top of SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
	db  *sql.DB
	now func() time.Time
}

var _ knowledge.Repository = (*Repo)(nil)

// NewRepo wraps an opened database (see Open) in the Repository
// implementation. now supplies decision timestamps (decided_at); inject a
// fixed clock in tests. Method semantics are documented on the interface.
func NewRepo(db *sql.DB, now func() time.Time) *Repo { return &Repo{db: db, now: now} }

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

// timeLayout is the on-disk format for proposal timestamps: RFC3339 with
// nanoseconds, always stored in UTC so lexicographic order matches chronological
// order. The DSN does not enable the driver's TEXT-to-time conversion, so times
// are formatted and parsed explicitly.
const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// SaveProposal implements knowledge.Repository.
func (r *Repo) SaveProposal(ctx context.Context, p knowledge.Proposal) error {
	payload, err := json.Marshal(p.Ops)
	if err != nil {
		return fmt.Errorf("marshal proposal %s ops: %w", p.ID, err)
	}
	// decided_at stays NULL while pending; a nil interface binds as SQL NULL.
	var decidedAt any
	if !p.DecidedAt.IsZero() {
		decidedAt = formatTime(p.DecidedAt)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO proposals (id, source, status, title, payload, created_at, decided_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(p.ID), string(p.Source), string(p.Status), p.Title,
		string(payload), formatTime(p.CreatedAt), decidedAt,
	)
	if err != nil {
		return fmt.Errorf("save proposal %s: %w", p.ID, err)
	}
	return nil
}

// ListProposals implements knowledge.Repository.
func (r *Repo) ListProposals(ctx context.Context, status knowledge.Status) ([]knowledge.Proposal, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, source, status, title, payload, created_at, decided_at
		 FROM proposals WHERE status = ? ORDER BY created_at, id`,
		string(status),
	)
	if err != nil {
		return nil, fmt.Errorf("list %s proposals: %w", status, err)
	}
	defer rows.Close()

	var out []knowledge.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s proposals: %w", status, err)
	}
	return out, nil
}

// scanProposal reads one proposals row into a domain Proposal, reversing the
// SaveProposal encoding (JSON payload, TEXT timestamps, nullable decided_at).
func scanProposal(rows *sql.Rows) (knowledge.Proposal, error) {
	var (
		p                                      knowledge.Proposal
		id, source, status, payload, createdAt string
		decidedAt                              sql.NullString
	)
	if err := rows.Scan(&id, &source, &status, &p.Title, &payload, &createdAt, &decidedAt); err != nil {
		return knowledge.Proposal{}, fmt.Errorf("scan proposal: %w", err)
	}
	p.ID = knowledge.ID(id)
	p.Source = knowledge.Source(source)
	p.Status = knowledge.Status(status)
	if err := json.Unmarshal([]byte(payload), &p.Ops); err != nil {
		return knowledge.Proposal{}, fmt.Errorf("unmarshal proposal %s ops: %w", id, err)
	}
	created, err := time.Parse(timeLayout, createdAt)
	if err != nil {
		return knowledge.Proposal{}, fmt.Errorf("parse created_at of proposal %s: %w", id, err)
	}
	p.CreatedAt = created
	if decidedAt.Valid {
		t, err := time.Parse(timeLayout, decidedAt.String)
		if err != nil {
			return knowledge.Proposal{}, fmt.Errorf("parse decided_at of proposal %s: %w", id, err)
		}
		p.DecidedAt = t
	}
	return p, nil
}

// ApplyProposal implements knowledge.Repository.
func (r *Repo) ApplyProposal(ctx context.Context, id knowledge.ID, ops []knowledge.Op) error {
	return knowledge.ErrNotImplemented
}

// RejectProposal implements knowledge.Repository.
func (r *Repo) RejectProposal(ctx context.Context, id knowledge.ID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE proposals SET status = ?, decided_at = ?
		 WHERE id = ? AND status = ?`,
		string(knowledge.StatusRejected), formatTime(r.now()),
		string(id), string(knowledge.StatusPending),
	)
	if err != nil {
		return fmt.Errorf("reject proposal %s: %w", id, err)
	}
	return nil
}

// Search implements knowledge.Repository.
func (r *Repo) Search(ctx context.Context, query string) ([]knowledge.Block, error) {
	return nil, knowledge.ErrNotImplemented
}
