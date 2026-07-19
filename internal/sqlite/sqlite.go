// Package sqlite is the storage adapter: the knowledge.Repository
// implementation on top of SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	db    *sql.DB
	now   func() time.Time
	newID func() knowledge.ID
}

var _ knowledge.Repository = (*Repo)(nil)

// NewRepo wraps an opened database (see Open) in the Repository
// implementation. now stamps revisions (nil = time.Now); newID mints
// revision ids and must never collide (production injects a ULID
// generator — the same one handed to knowledge.Service).
func NewRepo(db *sql.DB, now func() time.Time, newID func() knowledge.ID) *Repo {
	if now == nil {
		now = time.Now
	}
	return &Repo{db: db, now: now, newID: newID}
}

// timeLayout is the on-disk format for revision and block timestamps:
// fixed-width, zero-padded RFC3339 nanoseconds in UTC. Fixed width keeps
// lexicographic order equal to chronological order — which ORDER BY on a
// TEXT timestamp column relies on — since time.RFC3339Nano trims trailing
// zeros and would break it.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// batch bundles the values every op writer needs beyond its own op: the
// group and provenance shared by the whole ApplyOps call, the review state
// that provenance implies, and the timestamp string stamped on every row
// the batch writes.
type batch struct {
	groupID  knowledge.ID
	meta     knowledge.WriteMeta
	reviewed bool
	now      string
}

