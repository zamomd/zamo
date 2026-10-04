// Package sqlite is the storage adapter: the knowledge.Repository
// implementation on top of SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	sqlite3 "modernc.org/sqlite"

	"github.com/zamomd/zamo/internal/knowledge"
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

// titleKey is the value blocks.title_key takes for a block: the key a topic
// root is addressable by (knowledge.TopicKey), and NULL for everything else.
// A root nothing can reference — an empty name, or one carrying a bracket — is
// unnamed, which is also NULL; the partial unique index admits any number of
// those, so such titles are ordinary content rather than a refused write.
func titleKey(parent any, content string) any {
	if parent != nil {
		return nil
	}
	key := knowledge.TopicKey(content)
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
// new block for add; Parent/From/To are resolved ids), so the only reading
// storage does on its own account is the optimistic guard an op may carry (see
// guard). The apply is atomic: any error rolls the whole transaction back, so
// search and the tree never observe a half-applied batch.
func (r *Repo) ApplyOps(
	ctx context.Context, groupID knowledge.ID, meta knowledge.WriteMeta, ops []knowledge.Op,
) (changed bool, err error) {
	b := batch{
		groupID: groupID,
		meta:    meta,
		now:     formatTime(r.now()),
		// Owner writes are trusted and need no review; AI writes, and the owner
		// writes that ask for it, wait in the Changes queue until the human
		// accepts them.
		reviewed: meta.BornReviewed(),
	}
	// Not withTx: applyOp's errors already name the op and the block, and a
	// second "apply ops group …" in front of them would say nothing.
	// _txlock=immediate (DSN) makes this a BEGIN IMMEDIATE, so a read-then-write
	// apply cannot deadlock on a lock upgrade under WAL.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}
	defer tx.Rollback()
	if err := guard(ctx, tx, ops); err != nil {
		return false, err
	}
	if err := refuseOverUnreviewed(ctx, tx, meta.RefuseOverUnreviewedIn); err != nil {
		return false, fmt.Errorf("apply ops group %s: %w", groupID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO revision_groups (id, title, source, atomic, created_at) VALUES (?, ?, ?, ?, ?)`,
		string(groupID), meta.Title, string(meta.Source), meta.Atomic, b.now); err != nil {
		return false, fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}
	for _, op := range ops {
		opChanged, err := r.applyOp(ctx, tx, op, b)
		if err != nil {
			return false, err
		}
		changed = changed || opChanged
	}
	if !changed {
		return false, nil
	}
	if err := refuseDocumentChildren(ctx, tx, ops); err != nil {
		return false, fmt.Errorf("apply ops group %s: %w", groupID, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("apply ops group %s: %w", groupID, translate(err))
	}
	return true, nil
}

// guard checks the optimistic preconditions a rename expansion attached to the
// batch (see knowledge.Op.ExpectedContent and ExpectedTopicRefs). It runs inside
// the apply transaction, which is what makes the check hold: BEGIN IMMEDIATE has
// the write lock already, so nothing can land between the check and the writes
// it clears the way for.
//
// A guarded block that is gone was written over as surely as one holding other
// text, so both answer ErrConcurrentWrite — what the caller has to do, prepare
// the rename again, is the same either way.
func guard(ctx context.Context, tx *sql.Tx, ops []knowledge.Op) error {
	for _, op := range ops {
		if expected, guarded := op.ExpectedContent(); guarded {
			var current string
			err := tx.QueryRowContext(ctx,
				`SELECT content FROM blocks WHERE id = ?`, string(op.BlockID)).Scan(&current)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("guard %s %s: %w", op.Kind, op.BlockID, translate(err))
			}
			if err != nil || current != expected {
				return fmt.Errorf("guard %s %s: %w", op.Kind, op.BlockID, knowledge.ErrConcurrentWrite)
			}
		}
		key, expected, guarded := op.ExpectedTopicRefs()
		if !guarded {
			continue
		}
		// Ordered by source_id to compare against BlocksReferencingTopic,
		// which orders the sources it hands the domain by block id.
		current, err := queryIDs(ctx, tx, `
SELECT source_id FROM refs WHERE kind = 'topic' AND target_key = ? ORDER BY source_id`, key)
		if err != nil {
			return fmt.Errorf("guard topic references %q: %w", key, err)
		}
		same := func(id string, want knowledge.ID) bool { return id == string(want) }
		if !slices.EqualFunc(current, expected, same) {
			return fmt.Errorf("guard topic references %q: %w", key, knowledge.ErrConcurrentWrite)
		}
	}
	return nil
}

// refuseDocumentChildren fails the batch when it leaves any block under a topic
// root in ModeDoc. Checked once after every op has run rather than per op: a
// conversion sets the mode and removes the children in one batch, and the order
// its ops happen to take must not decide whether it is legal.
//
// Scoped to the blocks the batch touched, on both sides of the parent link: an
// op can only create the pair by placing a child (the child is touched) or by
// making a parent a document (the parent is touched). A violation the batch did
// not cause is left alone: an existing inconsistency elsewhere must not refuse
// a write that never addressed it.
func refuseDocumentChildren(ctx context.Context, tx *sql.Tx, ops []knowledge.Op) error {
	touched := make([]any, 0, len(ops))
	seen := make(map[knowledge.ID]bool, len(ops))
	for _, op := range ops {
		if op.BlockID == "" || seen[op.BlockID] {
			continue
		}
		seen[op.BlockID] = true
		touched = append(touched, string(op.BlockID))
	}
	if len(touched) == 0 {
		return nil
	}
	list := strings.Repeat(",?", len(touched))[1:]
	args := append(slices.Clone(touched), touched...)
	var offender sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT c.id FROM blocks c JOIN blocks p ON p.id = c.parent_id
		WHERE p.mode = 'doc' AND (c.id IN (`+list+`) OR p.id IN (`+list+`)) LIMIT 1`,
		args...).Scan(&offender); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return translate(err)
	}
	if !offender.Valid {
		return nil
	}
	return fmt.Errorf("block %s: %w", offender.String, knowledge.ErrDocumentHasChildren)
}

// applyOp applies one resolved operation inside the apply transaction and
// reports whether it changed anything.
func (r *Repo) applyOp(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) (changed bool, err error) {
	switch op.Kind {
	case knowledge.OpAdd:
		return true, r.applyAdd(ctx, tx, op, b)
	case knowledge.OpEdit:
		return r.applyEdit(ctx, tx, op, b)
	case knowledge.OpMove:
		return true, r.applyMove(ctx, tx, op, b)
	case knowledge.OpDelete:
		return true, r.applyDelete(ctx, tx, op, b)
	default:
		return false, fmt.Errorf("apply: unsupported op kind %q", op.Kind)
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

// storedMode resolves the mode a write leaves on a block. Only a root has one,
// so a block with a parent is always ModeOutline — including a root moved under
// one, which stops being a topic. An op that names no mode leaves current
// standing.
func storedMode(parent any, current, asked knowledge.Mode) knowledge.Mode {
	if parent != nil {
		return knowledge.ModeOutline
	}
	if asked != "" {
		return asked
	}
	if current == "" {
		return knowledge.ModeOutline
	}
	return current
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
	key := titleKey(parent, op.Content)
	if err := requireNameFree(ctx, tx, op.BlockID, key); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, err)
	}
	mode := storedMode(parent, "", op.Mode)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (id, parent_id, position, content, title_key, mode, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(op.BlockID), parent, pos, op.Content, key, string(mode),
		b.now, b.now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	rev := b.revision(r.newID(), op, 1, knowledge.Revision{
		BlockID:  op.BlockID,
		Content:  op.Content,
		ParentID: knowledge.ID(op.Parent),
		Position: pos,
		Mode:     mode,
	})
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, translate(err))
	}
	if err := putRefs(ctx, tx, op.BlockID, op.Content); err != nil {
		return fmt.Errorf("apply add %s: %w", op.BlockID, err)
	}
	return nil
}

