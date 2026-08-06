package sqlite

// Revision helpers shared by writes and review operations.

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
)

// revisionSelect is the block_revisions projection every read shares, in the
// order scanRevision expects. The group is joined in for its atomic flag: a
// revision carries whether its batch has to be reviewed whole, so no caller
// has to look that up separately and none can forget to.
const revisionSelect = `
SELECT r.id, r.block_id, r.seq, r.content, r.parent_id, r.position, r.mode,
       r.deleted, r.author, r.source, r.origin, r.rationale, r.reviewed,
       r.group_id, r.created_at, COALESCE(g.atomic, 0)
FROM block_revisions r LEFT JOIN revision_groups g ON g.id = r.group_id`

// rowQueryer is the read surface shared by *sql.DB and *sql.Tx, so
// headRevision works standalone (a plain read) or inside a write
// transaction (rebuilding the projection after an insert).
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is implemented by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// nextSeq returns the next per-block sequence number inside tx. The first
// revision of a block is seq 1.
func nextSeq(ctx context.Context, tx *sql.Tx, blockID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM block_revisions WHERE block_id = ?`,
		blockID).Scan(&n)
	return n, err
}

// insertRevision writes one snapshot row. ParentID is empty for a topic root.
func insertRevision(ctx context.Context, tx *sql.Tx, rev knowledge.Revision, now string) error {
	var parent any
	if rev.ParentID != "" {
		parent = string(rev.ParentID)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO block_revisions
		(id, block_id, seq, content, parent_id, position, mode, deleted, author,
		 source, origin, rationale, reviewed, group_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(rev.ID), string(rev.BlockID), rev.Seq, rev.Content, parent,
		rev.Position, string(rev.Mode), rev.Deleted, string(rev.Author), string(rev.Source),
		rev.Origin, rev.Rationale, rev.Reviewed, string(rev.GroupID), now)
	return err
}

// headRevision returns the block's highest-seq revision, or ok=false when
// the block has none.
func headRevision(ctx context.Context, q rowQueryer, blockID string) (knowledge.Revision, bool, error) {
	row := q.QueryRowContext(ctx,
		revisionSelect+` WHERE r.block_id = ? ORDER BY r.seq DESC LIMIT 1`, blockID)
	rev, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return knowledge.Revision{}, false, nil
	}
	if err != nil {
		return knowledge.Revision{}, false, err
	}
	return rev, true, nil
}

// scanRevision maps one revisionSelect row into a domain Revision: NULL
// parent_id becomes an empty ID (a topic root), the nullable provenance
// columns (source, origin, rationale, group_id) become their zero value, and
// created_at is parsed via parseTime.
func scanRevision(row scanner) (knowledge.Revision, error) {
	var (
		rev                                      knowledge.Revision
		id, blockID, author                      string
		parent, source, origin, rationale, group sql.NullString
		created                                  string
	)
	if err := row.Scan(&id, &blockID, &rev.Seq, &rev.Content, &parent, &rev.Position,
		&rev.Mode, &rev.Deleted, &author, &source, &origin, &rationale, &rev.Reviewed,
		&group, &created, &rev.Atomic); err != nil {
		return knowledge.Revision{}, err
	}
	rev.ID = knowledge.ID(id)
	rev.BlockID = knowledge.ID(blockID)
	rev.ParentID = knowledge.ID(parent.String)
	rev.Author = knowledge.Author(author)
	rev.Source = knowledge.Source(source.String)
	rev.Origin = origin.String
	rev.Rationale = rationale.String
	rev.GroupID = knowledge.ID(group.String)
	t, err := parseTime(created)
	if err != nil {
		return knowledge.Revision{}, fmt.Errorf("parse created_at of revision %s: %w", id, err)
	}
	rev.CreatedAt = t
	return rev, nil
}

// projectHead rebuilds a block from its current head revision. A live head
// requires a live parent, and a block with live children cannot be removed.
// Timestamps come from revision history so rollback reproduces historical state.
//
// It restores every piece of state derived from content, not just the row:
// blocks_fts through the normal triggers, blocks.title_key computed here, and
// the block's refs rows rebuilt from the projected content — emptied when the
// head is a deletion. Dropping either of the last two is silent: the tree still
// renders while the topic loses its name and every backlink to it.
func projectHead(ctx context.Context, tx *sql.Tx, blockID string) error {
	head, ok, err := headRevision(ctx, tx, blockID)
	if err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	if !ok || head.Deleted {
		var kids bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM blocks WHERE parent_id = ?)`,
			blockID).Scan(&kids); err != nil {
			return fmt.Errorf("project head of %s: %w", blockID, translate(err))
		}
		if kids {
			return fmt.Errorf("block %s: delete or move children first: %w",
				blockID, knowledge.ErrInvalidRollback)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM blocks WHERE id = ?`, blockID); err != nil {
			return fmt.Errorf("project head of %s: %w", blockID, translate(err))
		}
		if err := putRefs(ctx, tx, knowledge.ID(blockID), ""); err != nil {
			return fmt.Errorf("project head of %s: %w", blockID, err)
		}
		return nil
	}
	if head.ParentID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM blocks WHERE id = ?)`,
			string(head.ParentID)).Scan(&exists); err != nil {
			return fmt.Errorf("project head of %s: %w", blockID, translate(err))
		}
		if !exists {
			return fmt.Errorf("block %s: restore parent %s first: %w",
				blockID, head.ParentID, knowledge.ErrInvalidRollback)
		}
	}
	var first string
	if err := tx.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM block_revisions WHERE block_id = ?`,
		blockID).Scan(&first); err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	var parent any
	if head.ParentID != "" {
		parent = string(head.ParentID)
	}
	key := titleKey(parent, head.Content)
	mode := storedMode(parent, head.Mode, "")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blocks (id, parent_id, position, content, title_key, mode, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '{}', ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			parent_id = excluded.parent_id, position = excluded.position,
			content = excluded.content, title_key = excluded.title_key,
			mode = excluded.mode, updated_at = excluded.updated_at`,
		blockID, parent, head.Position, head.Content, key, string(mode),
		first, formatTime(head.CreatedAt)); err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	if err := putRefs(ctx, tx, knowledge.ID(blockID), head.Content); err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, err)
	}
	return nil
}

