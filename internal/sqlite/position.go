package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// positionStride spaces sibling positions so a block can be inserted between
// two neighbours by taking the midpoint, without touching either of them.
// That matters because position is part of every revision snapshot and
// projectHead restores it verbatim (see revision.go): a neighbour shifted in
// the blocks table but not re-revisioned would be restored to a position it
// no longer holds. Ordering only ever compares positions (see read.go's
// preorder), so gaps are free.
const positionStride = 1 << 16

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
// keeping their order, so an exhausted gap gets room again. It writes
// positions without writing revisions: every sibling of parent then holds a
// position its own head revision does not remember, so rolling back any one
// of them — including a block the user never touched — lands it one slot off.
// Accepted deliberately. It fires on the 17th consecutive insert anchored
// into the same gap; because each Enter anchors on the block the caret just
// landed in, that gap halves with every insert, so "17 consecutive inserts
// into one gap" is just typing seventeen lines between two existing
// blocks — an ordinary gesture, not a pathological one — and the result is a
// reordering, not a loss.
func renumber(ctx context.Context, tx *sql.Tx, parent any) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM blocks WHERE parent_id IS ? ORDER BY position`, parent)
	if err != nil {
		return fmt.Errorf("renumber children: %w", translate(err))
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("renumber children: %w", translate(err))
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("renumber children: %w", translate(err))
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE blocks SET position = ? WHERE id = ?`, i*positionStride, id); err != nil {
			return fmt.Errorf("renumber %s: %w", id, translate(err))
		}
	}
	return nil
}
