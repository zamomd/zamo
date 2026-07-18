package sqlite

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// GetEdges implements knowledge.Repository: every edge incident to the
// block, in either direction, ordered by relation and then endpoints so the
// result is deterministic. Soft-deleted edges (RemoveEdge) and edges with a
// dead endpoint (the neighbour tombstoned by an unreviewed AI delete) are
// hidden — restoring the endpoint restores the edge without storage having
// to remember it separately. No links — or an unknown id — yields an empty
// slice, not an error.
//
// ponytail: the to_id leg is a table scan (the PK only covers the from_id
// prefix). Fine for a personal base; add an index on to_id if it ever shows.
func (r *Repo) GetEdges(ctx context.Context, id knowledge.ID) ([]knowledge.Edge, error) {
	const q = `
SELECT from_id, to_id, relation, reviewed FROM edges
WHERE (from_id = ? OR to_id = ?) AND deleted = 0
  AND EXISTS(SELECT 1 FROM blocks WHERE id = from_id)
  AND EXISTS(SELECT 1 FROM blocks WHERE id = to_id)
ORDER BY relation, from_id, to_id`

	rows, err := r.db.QueryContext(ctx, q, string(id), string(id))
	if err != nil {
		return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
	}
	defer rows.Close()

	var out []knowledge.Edge
	for rows.Next() {
		var e knowledge.Edge
		if err := rows.Scan(&e.From, &e.To, &e.Relation, &e.Reviewed); err != nil {
			return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
	}
	return out, nil
}

// AddEdge implements knowledge.Repository: records one edge directly, the
// same upsert applyLink uses for a link op — an owner write (meta.Author ==
// AuthorOwner) is born reviewed, an AI write awaits review; either way the
// upsert resurrects a soft-deleted edge (deleted=0). Either endpoint gone
// maps to ErrNotFound.
func (r *Repo) AddEdge(
	ctx context.Context, from, to knowledge.ID, rel knowledge.Relation, meta knowledge.WriteMeta,
) error {
	// _txlock=immediate (DSN) makes this a BEGIN IMMEDIATE, so the existence
	// checks and the insert cannot deadlock on a lock upgrade under WAL.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add edge %s->%s: %w", from, to, translate(err))
	}
	defer tx.Rollback()

	if err := requireBlock(ctx, tx, knowledge.OpLink, string(from)); err != nil {
		return err
	}
	if err := requireBlock(ctx, tx, knowledge.OpLink, string(to)); err != nil {
		return err
	}
	reviewed := meta.Author == knowledge.AuthorOwner
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO edges (from_id, to_id, relation, author, reviewed, deleted, group_id, created_at)
		VALUES (?, ?, ?, ?, ?, 0, NULL, ?)
		ON CONFLICT (from_id, to_id, relation)
		DO UPDATE SET deleted = 0, reviewed = excluded.reviewed, group_id = excluded.group_id`,
		string(from), string(to), string(rel), string(meta.Author), reviewed, formatTime(r.now())); err != nil {
		return fmt.Errorf("add edge %s->%s: %w", from, to, translate(err))
	}
	return tx.Commit()
}

// RemoveEdge implements knowledge.Repository: soft-deletes one edge by its
// full identity, so a later re-AddEdge or applyLink resurrects the same row
// instead of racing a fresh insert against the primary key. Removing an
// absent edge is a no-op.
func (r *Repo) RemoveEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE edges SET deleted = 1 WHERE from_id = ? AND to_id = ? AND relation = ?`,
		string(from), string(to), string(rel)); err != nil {
		return fmt.Errorf("remove edge %s->%s: %w", from, to, translate(err))
	}
	return nil
}

// MarkEdgeReviewed implements knowledge.Repository. No matching row —
// absent, already deleted, or already reviewed makes no difference to the
// caller — is ErrNotFound.
func (r *Repo) MarkEdgeReviewed(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE edges SET reviewed = 1 WHERE from_id = ? AND to_id = ? AND relation = ?`,
		string(from), string(to), string(rel))
	if err != nil {
		return fmt.Errorf("mark edge reviewed %s->%s: %w", from, to, translate(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark edge reviewed %s->%s: %w", from, to, translate(err))
	}
	if n == 0 {
		return fmt.Errorf("mark edge reviewed %s->%s: %w", from, to, knowledge.ErrNotFound)
	}
	return nil
}

// RejectEdge implements knowledge.Repository: hard-deletes an unreviewed
// edge — rejecting a pending link has nothing to roll back to, unlike a
// block revision. A reviewed edge (or an absent one) is refused with
// ErrNotFound: only a pending edge is deletable this way.
func (r *Repo) RejectEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM edges WHERE from_id = ? AND to_id = ? AND relation = ? AND reviewed = 0`,
		string(from), string(to), string(rel))
	if err != nil {
		return fmt.Errorf("reject edge %s->%s: %w", from, to, translate(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reject edge %s->%s: %w", from, to, translate(err))
	}
	if n == 0 {
		return fmt.Errorf("reject edge %s->%s: %w", from, to, knowledge.ErrNotFound)
	}
	return nil
}
