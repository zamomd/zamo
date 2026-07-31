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
	"strings"
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

// isDuplicateTopic reports the partial unique index on blocks.title_key
// firing. SQLite names the offending column rather than the index, so the
// column is what the message can be recognised by.
func isDuplicateTopic(err error) bool {
	e, ok := errors.AsType[*sqlite3.Error](err)
	return ok && strings.Contains(e.Error(), "UNIQUE constraint failed: blocks.title_key")
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
	case isDuplicateTopic(err):
		return knowledge.ErrDuplicateTopic
	default:
		return err
	}
}

// titleKey is the value blocks.title_key takes for a block: the normalized
// first line of a topic root's content, and NULL for everything else. A root
// whose first line normalizes to nothing is unnamed, which is also NULL — the
// partial unique index admits any number of those.
func titleKey(parent any, content string) any {
	if parent != nil {
		return nil
	}
	key := knowledge.NormalizeTitle(content)
	if key == "" {
		return nil
	}
	return key
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

// queryer is the multi-row read surface shared by *sql.DB and *sql.Tx, so a
// helper works standalone or inside a write transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryIDs runs a single-column query and collects that column. Errors come
// back already translated; the caller adds its own context.
func queryIDs(ctx context.Context, q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, translate(err)
		}
		ids = append(ids, id)
	}
	return ids, translate(rows.Err())
}

// withTx runs fn in one transaction, committing when it returns nil and rolling
// back otherwise. _txlock=immediate (DSN, see Open) makes this a BEGIN
// IMMEDIATE: the write lock is taken up front, so a read-then-write body cannot
// deadlock on a lock upgrade under WAL. what prefixes every error the call
// produces; fn returns its errors already translated.
func (r *Repo) withTx(ctx context.Context, what string, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", what, translate(err))
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", what, translate(err))
	}
	return nil
}

// subtreeCTE selects a root and every live descendant into a subtree(id)
// relation. UNION, not UNION ALL: on a database whose parent_id somehow became
// cyclic, UNION discards the repeated row and terminates instead of looping
// forever (ARCHITECTURE.md §5.2).
const subtreeCTE = `
WITH RECURSIVE subtree(id) AS (
    SELECT id FROM blocks WHERE id = ?
    UNION
    SELECT b.id FROM blocks b JOIN subtree s ON b.parent_id = s.id
)`

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

// revision builds the snapshot row for one touched block, filling in the
// provenance the whole batch shares. The caller supplies what is the block's
// own: its identity, sequence, and the content/parent/position the op leaves
// it with — a revision is a full snapshot, not a diff.
func (b batch) revision(id knowledge.ID, op knowledge.Op, seq int, snap knowledge.Revision) knowledge.Revision {
	snap.ID = id
	snap.Seq = seq
	snap.Author = b.meta.Author
	snap.Source = b.meta.Source
	snap.Origin = op.Origin
	snap.Rationale = op.Rationale
	snap.Reviewed = b.reviewed
	snap.GroupID = b.groupID
	return snap
}