// DeleteRevision removes a revision and rebuilds the projection when the head
// changes. Invalid projections roll back the transaction.
//
// An unreviewed revision of an atomic group is refused: deleting it is a review
// decision on that block alone by another route. Reverting the rename of a
// topic would leave the blocks it rewrote pending on their own, which is
// exactly what refuseSplitGroup then reads as a whole group and accepts —
// ending in a topic under its old name and accepted text pointing nowhere.
//
// The reviewed case is left open. Editing history can split a group that was
// already decided on too, but that is a property of history editing rather
// than of the review queue, and needs its own answer.
func (r *Repo) DeleteRevision(ctx context.Context, blockID, revID knowledge.ID) error {
	what := fmt.Sprintf("delete revision %s of %s", revID, blockID)
	return r.withTx(ctx, what, func(tx *sql.Tx) error {
		var seq int
		var reviewed, atomic bool
		if err := tx.QueryRowContext(ctx, `
			SELECT r.seq, r.reviewed, COALESCE(g.atomic, 0)
			FROM block_revisions r LEFT JOIN revision_groups g ON g.id = r.group_id
			WHERE r.id = ? AND r.block_id = ?`,
			string(revID), string(blockID)).Scan(&seq, &reviewed, &atomic); err != nil {
			return translate(err)
		}
		if !reviewed && atomic {
			return fmt.Errorf("revision %s of %s is part of a group awaiting review: %w",
				revID, blockID, knowledge.ErrAtomicGroup)
		}
		var maxSeq int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(seq), 0) FROM block_revisions WHERE block_id = ?`,
			string(blockID)).Scan(&maxSeq); err != nil {
			return translate(err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM block_revisions WHERE id = ?`, string(revID)); err != nil {
			return translate(err)
		}
		if seq != maxSeq {
			return nil
		}
		return translate(projectHead(ctx, tx, string(blockID)))
	})
}

// RejectBlock implements knowledge.Repository: drops the block's unreviewed
// head revisions (everything after the last reviewed one) and rebuilds the
// projection. A fully reviewed block — or an unknown one — has nothing past
// its last reviewed revision to drop, so it is a no-op.
func (r *Repo) RejectBlock(ctx context.Context, blockID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("reject block %s", blockID), func(tx *sql.Tx) error {
		if err := refuseSplitGroup(ctx, tx, []string{string(blockID)}); err != nil {
			return err
		}
		return rejectBlockTx(ctx, tx, string(blockID))
	})
}