// ApplyOps implements knowledge.Repository. It applies the domain's ready,
// ordered op list in one immediate transaction: one revision row per touched
// block plus the blocks projection, and one revision_groups row tying them
// together. Ops carry final identifiers (BlockID is the target block — the
// new block for add; Parent/From/To are resolved ids), so storage only
// writes. The apply is atomic: any error rolls the whole transaction back,
// so search and the tree never observe a half-applied batch.
func (r *Repo) ApplyOps(ctx context.Context, groupID knowledge.ID, meta knowledge.WriteMeta, ops []knowledge.Op) error {
	// _txlock=immediate (DSN) makes this a BEGIN IMMEDIATE: the write lock is
	// taken up front, so a read-then-write apply cannot deadlock on a lock
	// upgrade under WAL.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}
	defer tx.Rollback()

	b := batch{
		groupID: groupID,
		meta:    meta,
		now:     formatTime(r.now()),
		// Owner writes are trusted and need no review; AI writes wait in the
		// Changes queue until the human accepts them (Task 4).
		reviewed: meta.Author == knowledge.AuthorOwner,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO revision_groups (id, title, source, created_at) VALUES (?, ?, ?, ?)`,
		string(groupID), meta.Title, string(meta.Source), b.now); err != nil {
		return fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}

	for _, op := range ops {
		if err := r.applyOp(ctx, tx, op, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// applyOp applies one resolved operation inside the apply transaction.
func (r *Repo) applyOp(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	switch op.Kind {
	case knowledge.OpAdd:
		return r.applyAdd(ctx, tx, op, b)
	case knowledge.OpEdit:
		return r.applyEdit(ctx, tx, op, b)
	case knowledge.OpMove:
		return r.applyMove(ctx, tx, op, b)
	case knowledge.OpDelete:
		return r.applyDelete(ctx, tx, op, b)
	case knowledge.OpLink:
		return r.applyLink(ctx, tx, op, b)
	case knowledge.OpUnlink:
		return r.applyUnlink(ctx, tx, op, b)
	default:
		return fmt.Errorf("apply: unsupported op kind %q", op.Kind)
	}
}

// requireParent returns ErrNotFound when a non-root op names a parent that
// does not exist. Root ops (empty Parent) have nothing to check. Domain
// ordering applies parents before children, so an intra-batch parent already
// exists by the time its children run.
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
		return fmt.Errorf("apply %s %s: parent %s: %w", kind, op.BlockID, op.Parent, knowledge.ErrNotFound)
	}
	return nil
}

// applyAdd inserts a new block appended under its parent, tagged with the
// batch's authorship and source as provenance, and writes its creation
// revision (seq 1).
func (r *Repo) applyAdd(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
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
	stamp := knowledge.Metadata{"author": string(b.meta.Author), "source": string(b.meta.Source)}
	if op.Origin != "" {
		stamp["origin"] = op.Origin
	}
	payload, err := json.Marshal(stamp)
	if err != nil {
		return fmt.Errorf("apply add %s: metadata: %w", op.BlockID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(op.BlockID), parent, pos, op.Content, string(payload), b.now, b.now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	rev := knowledge.Revision{
		ID:        r.newID(),
		BlockID:   op.BlockID,
		Seq:       1,
		Content:   op.Content,
		ParentID:  knowledge.ID(op.Parent),
		Position:  pos,
		Author:    b.meta.Author,
		Source:    b.meta.Source,
		Origin:    op.Origin,
		Rationale: op.Rationale,
		Reviewed:  b.reviewed,
		GroupID:   b.groupID,
	}
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	return nil
}

// applyEdit replaces a block's content, refreshing updated_at, and appends a
// revision that carries the block's unchanged parent/position alongside the
// new content — a revision is a full snapshot, not a diff.
func (r *Repo) applyEdit(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	var parent sql.NullString
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT parent_id, position FROM blocks WHERE id = ?`, string(op.BlockID)).Scan(&parent, &pos); err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	seq, err := nextSeq(ctx, tx, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply edit %s: next seq: %w", op.BlockID, translate(err))
	}
	rev := knowledge.Revision{
		ID:        r.newID(),
		BlockID:   op.BlockID,
		Seq:       seq,
		Content:   op.Content,
		ParentID:  knowledge.ID(parent.String),
		Position:  pos,
		Author:    b.meta.Author,
		Source:    b.meta.Source,
		Origin:    op.Origin,
		Rationale: op.Rationale,
		Reviewed:  b.reviewed,
		GroupID:   b.groupID,
	}
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, updated_at = ? WHERE id = ?`,
		op.Content, b.now, string(op.BlockID)); err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	return nil
}

// applyMove reparents a block, appending it after its new siblings, and
// writes a revision that carries the block's unchanged content alongside the
// new parent/position. It refuses a move into the block's own subtree (see
// rejectMoveCycle) before writing.
func (r *Repo) applyMove(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	if err := requireParent(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	if err := rejectMoveCycle(ctx, tx, op); err != nil {
		return err
	}
	var content string
	if err := tx.QueryRowContext(ctx,
		`SELECT content FROM blocks WHERE id = ?`, string(op.BlockID)).Scan(&content); err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
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
	seq, err := nextSeq(ctx, tx, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply move %s: next seq: %w", op.BlockID, translate(err))
	}
	rev := knowledge.Revision{
		ID:        r.newID(),
		BlockID:   op.BlockID,
		Seq:       seq,
		Content:   content,
		ParentID:  knowledge.ID(op.Parent),
		Position:  pos,
		Author:    b.meta.Author,
		Source:    b.meta.Source,
		Origin:    op.Origin,
		Rationale: op.Rationale,
		Reviewed:  b.reviewed,
		GroupID:   b.groupID,
	}
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET parent_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		parent, pos, b.now, string(op.BlockID)); err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
	}
	return nil
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

// requireBlock returns ErrNotFound when a referenced block is absent. An
// empty id (no reference) is fine.
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
		return fmt.Errorf("apply %s: block %s: %w", kind, id, knowledge.ErrNotFound)
	}
	return nil
}

// applyLink upserts a non-hierarchical edge between two existing blocks.
// Either endpoint gone maps to ErrNotFound; the relation defaults to
// "related" when the op leaves it unset, matching the domain's default and
// the schema's CHECK. The upsert resurrects a soft-deleted edge (deleted=0)
// and refreshes its review state and group — re-linking is how the domain
// restores a link an owner had rejected — while an edge the owner
// hand-created (AddEdge, reviewed=1) stays reviewed only if this write is
// also an owner write; an AI re-link of an owner edge intentionally regresses
// it to unreviewed, since the link itself changed provenance.
func (r *Repo) applyLink(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
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
	var origin any
	if op.Origin != "" {
		origin = op.Origin
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO edges (from_id, to_id, relation, author, origin, reviewed, deleted, group_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT (from_id, to_id, relation)
		DO UPDATE SET deleted = 0, reviewed = excluded.reviewed, group_id = excluded.group_id`,
		op.From, op.To, string(relation), string(b.meta.Author), origin, b.reviewed, string(b.groupID), b.now); err != nil {
		return fmt.Errorf("apply link %s->%s: %w", op.From, op.To, translate(err))
	}
	return nil
}

