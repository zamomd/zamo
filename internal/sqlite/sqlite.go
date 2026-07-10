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

// translate maps infrastructure errors to domain sentinels so driver types never leak to callers.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return knowledge.ErrNotFound
	case isBusy(err):
		return knowledge.ErrBusy
	default:
		return err
	}
}

// Repo implements knowledge.Repository.
type Repo struct {
	db  *sql.DB
	now func() time.Time
}

var _ knowledge.Repository = (*Repo)(nil)

// NewRepo wraps an opened database (see Open) in the Repository
// implementation. now supplies decision timestamps (decided_at); inject a
// fixed clock in tests. A nil now defaults to time.Now. Method semantics are
// documented on the interface.
func NewRepo(db *sql.DB, now func() time.Time) *Repo {
	if now == nil {
		now = time.Now
	}
	return &Repo{db: db, now: now}
}

// EditBlockContent implements knowledge.Repository.
func (r *Repo) EditBlockContent(ctx context.Context, id knowledge.ID, content string) error {
	return knowledge.ErrNotImplemented
}

// timeLayout is the on-disk format for proposal timestamps: fixed-width,
// zero-padded RFC3339 nanoseconds in UTC. Fixed width keeps lexicographic order
// equal to chronological order — which ORDER BY created_at (a TEXT column)
// relies on — since time.RFC3339Nano trims trailing zeros and would break it.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

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
		return fmt.Errorf("save proposal %s: %w", p.ID, translate(err))
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
		return nil, fmt.Errorf("list %s proposals: %w", status, translate(err))
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
		return nil, fmt.Errorf("list %s proposals: %w", status, translate(err))
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
	created, err := parseTime(createdAt)
	if err != nil {
		return knowledge.Proposal{}, fmt.Errorf("parse created_at of proposal %s: %w", id, err)
	}
	p.CreatedAt = created
	if decidedAt.Valid {
		t, err := parseTime(decidedAt.String)
		if err != nil {
			return knowledge.Proposal{}, fmt.Errorf("parse decided_at of proposal %s: %w", id, err)
		}
		p.DecidedAt = t
	}
	return p, nil
}

// ApplyProposal implements knowledge.Repository. It applies the domain's ready,
// ordered op list in one immediate transaction and marks the proposal applied.
// Ops carry final identifiers (BlockID is the target block — the new block for
// add; Parent/From/To are resolved ids), so storage only writes. The apply is
// atomic: any error rolls the whole transaction back, so search and the tree
// never observe a half-applied proposal.
func (r *Repo) ApplyProposal(ctx context.Context, id knowledge.ID, ops []knowledge.Op) error {
	// _txlock=immediate (DSN) makes this a BEGIN IMMEDIATE: the write lock is
	// taken up front, so a read-then-write apply cannot deadlock on a lock
	// upgrade under WAL.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply proposal %s: %w", id, err)
	}
	defer tx.Rollback()

	// The proposal must still be pending; its source is the blocks' provenance.
	var source string
	err = tx.QueryRowContext(ctx,
		`SELECT source FROM proposals WHERE id = ? AND status = ?`,
		string(id), string(knowledge.StatusPending)).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return decideReason(ctx, tx, id)
	}
	if err != nil {
		return fmt.Errorf("apply proposal %s: %w", id, err)
	}

	now := formatTime(r.now())
	for _, op := range ops {
		if err := applyOp(ctx, tx, op, knowledge.Source(source), now); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE proposals SET status = ?, decided_at = ? WHERE id = ?`,
		string(knowledge.StatusApplied), now, string(id)); err != nil {
		return fmt.Errorf("apply proposal %s: %w", id, err)
	}
	return tx.Commit()
}

// applyOp applies one resolved operation inside the apply transaction.
func applyOp(ctx context.Context, tx *sql.Tx, op knowledge.Op, source knowledge.Source, now string) error {
	switch op.Kind {
	case knowledge.OpAdd:
		return applyAdd(ctx, tx, op, source, now)
	case knowledge.OpEdit:
		return applyEdit(ctx, tx, op, now)
	case knowledge.OpMove:
		return applyMove(ctx, tx, op, now)
	case knowledge.OpDelete:
		return applyDelete(ctx, tx, op)
	default:
		return fmt.Errorf("apply: unsupported op kind %q", op.Kind)
	}
}

// applyAdd inserts a new block appended under its parent, tagged with AI
// authorship and the proposal's source as provenance.
func applyAdd(ctx context.Context, tx *sql.Tx, op knowledge.Op, source knowledge.Source, now string) error {
	// A topic root has NULL parent_id; `IS ?` matches both NULL and a value.
	var parent any
	if op.Parent != "" {
		parent = string(op.Parent)
	}
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position)+1, 0) FROM blocks WHERE parent_id IS ?`,
		parent).Scan(&pos); err != nil {
		return fmt.Errorf("apply add %s: next position: %w", op.BlockID, err)
	}
	meta := knowledge.Metadata{"author": "ai", "source": string(source)}
	if op.Origin != "" {
		meta["origin"] = op.Origin
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("apply add %s: metadata: %w", op.BlockID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(op.BlockID), parent, pos, op.Content, string(payload), now, now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, err)
	}
	return nil
}

