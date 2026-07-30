package sqlite

import (
	"context"
	"database/sql"
	"fmt"
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