// applyUnlink soft-deletes an edge as part of a write batch — the mirror of
// applyLink. An owner write removes the link at once (born reviewed); an AI
// write leaves a pending removal (unreviewed, deleted) that GetEdges already
// hides but review can still reject to restore. Unlinking an absent edge is a
// no-op: there is nothing to hide, and the batch should not fail on it.
func (r *Repo) applyUnlink(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	relation := op.Relation
	if relation == "" {
		relation = knowledge.RelationRelated
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE edges SET deleted = 1, reviewed = ?, group_id = ?
		WHERE from_id = ? AND to_id = ? AND relation = ?`,
		b.reviewed, string(b.groupID), op.From, op.To, string(relation)); err != nil {
		return fmt.Errorf("apply unlink %s->%s: %w", op.From, op.To, translate(err))
	}
	return nil
}

// applyDelete tombstones a block and its whole subtree, children-first: each
// touched block gets a tombstone revision (its current snapshot, Deleted
// true) before its blocks row is removed. Edges are left untouched — reads
// hide edges with a dead endpoint (Task 4), so restoring the block restores
// its links without storage having to remember them here.
func (r *Repo) applyDelete(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	subtree, err := subtreePreorder(ctx, tx, op.BlockID)
	if err != nil {
		return fmt.Errorf("apply delete %s: %w", op.BlockID, err)
	}
	if len(subtree) == 0 {
		return fmt.Errorf("apply delete %s: %w", op.BlockID, knowledge.ErrNotFound)
	}
	slices.Reverse(subtree) // children before their parents

	for _, blk := range subtree {
		seq, err := nextSeq(ctx, tx, string(blk.ID))
		if err != nil {
			return fmt.Errorf("apply delete %s: next seq: %w", blk.ID, translate(err))
		}
		rev := knowledge.Revision{
			ID:        r.newID(),
			BlockID:   blk.ID,
			Seq:       seq,
			Content:   blk.Content,
			ParentID:  blk.ParentID,
			Position:  blk.Position,
			Deleted:   true,
			Author:    b.meta.Author,
			Source:    b.meta.Source,
			Origin:    op.Origin,
			Rationale: op.Rationale,
			Reviewed:  b.reviewed,
			GroupID:   b.groupID,
		}
		if err := insertRevision(ctx, tx, rev, b.now); err != nil {
			return fmt.Errorf("apply delete %s: %w", blk.ID, translate(err))
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM blocks WHERE id = ?`, string(blk.ID)); err != nil {
			return fmt.Errorf("apply delete %s: %w", blk.ID, translate(err))
		}
	}
	return nil
}

// subtreePreorder collects root and every descendant inside tx, ordered
// parents-before-children — the tx-scoped twin of GetSubtree, needed because
// a read through r.db might land on a different pooled connection and miss
// this transaction's uncommitted writes.
func subtreePreorder(ctx context.Context, tx *sql.Tx, root knowledge.ID) ([]knowledge.Block, error) {
	const q = `
WITH RECURSIVE subtree(id) AS (
    SELECT id FROM blocks WHERE id = ?
    UNION
    SELECT b.id FROM blocks b JOIN subtree s ON b.parent_id = s.id
)
SELECT ` + blockColumns + ` FROM blocks WHERE id IN (SELECT id FROM subtree)`

	rows, err := tx.QueryContext(ctx, q, string(root))
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	blocks, err := scanBlocks(rows)
	if err != nil {
		return nil, translate(err)
	}
	return preorder(blocks, root), nil
}
