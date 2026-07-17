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

// metaAuthorPath is the json_set path to the authorship stamp. Every writer of
// a block's content must restamp it: the badge names the last writer, so a
// content write that leaves the stamp behind attributes one side's text to the
// other.
const metaAuthorPath = "$." + knowledge.MetaKeyAuthor

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
	// The owner editing a block by hand takes authorship of it (see metaAuthorPath).
	res, err := r.db.ExecContext(ctx,
		`UPDATE blocks SET content = ?, metadata = json_set(metadata, ?, ?), updated_at = ? WHERE id = ?`,
		content, metaAuthorPath, string(knowledge.AuthorHuman), formatTime(r.now()), string(id))
	if err != nil {
		return fmt.Errorf("edit block %s: %w", id, translate(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("edit block %s: %w", id, translate(err))
	}
	if n == 0 {
		// The block vanished between render and save: report it, don't feign success.
		return fmt.Errorf("edit block %s: %w", id, knowledge.ErrNotFound)
	}
	return nil
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
		return fmt.Errorf("apply proposal %s: %w", id, translate(err))
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
		return fmt.Errorf("apply proposal %s: %w", id, translate(err))
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
		return fmt.Errorf("apply proposal %s: %w", id, translate(err))
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
	case knowledge.OpLink:
		return applyLink(ctx, tx, op, now)
	default:
		return fmt.Errorf("apply: unsupported op kind %q", op.Kind)
	}
}

// requireParent returns ErrStale when a non-root op names a parent that vanished
// between the proposal and its apply — the same "the base moved" signal a missing
// target gives. Root ops (empty Parent) have nothing to check. Domain ordering
// applies parents before children, so an intra-proposal parent already exists.
func requireParent(ctx context.Context, tx *sql.Tx, kind knowledge.OpKind, op knowledge.Op) error {
	if op.Parent == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM blocks WHERE id = ?)`, string(op.Parent)).Scan(&exists); err != nil {
		return fmt.Errorf("apply %s %s: %w", kind, op.BlockID, translate(err))
	}
	if !exists {
		return fmt.Errorf("apply %s %s: parent %s: %w", kind, op.BlockID, op.Parent, knowledge.ErrStale)
	}
	return nil
}

// applyAdd inserts a new block appended under its parent, tagged with AI
// authorship and the proposal's source as provenance.
func applyAdd(ctx context.Context, tx *sql.Tx, op knowledge.Op, source knowledge.Source, now string) error {
	if err := requireParent(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	// A topic root has NULL parent_id; `IS ?` matches both NULL and a value.
	var parent any
	if op.Parent != "" {
		parent = string(op.Parent)
	}
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position)+1, 0) FROM blocks WHERE parent_id IS ?`,
		parent).Scan(&pos); err != nil {
		return fmt.Errorf("apply add %s: next position: %w", op.BlockID, translate(err))
	}
	meta := knowledge.Metadata{
		knowledge.MetaKeyAuthor: string(knowledge.AuthorAI),
		"source":                string(source),
	}
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
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	return nil
}

// applyEdit replaces a block's content, refreshing updated_at. An accepted
// proposal makes the AI the author of the new content, even where the owner
// had written the old (see metaAuthorPath).
func applyEdit(ctx context.Context, tx *sql.Tx, op knowledge.Op, now string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, metadata = json_set(metadata, ?, ?), updated_at = ? WHERE id = ?`,
		op.Content, metaAuthorPath, string(knowledge.AuthorAI), now, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// applyMove reparents a block, appending it after its new siblings. It refuses
// a move into the block's own subtree (see rejectMoveCycle) before writing.
func applyMove(ctx context.Context, tx *sql.Tx, op knowledge.Op, now string) error {
	if err := requireParent(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	if err := rejectMoveCycle(ctx, tx, op); err != nil {
		return err
	}
	var parent any
	if op.Parent != "" {
		parent = string(op.Parent)
	}
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position)+1, 0) FROM blocks WHERE parent_id IS ?`,
		parent).Scan(&pos); err != nil {
		return fmt.Errorf("apply move %s: next position: %w", op.BlockID, translate(err))
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE blocks SET parent_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		parent, pos, now, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// rejectMoveCycle returns ErrCycle when reparenting op.BlockID under op.Parent
// would place the block inside its own subtree. It walks the ancestor chain of
// the new parent (the parent itself is the seed) up through parent_id; reaching
// the moved block means the block is an ancestor-or-self of its new parent — a
// ring. A move to a topic root (empty parent) can never cycle.
func rejectMoveCycle(ctx context.Context, tx *sql.Tx, op knowledge.Op) error {
	if op.Parent == "" {
		return nil
	}
	var cycle bool
	if err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE ancestors(id) AS (
		     SELECT ?
		     UNION ALL
		     SELECT b.parent_id FROM blocks b JOIN ancestors a ON b.id = a.id
		     WHERE b.parent_id IS NOT NULL
		 )
		 SELECT EXISTS(SELECT 1 FROM ancestors WHERE id = ?)`,
		string(op.Parent), string(op.BlockID)).Scan(&cycle); err != nil {
		return fmt.Errorf("apply move %s: cycle check: %w", op.BlockID, translate(err))
	}
	if cycle {
		return fmt.Errorf("apply move %s under %s: %w", op.BlockID, op.Parent, knowledge.ErrCycle)
	}
	return nil
}

// requireBlock returns ErrStale when a referenced block is absent — the same
// "base moved" signal a vanished parent gives, so callers branch on ErrStale
// instead of a raw foreign-key error. An empty id (no reference) is fine.
func requireBlock(ctx context.Context, tx *sql.Tx, kind knowledge.OpKind, id string) error {
	if id == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM blocks WHERE id = ?)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("apply %s: block %s: %w", kind, id, translate(err))
	}
	if !exists {
		return fmt.Errorf("apply %s: block %s: %w", kind, id, knowledge.ErrStale)
	}
	return nil
}

// applyLink records a non-hierarchical edge between two existing blocks. Either
// endpoint gone maps to ErrStale (the base moved), not a raw foreign-key error;
// the relation defaults to "related" when the op leaves it unset, matching the
// domain's default and the schema's CHECK.
func applyLink(ctx context.Context, tx *sql.Tx, op knowledge.Op, now string) error {
	if err := requireBlock(ctx, tx, op.Kind, op.From); err != nil {
		return err
	}
	if err := requireBlock(ctx, tx, op.Kind, op.To); err != nil {
		return err
	}
	relation := op.Relation
	if relation == "" {
		relation = knowledge.RelationRelated
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO edges (from_id, to_id, relation, created_at) VALUES (?, ?, ?, ?)`,
		op.From, op.To, string(relation), now); err != nil {
		return fmt.Errorf("apply link %s->%s: %w", op.From, op.To, translate(err))
	}
	return nil
}

// applyDelete removes a block. The schema has no ON DELETE CASCADE, so a block
// with surviving children or edges trips a foreign-key error and rolls the
// apply back; the domain must delete descendants and links in the same ops.
func applyDelete(ctx context.Context, tx *sql.Tx, op knowledge.Op) error {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM blocks WHERE id = ?`, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply delete %s: %w", op.BlockID, translate(err))
	}
	return affectedOne(res, op.Kind, op.BlockID)
}

// affectedOne maps a no-op edit/move/delete to ErrStale: the target block
// vanished between the proposal and its apply, so the proposal is out of date.
func affectedOne(res sql.Result, kind knowledge.OpKind, id knowledge.ID) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply %s %s: %w", kind, id, translate(err))
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