// applyEdit replaces a block's content, refreshing updated_at, and appends a
// revision that carries the block's unchanged parent/position alongside the
// new content — a revision is a full snapshot, not a diff.
//
// An edit names its target by id alone, so storage validates that a requested
// mode belongs to a topic root.
//
// An edit that leaves both the content and the mode as they are writes
// nothing: a revision is a change, and an identical one would queue a review
// of nothing.
func (r *Repo) applyEdit(ctx context.Context, tx *sql.Tx, op knowledge.Op, b batch) (changed bool, err error) {
	var parent sql.NullString
	var pos int
	var current knowledge.Mode
	var content string
	if err := tx.QueryRowContext(ctx,
		`SELECT parent_id, position, mode, content FROM blocks WHERE id = ?`,
		string(op.BlockID)).Scan(&parent, &pos, &current, &content); err != nil {
		return false, fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	var parentArg any
	if parent.Valid && parent.String != "" {
		parentArg = parent.String
	}
	if op.Mode != "" && parentArg != nil {
		return false, fmt.Errorf("apply edit %s: only a topic root has a mode, and %s is a child of %s: %w",
			op.BlockID, op.BlockID, parent.String, knowledge.ErrInvalidOps)
	}
	mode := storedMode(parentArg, current, op.Mode)
	if op.Content == content && mode == current {
		return false, nil
	}
	seq, err := nextSeq(ctx, tx, string(op.BlockID))
	if err != nil {
		return false, fmt.Errorf("apply edit %s: next seq: %w", op.BlockID, translate(err))
	}
	rev := b.revision(r.newID(), op, seq, knowledge.Revision{
		BlockID:  op.BlockID,
		Content:  op.Content,
		ParentID: knowledge.ID(parent.String),
		Position: pos,
		Mode:     mode,
	})
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return false, fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	key := titleKey(parentArg, op.Content)
	if err := requireNameFree(ctx, tx, op.BlockID, key); err != nil {
		return false, fmt.Errorf("apply edit %s: %w", op.BlockID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, title_key = ?, mode = ?, updated_at = ? WHERE id = ?`,
		op.Content, key, string(mode), b.now, string(op.BlockID)); err != nil {
		return false, fmt.Errorf("apply edit %s: %w", op.BlockID, translate(err))
	}
	if err := putRefs(ctx, tx, op.BlockID, op.Content); err != nil {
		return false, fmt.Errorf("apply edit %s: %w", op.BlockID, err)
	}
	return true, nil
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
	var current knowledge.Mode
	if err := tx.QueryRowContext(ctx,
		`SELECT content, mode FROM blocks WHERE id = ?`, string(op.BlockID)).Scan(&content, &current); err != nil {
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
	mode := storedMode(parent, current, op.Mode)
	key := titleKey(parent, content)
	if err := requireNameFree(ctx, tx, op.BlockID, key); err != nil {
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
		Mode:     mode,
	})
	if err := insertRevision(ctx, tx, rev, b.now); err != nil {
		return fmt.Errorf("apply move %s: %w", op.BlockID, translate(err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET parent_id = ?, position = ?, title_key = ?, mode = ?, updated_at = ? WHERE id = ?`,
		parent, pos, key, string(mode), b.now, string(op.BlockID)); err != nil {
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
			Mode:     blk.Mode,
			Deleted:  true,
		})
		if err := insertRevision(ctx, tx, rev, b.now); err != nil {
			return fmt.Errorf("apply delete %s: %w", blk.ID, translate(err))
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM blocks WHERE id = ?`, string(blk.ID)); err != nil {
			return fmt.Errorf("apply delete %s: %w", blk.ID, translate(err))
		}
		if err := putRefs(ctx, tx, blk.ID, ""); err != nil {
			return fmt.Errorf("apply delete %s: %w", blk.ID, err)
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
