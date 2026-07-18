package sqlite

// revision row helpers shared by the write path (sqlite.go) and the review
// operations (Task 4: reject, mark-reviewed, list-unreviewed).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/cominddev/comind/internal/knowledge"
)

// revisionColumns is the block_revisions projection every read shares, in
// the order scanRevision expects.
const revisionColumns = `id, block_id, seq, content, parent_id, position,
	deleted, author, source, origin, rationale, reviewed, group_id, created_at`

// rowQueryer is the read surface shared by *sql.DB and *sql.Tx, so
// headRevision works standalone (a plain read) or inside a write
// transaction (rebuilding the projection after an insert).
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is satisfied by *sql.Row and *sql.Rows, so scanRevision maps
// either a single lookup (headRevision) or one row of a multi-row listing
// (Task 4's ListRevisions and friends).
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
		(id, block_id, seq, content, parent_id, position, deleted, author,
		 source, origin, rationale, reviewed, group_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(rev.ID), string(rev.BlockID), rev.Seq, rev.Content, parent,
		rev.Position, rev.Deleted, string(rev.Author), string(rev.Source),
		rev.Origin, rev.Rationale, rev.Reviewed, string(rev.GroupID), now)
	return err
}

// headRevision returns the block's highest-seq revision, or ok=false when
// the block has none.
func headRevision(ctx context.Context, q rowQueryer, blockID string) (knowledge.Revision, bool, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+revisionColumns+` FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`,
		blockID)
	rev, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return knowledge.Revision{}, false, nil
	}
	if err != nil {
		return knowledge.Revision{}, false, err
	}
	return rev, true, nil
}

// scanRevision maps one revisionColumns row into a domain Revision: NULL
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
		&rev.Deleted, &author, &source, &origin, &rationale, &rev.Reviewed, &group, &created); err != nil {
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

// projectHead rebuilds the blocks row from the block's current head
// revision, enforcing the rollback invariants: a live head needs a live
// parent (or root), and removing a block from the projection (no head, or a
// tombstone head) is refused while live children exist. created_at is the
// first revision's timestamp, updated_at the head's — not "now": rebuilding
// the projection from history must reproduce the state as of that revision,
// not stamp the moment of the rollback.
//
// The upsert's content change fires the FTS update trigger; the resurrect
// insert fires the insert trigger; the delete fires the delete trigger — FTS
// stays consistent for free.
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			parent_id = excluded.parent_id, position = excluded.position,
			content = excluded.content, updated_at = excluded.updated_at`,
		blockID, parent, head.Position, head.Content, first, formatTime(head.CreatedAt)); err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	return nil
}

// DeleteRevision implements knowledge.Repository: the rollback primitive.
// Deleting a non-head revision only prunes history — the projection is
// already built from a later revision. Deleting the head rebuilds the
// projection from whatever revision is now the head (projectHead), which
// refuses (ErrInvalidRollback) and rolls the whole transaction back when
// that would orphan live children or leave the block pointing at a missing
// parent — a refused rollback leaves history untouched.
func (r *Repo) DeleteRevision(ctx context.Context, blockID, revID knowledge.ID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
	}
	defer tx.Rollback()

	var seq int
	if err := tx.QueryRowContext(ctx,
		`SELECT seq FROM block_revisions WHERE id = ? AND block_id = ?`,
		string(revID), string(blockID)).Scan(&seq); err != nil {
		return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
	}
	var maxSeq int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM block_revisions WHERE block_id = ?`,
		string(blockID)).Scan(&maxSeq); err != nil {
		return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM block_revisions WHERE id = ?`, string(revID)); err != nil {
		return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
	}
	if seq == maxSeq {
		if err := projectHead(ctx, tx, string(blockID)); err != nil {
			return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete revision %s of %s: %w", revID, blockID, translate(err))
	}
	return nil
}

// RejectBlock implements knowledge.Repository: drops the block's unreviewed
// head revisions (everything after the last reviewed one) and rebuilds the
// projection. A fully reviewed block — or an unknown one — has nothing past
// its last reviewed revision to drop, so it is a no-op.
func (r *Repo) RejectBlock(ctx context.Context, blockID knowledge.ID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reject block %s: %w", blockID, translate(err))
	}
	defer tx.Rollback()

	if err := rejectBlockTx(ctx, tx, string(blockID)); err != nil {
		return fmt.Errorf("reject block %s: %w", blockID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reject block %s: %w", blockID, translate(err))
	}
	return nil
}

// rejectBlockTx runs RejectBlock's body inside an already-open transaction:
// shared by RejectBlock itself and RejectPage, which rejects every page
// block in one transaction.
func rejectBlockTx(ctx context.Context, tx *sql.Tx, blockID string) error {
	var lastReviewed int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM block_revisions WHERE block_id = ? AND reviewed = 1`,
		blockID).Scan(&lastReviewed); err != nil {
		return translate(err)
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM block_revisions WHERE block_id = ? AND seq > ?`, blockID, lastReviewed)
	if err != nil {
		return translate(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return translate(err)
	}
	if n == 0 {
		return nil
	}
	return projectHead(ctx, tx, blockID)
}

// RejectPage implements knowledge.Repository: rejects every unreviewed block
// under topicID in one transaction — live blocks with a pending edit and
// ghosts (tombstoned blocks whose pending delete hasn't been reviewed).
// Blocks are processed deepest-first so rejecting an AI-added subtree (whose
// leaves would otherwise be orphaned by rejecting their parent first) never
// trips projectHead's orphan guard.
func (r *Repo) RejectPage(ctx context.Context, topicID knowledge.ID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reject page %s: %w", topicID, translate(err))
	}
	defer tx.Rollback()

	ids, err := pageBlockIDs(ctx, tx, string(topicID))
	if err != nil {
		return fmt.Errorf("reject page %s: %w", topicID, err)
	}
	for _, id := range ids {
		if err := rejectBlockTx(ctx, tx, id); err != nil {
			return fmt.Errorf("reject page %s: block %s: %w", topicID, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reject page %s: %w", topicID, translate(err))
	}
	return nil
}

// MarkBlockReviewed implements knowledge.Repository.
func (r *Repo) MarkBlockReviewed(ctx context.Context, blockID knowledge.ID) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE block_revisions SET reviewed = 1 WHERE block_id = ?`, string(blockID)); err != nil {
		return fmt.Errorf("mark block reviewed %s: %w", blockID, translate(err))
	}
	return nil
}