// trimUnreviewedRevisions deletes blockID's revisions past its last
// reviewed one — RejectBlock's core, and the order-independent half of
// RejectPage's per-block reject (see RejectPage's doc). Reports whether
// anything was actually removed: a fully reviewed block trims nothing, and
// the caller must treat that as a no-op rather than calling projectHead
// (which would needlessly touch the projection row) or retrying it.
func trimUnreviewedRevisions(ctx context.Context, tx *sql.Tx, blockID string) (bool, error) {
	var lastReviewed int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM block_revisions WHERE block_id = ? AND reviewed = 1`,
		blockID).Scan(&lastReviewed); err != nil {
		return false, translate(err)
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM block_revisions WHERE block_id = ? AND seq > ?`, blockID, lastReviewed)
	if err != nil {
		return false, translate(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, translate(err)
	}
	return n > 0, nil
}

// rejectBlockTx runs RejectBlock's full body (trim, then project only if
// the trim actually removed something) inside an already-open transaction.
// Used by RejectBlock itself. RejectPage cannot reuse it as-is: rejecting
// several blocks of one page needs the trim and project steps decoupled
// (see RejectPage's doc), so it calls trimUnreviewedRevisions and
// projectFixpoint directly instead.
func rejectBlockTx(ctx context.Context, tx *sql.Tx, blockID string) error {
	trimmed, err := trimUnreviewedRevisions(ctx, tx, blockID)
	if err != nil {
		return err
	}
	if !trimmed {
		return nil
	}
	return projectHead(ctx, tx, blockID)
}

// RejectPage implements knowledge.Repository: rejects every unreviewed
// block under topicID in one transaction — live blocks with a pending edit
// and ghosts (tombstoned blocks whose pending delete hasn't been
// reviewed).
//
// Trimming (dropping each block's own revisions past its last reviewed
// one) is independent per block and safe to run for the whole page in any
// order. Rebuilding the projection (projectHead) is not: an AI-added
// subtree needs its children's projection removed before their parent's —
// projectHead's orphan-children guard trips otherwise — while resurrecting
// an AI-deleted subtree needs the opposite order, since a child cannot
// come back to life until its parent already has (projectHead's
// restore-parent guard). A page can contain both shapes at once (one
// pending add, one pending delete, under the same topic), so no single
// global order satisfies every block; instead of computing one,
// projectFixpoint discovers a working order by retrying.
func (r *Repo) RejectPage(ctx context.Context, topicID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("reject page %s", topicID), func(tx *sql.Tx) error {
		ids, err := pageBlockIDs(ctx, tx, string(topicID))
		if err != nil {
			return err
		}
		if err := refuseSplitGroup(ctx, tx, ids); err != nil {
			return err
		}
		var toProject []string
		for _, id := range ids {
			trimmed, err := trimUnreviewedRevisions(ctx, tx, id)
			if err != nil {
				return fmt.Errorf("block %s: %w", id, translate(err))
			}
			if trimmed {
				toProject = append(toProject, id)
			}
		}
		return projectFixpoint(ctx, tx, toProject)
	})
}

// refuseSplitGroup rejects a review decision that would cover only part of an
// atomic group. ids are the blocks the decision touches; every block of such a
// group with something still pending has to be among them.
//
// Group-wide operations — MarkGroupReviewed, UndoGroup — never need the check:
// they are the whole group by construction, and they are what the frontend
// offers instead of the per-block controls (see UnreviewedBlock.Atomic). A page
// that happens to contain the entire group passes, since nothing is left split.
func refuseSplitGroup(ctx context.Context, tx *sql.Tx, ids []string) error {
	groups, err := pendingAtomicGroups(ctx, tx, ids)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	covered := make(map[string]bool, len(ids))
	for _, id := range ids {
		covered[id] = true
	}
	for _, group := range groups {
		members, err := queryIDs(ctx, tx,
			`SELECT DISTINCT block_id FROM block_revisions WHERE group_id = ? AND reviewed = 0`, group)
		if err != nil {
			return err
		}
		for _, m := range members {
			if !covered[m] {
				return fmt.Errorf("group %s also changed block %s: %w", group, m, knowledge.ErrAtomicGroup)
			}
		}
	}
	return nil
}

