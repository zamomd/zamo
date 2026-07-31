package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// positionStride spaces sibling positions so a block can be inserted between
// two neighbours by taking the midpoint, without touching either of them.
// Position is part of every revision snapshot and projectHead restores it
// verbatim (see revision.go), so renumber keeps the projection and history in
// the same coordinate system. Ordering only ever compares positions (see
// read.go's preorder), so gaps are free.
//
// The size buys headroom against renumber, which is the one case that does
// rewrite existing revision positions (see there): a gap survives as many
// midpoint inserts as the stride has halvings, so 1<<30 tolerates a run of 30
// lines typed into one gap where 1<<16 tolerated 16. Nothing counts positions,
// so the only ceiling is int64: even a million siblings appended at this
// stride stays four orders of magnitude short of overflow.
const positionStride = 1 << 30

// nextPosition returns the position for a block appended under parent.
// parent is nil for a topic root or a block id; `IS ?` matches both.
func nextPosition(ctx context.Context, tx *sql.Tx, parent any) (int, error) {
	var pos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position) + ?, 0) FROM blocks WHERE parent_id IS ?`,
		positionStride, parent).Scan(&pos); err != nil {
		return 0, fmt.Errorf("next position: %w", translate(err))
	}
	return pos, nil
}

// resolvePosition picks the position for an add or a move from the op's
// anchor: after a sibling, before a sibling, or appended when neither is set.
func resolvePosition(ctx context.Context, tx *sql.Tx, parent any, op knowledge.Op) (int, error) {
	switch {
	case op.After != "":
		return positionAfter(ctx, tx, parent, op.After)
	case op.Before != "":
		return positionBefore(ctx, tx, parent, op.Before)
	default:
		return nextPosition(ctx, tx, parent)
	}
}

// positionAfter returns the position for a block inserted directly after the
// sibling anchor: the midpoint between the anchor and the next sibling, or a
// stride past the anchor when it is last. When the midpoint does not exist
// (the gap is smaller than 2) the parent's children are respaced first — see
// renumber.
func positionAfter(ctx context.Context, tx *sql.Tx, parent any, after string) (int, error) {
	anchor, next, err := neighbourhood(ctx, tx, parent, after)
	if err != nil {
		return 0, err
	}
	if next == nil {
		return anchor + positionStride, nil
	}
	if *next-anchor < 2 {
		if err := renumber(ctx, tx, parent); err != nil {
			return 0, err
		}
		if anchor, next, err = neighbourhood(ctx, tx, parent, after); err != nil {
			return 0, err
		}
		if next == nil {
			return anchor + positionStride, nil
		}
	}
	return anchor + (*next-anchor)/2, nil
}

// neighbourhood returns the anchor's position and the position of the sibling
// right after it, nil when the anchor is last.
func neighbourhood(ctx context.Context, tx *sql.Tx, parent any, anchorID string) (int, *int, error) {
	var anchor int
	if err := tx.QueryRowContext(ctx,
		`SELECT position FROM blocks WHERE id = ?`, anchorID).Scan(&anchor); err != nil {
		return 0, nil, fmt.Errorf("anchor %s position: %w", anchorID, translate(err))
	}
	var next sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MIN(position) FROM blocks WHERE parent_id IS ? AND position > ?`,
		parent, anchor).Scan(&next); err != nil {
		return 0, nil, fmt.Errorf("sibling after %s: %w", anchorID, translate(err))
	}
	if !next.Valid {
		return anchor, nil, nil
	}
	return anchor, new(int(next.Int64)), nil
}

// positionBefore returns the position for a block inserted directly before the
// sibling anchor: the midpoint between the previous sibling and the anchor, or
// a stride ahead of the anchor when it is first. When the midpoint does not
// exist (the gap is smaller than 2) the parent's children are respaced first
// — see renumber. Positions may go negative — they are compared, never
// counted (see read.go's preorder).
func positionBefore(ctx context.Context, tx *sql.Tx, parent any, before string) (int, error) {
	anchor, prev, err := precedingNeighbourhood(ctx, tx, parent, before)
	if err != nil {
		return 0, err
	}
	if prev == nil {
		return anchor - positionStride, nil
	}
	if anchor-*prev < 2 {
		if err := renumber(ctx, tx, parent); err != nil {
			return 0, err
		}
		if anchor, prev, err = precedingNeighbourhood(ctx, tx, parent, before); err != nil {
			return 0, err
		}
		if prev == nil {
			return anchor - positionStride, nil
		}
	}
	return *prev + (anchor-*prev)/2, nil
}

// precedingNeighbourhood returns the anchor's position and the position of the
// sibling right before it, nil when the anchor is first.
func precedingNeighbourhood(ctx context.Context, tx *sql.Tx, parent any, anchorID string) (int, *int, error) {
	var anchor int
	if err := tx.QueryRowContext(ctx,
		`SELECT position FROM blocks WHERE id = ?`, anchorID).Scan(&anchor); err != nil {
		return 0, nil, fmt.Errorf("anchor %s position: %w", anchorID, translate(err))
	}
	var prev sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM blocks WHERE parent_id IS ? AND position < ?`,
		parent, anchor).Scan(&prev); err != nil {
		return 0, nil, fmt.Errorf("sibling before %s: %w", anchorID, translate(err))
	}
	if !prev.Valid {
		return anchor, nil, nil
	}
	return anchor, new(int(prev.Int64)), nil
}

// renumber respaces every child of parent at positionStride intervals,
// keeping their order, so an exhausted gap gets room again. It also rewrites
// every revision position for each child: respacing changes the coordinate
// system rather than the block's historical placement, and projectHead may
// restore any surviving revision after a rejection or deletion. This is the
// deliberate exception to block_revisions being append-only; adding a revision
// per bystander would expose storage maintenance as user-visible history.
func renumber(ctx context.Context, tx *sql.Tx, parent any) error {
	ids, err := queryIDs(ctx, tx,
		`SELECT id FROM blocks WHERE parent_id IS ? ORDER BY position`, parent)
	if err != nil {
		return fmt.Errorf("renumber children: %w", err)
	}
	for i, id := range ids {
		position := i * positionStride
		if _, err := tx.ExecContext(ctx,
			`UPDATE blocks SET position = ? WHERE id = ?`, position, id); err != nil {
			return fmt.Errorf("renumber %s: %w", id, translate(err))
		}
		result, err := tx.ExecContext(ctx,
			`UPDATE block_revisions SET position = ? WHERE block_id = ?`, position, id)
		if err != nil {
			return fmt.Errorf("renumber revisions of %s: %w", id, translate(err))
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("renumber revisions of %s: %w", id, translate(err))
		}
		if updated == 0 {
			return fmt.Errorf("renumber revisions of %s: %w", id, knowledge.ErrNotFound)
		}
	}
	return nil
}