// MarkGroupReviewed implements knowledge.Repository: accepts every revision
// of one ApplyOps batch at once.
func (r *Repo) MarkGroupReviewed(ctx context.Context, groupID knowledge.ID) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE block_revisions SET reviewed = 1 WHERE group_id = ?`, string(groupID)); err != nil {
		return fmt.Errorf("mark group reviewed %s: %w", groupID, translate(err))
	}
	return nil
}

// MarkPageReviewed implements knowledge.Repository: accepts every pending
// block under topicID — the same live-plus-ghost collection RejectPage
// uses, but review order does not matter, only membership.
func (r *Repo) MarkPageReviewed(ctx context.Context, topicID knowledge.ID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark page reviewed %s: %w", topicID, translate(err))
	}
	defer tx.Rollback()

	ids, err := pageBlockIDs(ctx, tx, string(topicID))
	if err != nil {
		return fmt.Errorf("mark page reviewed %s: %w", topicID, err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE block_revisions SET reviewed = 1 WHERE block_id = ?`, id); err != nil {
			return fmt.Errorf("mark page reviewed %s: block %s: %w", topicID, id, translate(err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark page reviewed %s: %w", topicID, translate(err))
	}
	return nil
}

// ListRevisions implements knowledge.Repository: full history, newest
// first. A block with no revisions at all is ErrNotFound — a live block
// always has at least its creation revision, so an empty result means the
// id never existed (or was fully rejected/rolled back away).
func (r *Repo) ListRevisions(ctx context.Context, blockID knowledge.ID) ([]knowledge.Revision, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+revisionColumns+` FROM block_revisions WHERE block_id = ? ORDER BY seq DESC`,
		string(blockID))
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
		var title string
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(g.title, '') FROM block_revisions rv
			LEFT JOIN revision_groups g ON g.id = rv.group_id
			WHERE rv.block_id = ? AND rv.reviewed = 0
			ORDER BY rv.seq DESC LIMIT 1`, id).Scan(&title); err != nil {
			return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: group title of %s: %w", id, translate(err))
		}
		out = append(out, knowledge.UnreviewedBlock{Head: head, TopicID: topic, GroupTitle: title})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TopicID != out[j].TopicID {
			return out[i].TopicID < out[j].TopicID
		}
		return out[i].Head.BlockID < out[j].Head.BlockID
	})

	const edgeQ = `
SELECT from_id, to_id, relation, reviewed FROM edges
WHERE reviewed = 0 AND deleted = 0
  AND EXISTS(SELECT 1 FROM blocks WHERE id = from_id)
  AND EXISTS(SELECT 1 FROM blocks WHERE id = to_id)
ORDER BY relation, from_id, to_id`
	edgeRows, err := r.db.QueryContext(ctx, edgeQ)
	if err != nil {
		return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: edges: %w", translate(err))
	}
	defer edgeRows.Close()
	var edges []knowledge.Edge
	for edgeRows.Next() {
		var e knowledge.Edge
		if err := edgeRows.Scan(&e.From, &e.To, &e.Relation, &e.Reviewed); err != nil {
			return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: edges: %w", translate(err))
		}
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return knowledge.UnreviewedChanges{}, fmt.Errorf("list unreviewed: edges: %w", translate(err))
	}

	return knowledge.UnreviewedChanges{Blocks: out, Edges: edges}, nil
}

// unreviewedBlockIDs returns the distinct ids of every block that has at
// least one unreviewed revision (not necessarily the head — see
// ListUnreviewed's doc).
func unreviewedBlockIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT block_id FROM block_revisions WHERE reviewed = 0`)
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