// pendingAtomicGroups returns the atomic groups any of ids still has an
// unreviewed revision in.
func pendingAtomicGroups(ctx context.Context, tx *sql.Tx, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return queryIDs(ctx, tx, `
SELECT DISTINCT r.group_id FROM block_revisions r
JOIN revision_groups g ON g.id = r.group_id
WHERE r.reviewed = 0 AND g.atomic = 1
  AND r.block_id IN (`+strings.Repeat(",?", len(ids))[1:]+`)`, args...)
}

// undoableBlockIDs returns the affected blocks when a group can be removed
// without crossing newer revisions.
func undoableBlockIDs(ctx context.Context, tx *sql.Tx, groupID string) ([]string, error) {
	var buried bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM block_revisions r
			WHERE r.group_id = ? AND EXISTS(
				SELECT 1 FROM block_revisions later
				WHERE later.block_id = r.block_id AND later.seq > r.seq
				  AND (later.group_id IS NULL OR later.group_id <> ?)))`,
		groupID, groupID).Scan(&buried); err != nil {
		return nil, translate(err)
	}
	if buried {
		return nil, fmt.Errorf("group %s: %w", groupID, knowledge.ErrGroupWrittenOver)
	}

	ids, err := queryIDs(ctx, tx,
		`SELECT DISTINCT block_id FROM block_revisions WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("group %s: %w", groupID, knowledge.ErrNotFound)
	}
	return ids, nil
}

// UndoGroup removes a revision group and rebuilds its block projections.
func (r *Repo) UndoGroup(ctx context.Context, groupID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("undo group %s", groupID), func(tx *sql.Tx) error {
		ids, err := undoableBlockIDs(ctx, tx, string(groupID))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM block_revisions WHERE group_id = ?`, string(groupID)); err != nil {
			return translate(err)
		}
		if err := projectFixpoint(ctx, tx, ids); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM revision_groups WHERE id = ?`, string(groupID))
		return translate(err)
	})
}

// projectFixpoint calls projectHead for every id, retrying across passes:
// a block that fails with ErrInvalidRollback (its dependency — a child or
// a parent, depending on the shape being rejected — hasn't been resolved
// yet) stays pending for the next pass. The loop stops as soon as a full
// pass makes no progress at all, and returns that pass's error, which
// rolls the whole transaction back — the same "refused rollback leaves
// history intact" guarantee DeleteRevision gives for a single block.
// Terminates: every pass that makes progress removes at least one id from
// the pending set, so there are at most len(ids) passes.
func projectFixpoint(ctx context.Context, tx *sql.Tx, ids []string) error {
	pending := ids
	for len(pending) > 0 {
		var next []string
		var lastErr error
		progressed := false
		for _, id := range pending {
			switch err := projectHead(ctx, tx, id); {
			case err == nil:
				progressed = true
			case errors.Is(err, knowledge.ErrInvalidRollback):
				next = append(next, id)
				lastErr = err
			default:
				return fmt.Errorf("block %s: %w", id, translate(err))
			}
		}
		if !progressed {
			return lastErr
		}
		pending = next
	}
	return nil
}

// MarkBlockReviewed implements knowledge.Repository. When blockID's head
// revision is a tombstone, every unreviewed-tombstone-headed descendant is
// reviewed in the same transaction: accepting only the root of a deleted
// subtree would otherwise strand each child's ghost with no reachable
// parent (the root's ghost just disappeared) — invisible to the frontend's
// buildTree (which merges ghosts in by parent) yet still counted as
// pending, and unrejectable (RejectBlock's restore-parent guard would have
// nothing live to find). A live head is unaffected: only the block's own
// revisions are marked, as before.
func (r *Repo) MarkBlockReviewed(ctx context.Context, blockID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("mark block reviewed %s", blockID), func(tx *sql.Tx) error {
		ids := []string{string(blockID)}
		head, ok, err := headRevision(ctx, tx, string(blockID))
		if err != nil {
			return translate(err)
		}
		if ok && head.Deleted {
			descendants, err := tombstoneDescendants(ctx, tx, string(blockID))
			if err != nil {
				return err
			}
			ids = append(ids, descendants...)
		}
		if err := refuseSplitGroup(ctx, tx, ids); err != nil {
			return err
		}
		return markReviewed(ctx, tx, ids)
	})
}