// ApplyOps implements knowledge.Repository. It applies the domain's ready,
// ordered op list in one immediate transaction: one revision row per touched
// block plus the blocks projection, and one revision_groups row tying them
// together. Ops carry final identifiers (BlockID is the target block — the
// new block for add; Parent/From/To are resolved ids), so storage only
// writes. The apply is atomic: any error rolls the whole transaction back,
// so search and the tree never observe a half-applied batch.
func (r *Repo) ApplyOps(ctx context.Context, groupID knowledge.ID, meta knowledge.WriteMeta, ops []knowledge.Op) error {
	b := batch{
		groupID: groupID,
		meta:    meta,
		now:     formatTime(r.now()),
		// Owner writes are trusted and need no review; AI writes wait in the
		// Changes queue until the human accepts them (Task 4).
		reviewed: meta.Author == knowledge.AuthorOwner,
	}
	// Not withTx: applyOp's errors already name the op and the block, and a
	// second "apply ops group …" in front of them would say nothing.
	// _txlock=immediate (DSN) makes this a BEGIN IMMEDIATE, so a read-then-write
	// apply cannot deadlock on a lock upgrade under WAL.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}
	defer tx.Rollback()

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

// requireAnchor rejects an op whose sibling anchor is missing or belongs to a
// different parent. Without this an unknown anchor would fall through to a
// plain append: the batch would succeed while putting the block somewhere the
// caller did not ask for.
func requireAnchor(ctx context.Context, tx *sql.Tx, kind knowledge.OpKind, op knowledge.Op) error {
	anchor := op.After
	if anchor == "" {
		anchor = op.Before
	}
	if anchor == "" {
		return nil
	}
	var parent sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT parent_id FROM blocks WHERE id = ?`, anchor).Scan(&parent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("apply %s %s: anchor %s: %w", kind, op.BlockID, anchor, knowledge.ErrNotFound)
		}
		return fmt.Errorf("apply %s %s: anchor %s: %w", kind, op.BlockID, anchor, translate(err))
	}
	if parent.String != op.Parent {
		return fmt.Errorf("apply %s %s: anchor %s is not a child of %q: %w",
			kind, op.BlockID, anchor, op.Parent, knowledge.ErrInvalidOps)
	}
	return nil
}

// applyAdd inserts a new block, appended under its parent or inserted next to
// an anchor sibling when op.After or op.Before is set, tagged with the
// batch's authorship and source as provenance, and writes its creation
// revision (seq 1).
func (r *Repo) applyAdd(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	if err := requireParent(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	if err := requireAnchor(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	// A topic root has NULL parent_id; `IS ?` matches both NULL and a value.
	var parent any
	if op.Parent != "" {
		parent = string(op.Parent)
	}
	pos, err := resolvePosition(ctx, tx, parent, op)
	if err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, err)
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
		`INSERT INTO blocks (id, parent_id, position, content, title_key, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(op.BlockID), parent, pos, op.Content, titleKey(parent, op.Content),
		string(payload), b.now, b.now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	rev := b.revision(r.newID(), op, 1, knowledge.Revision{
		BlockID:  op.BlockID,
		Content:  op.Content,
		ParentID: knowledge.ID(op.Parent),
		Position: pos,
	})
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
	rev := b.revision(r.newID(), op, seq, knowledge.Revision{
		BlockID:  op.BlockID,
		Content:  op.Content,
		ParentID: knowledge.ID(parent.String),
		Position: pos,
	})
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	var parentArg any
	if parent.Valid && parent.String != "" {
		parentArg = parent.String
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, title_key = ?, updated_at = ? WHERE id = ?`,
		op.Content, titleKey(parentArg, op.Content), b.now, string(op.BlockID)); err != nil {
		return fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	return nil
}

// applyMove reparents a block, appending it after its new siblings or
// inserting it next to an anchor sibling when op.After or op.Before is set,
// and writes a revision that carries the block's unchanged content alongside
// the new parent/position. It refuses a move into the block's own subtree
// (see rejectMoveCycle) before writing.
func (r *Repo) applyMove(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) error {
	if err := requireParent(ctx, tx, op.Kind, op); err != nil {
		return err
	}
	if err := requireAnchor(ctx, tx, op.Kind, op); err != nil {
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
	pos, err := resolvePosition(ctx, tx, parent, op)
	if err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, err)
	}
	seq, err := nextSeq(ctx, tx, string(op.BlockID))
	if err != nil {
		return fmt.Errorf("apply move %s: next seq: %w", op.BlockID, translate(err))
	}
	rev := b.revision(r.newID(), op, seq, knowledge.Revision{
		BlockID:  op.BlockID,
		Content:  content,
		ParentID: knowledge.ID(op.Parent),
		Position: pos,
	})
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET parent_id = ?, position = ?, title_key = ?, updated_at = ? WHERE id = ?`,
		parent, pos, titleKey(parent, content), b.now, string(op.BlockID)); err != nil {
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

// applyDelete tombstones a block and its whole subtree, children-first: each
// touched block gets a tombstone revision (its current snapshot, Deleted
// true) before its blocks row is removed.
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
		rev := b.revision(r.newID(), op, seq, knowledge.Revision{
			BlockID:  blk.ID,
			Content:  blk.Content,
			ParentID: blk.ParentID,
			Position: blk.Position,
			Deleted:  true,
		})
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
	rows, err := tx.QueryContext(ctx, subtreeQuery, string(root))
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