// applyEdit replaces a block's content, refreshing updated_at.
func applyEdit(ctx context.Context, tx *sql.Tx, op knowledge.Op, now string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, updated_at = ? WHERE id = ?`,
		op.Content, now, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, err)
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// applyMove reparents a block, appending it after its new siblings.
func applyMove(ctx context.Context, tx *sql.Tx, op knowledge.Op, now string) error {
	var parent any
	if op.Parent != "" {
		parent = string(op.Parent)
	}
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position)+1, 0) FROM blocks WHERE parent_id IS ?`,
		parent).Scan(&pos); err != nil {
		return fmt.Errorf("apply move %s: next position: %w", op.BlockID, err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE blocks SET parent_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		parent, pos, now, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, err)
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// applyDelete removes a block. The schema has no ON DELETE CASCADE, so a block
// with surviving children or edges trips a foreign-key error and rolls the
// apply back; the domain must delete descendants and links in the same ops.
func applyDelete(ctx context.Context, tx *sql.Tx, op knowledge.Op) error {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM blocks WHERE id = ?`, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply delete %s: %w", op.BlockID, err)
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// affectedOne maps a no-op edit/move/delete to ErrStale: the target block
// vanished between the proposal and its apply, so the proposal is out of date.
func affectedOne(res sql.Result, kind knowledge.OpKind, id knowledge.ID) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply %s %s: %w", kind, id, err)
	}
	if n == 0 {
		return fmt.Errorf("apply %s %s: %w", kind, id, knowledge.ErrStale)
	}
	return nil
}

// RejectProposal implements knowledge.Repository.
func (r *Repo) RejectProposal(ctx context.Context, id knowledge.ID) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE proposals SET status = ?, decided_at = ?
		 WHERE id = ? AND status = ?`,
		string(knowledge.StatusRejected), formatTime(r.now()),
		string(id), string(knowledge.StatusPending),
	)
	if err != nil {
		return fmt.Errorf("reject proposal %s: %w", id, translate(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reject proposal %s: %w", id, translate(err))
	}
	if n == 0 {
		return decideReason(ctx, r.db, id)
	}
	return nil
}
// rowQueryer is the read surface shared by *sql.DB and *sql.Tx, so decideReason
// works standalone (reject) or inside the apply transaction.
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// decideReason explains why deciding a proposal (reject/apply) matched no
// pending row: it is absent (ErrNotFound) or already decided (ErrNotPending).
// Run outside the deciding transaction (reject), a concurrent writer could swap
// the two, but only the message differs — the decide itself is atomic — and
// proposals are never deleted in the MVP.
func decideReason(ctx context.Context, q rowQueryer, id knowledge.ID) error {
	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM proposals WHERE id = ?)`, string(id)).Scan(&exists); err != nil {
		return fmt.Errorf("proposal %s: %w", id, translate(err))
	}
	if !exists {
		return fmt.Errorf("proposal %s: %w", id, knowledge.ErrNotFound)
	}
	return fmt.Errorf("proposal %s: %w", id, knowledge.ErrNotPending)
}