// markReviewed accepts every revision of each block.
func markReviewed(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE block_revisions SET reviewed = 1 WHERE block_id = ?`, id); err != nil {
			return fmt.Errorf("block %s: %w", id, translate(err))
		}
	}
	return nil
}

// tombstoneDescendants returns every unreviewed-tombstone-headed block whose
// head-revision parent_id chain reaches root — a deleted subtree's ghosts,
// found without a live blocks row to walk (their blocks rows are gone; see
// unreviewedGhostIDs).
func tombstoneDescendants(ctx context.Context, tx *sql.Tx, root string) ([]string, error) {
	ghosts, err := unreviewedGhostIDs(ctx, tx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ghosts {
		if id == root {
			continue
		}
		reaches, err := tombstoneChainReaches(ctx, tx, id, root)
		if err != nil {
			return nil, err
		}
		if reaches {
			out = append(out, id)
		}
	}
	return out, nil
}

// tombstoneChainReaches walks id's head-revision parent_id chain — a
// tombstone's ParentID is preserved from its last live snapshot (see
// applyDelete) — and reports whether it reaches root. A visited set guards
// against a cyclic chain.
func tombstoneChainReaches(ctx context.Context, tx *sql.Tx, id, root string) (bool, error) {
	current := id
	visited := make(map[string]bool)
	for current != "" {
		if visited[current] {
			return false, nil
		}
		visited[current] = true
		if current == root {
			return true, nil
		}
		head, ok, err := headRevision(ctx, tx, current)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		current = string(head.ParentID)
	}
	return false, nil
}

// MarkGroupReviewed implements knowledge.Repository: accepts every
// revision of one ApplyOps batch at once.
func (r *Repo) MarkGroupReviewed(ctx context.Context, groupID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("mark group reviewed %s", groupID), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE block_revisions SET reviewed = 1 WHERE group_id = ?`, string(groupID))
		return translate(err)
	})
}

// MarkPageReviewed implements knowledge.Repository: accepts every pending
// block under topicID — the same live-plus-ghost collection RejectPage
// uses, but review order does not matter, only membership.
func (r *Repo) MarkPageReviewed(ctx context.Context, topicID knowledge.ID) error {
	return r.withTx(ctx, fmt.Sprintf("mark page reviewed %s", topicID), func(tx *sql.Tx) error {
		ids, err := pageBlockIDs(ctx, tx, string(topicID))
		if err != nil {
			return err
		}
		if err := refuseSplitGroup(ctx, tx, ids); err != nil {
			return err
		}
		return markReviewed(ctx, tx, ids)
	})
}

// ListRevisions implements knowledge.Repository: full history, newest
// first. A block with no revisions at all is ErrNotFound — a live block
// always has at least its creation revision, so an empty result means the
// id never existed (or was fully rejected/rolled back away).
func (r *Repo) ListRevisions(ctx context.Context, blockID knowledge.ID) ([]knowledge.Revision, error) {
	rows, err := r.db.QueryContext(ctx,
		revisionSelect+` WHERE r.block_id = ? ORDER BY r.seq DESC`, string(blockID))
	if err != nil {
		return nil, fmt.Errorf("list revisions of %s: %w", blockID, translate(err))
	}
	defer rows.Close()

	var out []knowledge.Revision
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("list revisions of %s: %w", blockID, translate(err))
		}
		out = append(out, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list revisions of %s: %w", blockID, translate(err))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("list revisions of %s: %w", blockID, knowledge.ErrNotFound)
	}
	return out, nil
}

