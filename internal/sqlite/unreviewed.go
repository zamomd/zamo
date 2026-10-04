package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// CountUnreviewedUnder implements knowledge.Repository: the number of blocks
// at or under root that await review, pending deletions included.
func (r *Repo) CountUnreviewedUnder(ctx context.Context, root knowledge.ID) (int, error) {
	var n int
	err := r.withTx(ctx, fmt.Sprintf("count unreviewed under %s", root), func(tx *sql.Tx) error {
		var err error
		n, err = countUnreviewedUnder(ctx, tx, root)
		return err
	})
	return n, err
}

// countUnreviewedUnder counts live blocks of root's subtree that carry an
// unreviewed revision, plus the ghosts of pending deletions under it, whose
// blocks rows are gone.
func countUnreviewedUnder(ctx context.Context, tx *sql.Tx, root knowledge.ID) (int, error) {
	subtree, err := subtreePreorder(ctx, tx, root)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range subtree {
		if b.Unreviewed {
			n++
		}
	}
	ghosts, err := tombstoneDescendants(ctx, tx, string(root))
	if err != nil {
		return 0, err
	}
	return n + len(ghosts), nil
}

// refuseOverUnreviewed enforces WriteMeta.RefuseOverUnreviewedIn inside the
// apply transaction, which already holds the write lock: no review decision
// and no other write can land between this check and the batch.
func refuseOverUnreviewed(ctx context.Context, tx *sql.Tx, scope knowledge.ID) error {
	if scope == "" {
		return nil
	}
	n, err := countUnreviewedUnder(ctx, tx, scope)
	if err != nil {
		return err
	}
	if n > 0 {
		return &knowledge.UnreviewedInScopeError{Scope: scope, Count: n}
	}
	return nil
}