// topicOf walks blockID's parent chain up to the topic root: preferring the
// live blocks row's parent_id, falling back to the head revision's for a
// tombstoned ancestor (whose blocks row is gone). A visited set guards
// against a cyclic chain.
func topicOf(ctx context.Context, q rowQueryer, blockID string) (knowledge.ID, error) {
	root, _, err := walkAncestors(ctx, q, blockID)
	return root, err
}

// walkAncestors is topicOf's walk, also reporting the number of hops taken
// to reach the root — the depth pageBlockIDs sorts by so RejectPage
// processes children before their parents.
func walkAncestors(ctx context.Context, q rowQueryer, blockID string) (root knowledge.ID, depth int, err error) {
	current := blockID
	visited := make(map[string]bool)
	for {
		if visited[current] {
			return knowledge.ID(current), depth, nil
		}
		visited[current] = true

		var parent sql.NullString
		err := q.QueryRowContext(ctx, `SELECT parent_id FROM blocks WHERE id = ?`, current).Scan(&parent)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			head, ok, herr := headRevision(ctx, q, current)
			if herr != nil {
				return "", 0, herr
			}
			if !ok || head.ParentID == "" {
				return knowledge.ID(current), depth, nil
			}
			current = string(head.ParentID)
			depth++
		case err != nil:
			return "", 0, err
		case !parent.Valid || parent.String == "":
			return knowledge.ID(current), depth, nil
		default:
			current = parent.String
			depth++
		}
	}
}

// pageBlockIDs collects every block of the page rooted at topicID: the live
// subtree (recursive CTE over blocks) plus ghosts — tombstoned blocks whose
// unreviewed head revision resolves (via topicOf) to this topic. The result
// is ordered deepest-first so RejectPage can reject a whole AI-added subtree
// in one pass without projectHead's orphan guard tripping on a child that
// hasn't been rejected yet.
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

	depths := make(map[string]int, len(ids))
	for _, id := range ids {
		_, d, err := walkAncestors(ctx, tx, id)
		if err != nil {
			return nil, fmt.Errorf("page blocks of %s: depth of %s: %w", topicID, id, err)
		}
		depths[id] = d
	}
	sort.SliceStable(ids, func(i, j int) bool { return depths[ids[i]] > depths[ids[j]] })
	return ids, nil
}

// liveSubtreeIDs returns the ids of topicID and every live descendant.
func liveSubtreeIDs(ctx context.Context, tx *sql.Tx, root string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
WITH RECURSIVE subtree(id) AS (
    SELECT id FROM blocks WHERE id = ?
    UNION
    SELECT b.id FROM blocks b JOIN subtree s ON b.parent_id = s.id
)
SELECT id FROM subtree`, root)
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

// unreviewedGhostIDs returns every block whose current head revision is an
// unreviewed tombstone: a pending delete, absent from blocks, that
// ListUnreviewed and page-scoped review ops must still surface.
func unreviewedGhostIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT block_id FROM block_revisions r1
WHERE r1.deleted = 1 AND r1.reviewed = 0
  AND r1.seq = (SELECT MAX(seq) FROM block_revisions r2 WHERE r2.block_id = r1.block_id)`)
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