// ListUnreviewed implements knowledge.Repository: everything awaiting
// review, for the Changes screen. Blocks are ordered by topic then block id
// for a stable screen across polls.
func (r *Repo) ListUnreviewed(ctx context.Context) (knowledge.UnreviewedChanges, error) {
	blockIDs, err := unreviewedBlockIDs(ctx, r.db)
	if err != nil {
		return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: %w", translate(err))
	}

	out := make([]knowledge.UnreviewedBlock, 0, len(blockIDs))
	for _, id := range blockIDs {
		head, ok, err := headRevision(ctx, r.db, id)
		if err != nil {
			return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: head of %s: %w", id, translate(err))
		}
		if !ok {
			continue
		}
		topic, err := topicOf(ctx, r.db, id)
		if err != nil {
			return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: topic of %s: %w", id, translate(err))
		}
		// GroupID and GroupTitle both come from the same row — the block's
		// newest unreviewed revision — so a batch's title never labels a
		// different group's id (see UnreviewedBlock's doc: that row is not
		// always head, e.g. a reviewed owner edit sitting on top of a still-
		// unreviewed AI edit).
		var groupID, title string
		var atomic bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(rv.group_id, ''), COALESCE(g.title, ''), COALESCE(g.atomic, 0) FROM block_revisions rv
			LEFT JOIN revision_groups g ON g.id = rv.group_id
			WHERE rv.block_id = ? AND rv.reviewed = 0
			ORDER BY rv.seq DESC LIMIT 1`, id).Scan(&groupID, &title, &atomic); err != nil {
			return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: group of %s: %w", id, translate(err))
		}
		out = append(out, knowledge.UnreviewedBlock{
			Head: head, TopicID: topic, GroupID: knowledge.ID(groupID), GroupTitle: title, Atomic: atomic,
		})
	}
	slices.SortStableFunc(out, func(a, b knowledge.UnreviewedBlock) int {
		if c := cmp.Compare(a.TopicID, b.TopicID); c != 0 {
			return c
		}
		return cmp.Compare(a.Head.BlockID, b.Head.BlockID)
	})

	return knowledge.UnreviewedChanges{Blocks: out}, nil
}

// unreviewedBlockIDs returns the distinct ids of every block that has at
// least one unreviewed revision (not necessarily the head — see
// ListUnreviewed's doc).
func unreviewedBlockIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	return queryIDs(ctx, db, `SELECT DISTINCT block_id FROM block_revisions WHERE reviewed = 0`)
}

// topicOf walks blockID's parent chain up to the topic root: preferring the
// live blocks row's parent_id, falling back to the head revision's for a
// tombstoned ancestor (whose blocks row is gone). A visited set guards
// against a cyclic chain.
func topicOf(ctx context.Context, q rowQueryer, blockID string) (knowledge.ID, error) {
	current := blockID
	visited := make(map[string]bool)
	for {
		if visited[current] {
			return knowledge.ID(current), nil
		}
		visited[current] = true

		var parent sql.NullString
		err := q.QueryRowContext(ctx, `SELECT parent_id FROM blocks WHERE id = ?`, current).Scan(&parent)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			head, ok, herr := headRevision(ctx, q, current)
			if herr != nil {
				return "", herr
			}
			if !ok || head.ParentID == "" {
				return knowledge.ID(current), nil
			}
			current = string(head.ParentID)
		case err != nil:
			return "", err
		case !parent.Valid || parent.String == "":
			return knowledge.ID(current), nil
		default:
			current = parent.String
		}
	}
}

// pageBlockIDs collects every block of the page rooted at topicID: the live
// subtree (recursive CTE over blocks) plus ghosts — tombstoned blocks whose
// unreviewed head revision resolves (via topicOf) to this topic. The result
// is unordered: RejectPage cannot rely on a single computed order (see its
// doc) and discovers a working one by retrying instead; MarkPageReviewed's
// per-block UPDATE is order-independent regardless.
func pageBlockIDs(ctx context.Context, tx *sql.Tx, topicID string) ([]string, error) {
	ids, err := liveSubtreeIDs(ctx, tx, topicID)
	if err != nil {
		return nil, fmt.Errorf("page blocks of %s: %w", topicID, err)
	}
	ghosts, err := unreviewedGhostIDs(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("page blocks of %s: %w", topicID, err)
	}
	for _, id := range ghosts {
		topic, err := topicOf(ctx, tx, id)
		if err != nil {
			return nil, fmt.Errorf("page blocks of %s: topic of %s: %w", topicID, id, err)
		}
		if string(topic) == topicID {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// liveSubtreeIDs returns the ids of topicID and every live descendant.
func liveSubtreeIDs(ctx context.Context, tx *sql.Tx, root string) ([]string, error) {
	return queryIDs(ctx, tx, subtreeCTE+`
SELECT id FROM subtree`, root)
}

// unreviewedGhostIDs returns every block whose current head revision is an
// unreviewed tombstone: a pending delete, absent from blocks, that
// ListUnreviewed and page-scoped review ops must still surface.
func unreviewedGhostIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	return queryIDs(ctx, tx, `
SELECT block_id FROM block_revisions r1
WHERE r1.deleted = 1 AND r1.reviewed = 0
  AND r1.seq = (SELECT MAX(seq) FROM block_revisions r2 WHERE r2.block_id = r1.block_id)`)
}
