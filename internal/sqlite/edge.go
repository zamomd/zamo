package sqlite

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// GetEdges implements knowledge.Repository: every edge incident to the block,
// in either direction — a links panel must show links pointing at the block,
// not only away from it. Order is deterministic (relation, then endpoints).
// A block with no links, or an unknown id, yields an empty slice, not an
// error: "nothing to show" is a normal state.
//
// ponytail: the to_id leg is a table scan (the PK only covers the from_id
// prefix). Fine for a personal base; add an index on to_id if it ever shows.
func (r *Repo) GetEdges(ctx context.Context, id knowledge.ID) ([]knowledge.Edge, error) {
	const q = `
SELECT from_id, to_id, relation FROM edges
WHERE from_id = ? OR to_id = ?
ORDER BY relation, from_id, to_id`

	rows, err := r.db.QueryContext(ctx, q, string(id), string(id))
	if err != nil {
		return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
	}
	defer rows.Close()

	var out []knowledge.Edge
	for rows.Next() {
		var e knowledge.Edge
		if err := rows.Scan(&e.From, &e.To, &e.Relation); err != nil {
			return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get edges of %s: %w", id, translate(err))
	}
	return out, nil
}

// AddEdge implements knowledge.Repository: records one edge directly — the
// owner's write path, outside the review queue. Re-adding an existing edge is
// a no-op (the goal state already holds). Either endpoint gone maps to
// ErrStale, the same "base moved" signal applyLink gives; the relation CHECK
// stays a hard error because the domain validates relations before calling.
func (r *Repo) AddEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO edges (from_id, to_id, relation, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (from_id, to_id, relation) DO NOTHING`,
		string(from), string(to), string(rel), formatTime(r.now())); err != nil {
		return fmt.Errorf("add edge %s->%s: %w", from, to, translate(err))
	}
	return tx.Commit()
}

// RemoveEdge implements knowledge.Repository: deletes one edge by its full
// identity. Removing an absent edge is success — the caller's goal state,
// "no such edge", already holds.
func (r *Repo) RemoveEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM edges WHERE from_id = ? AND to_id = ? AND relation = ?`,
		string(from), string(to), string(rel)); err != nil {
		return fmt.Errorf("remove edge %s->%s: %w", from, to, translate(err))
	}
	return nil
}
