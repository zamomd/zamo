package sqlite

import (
	"context"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// BlocksReferencingTopic implements knowledge.Repository. It reads the
// reference index rather than scanning content, so the cost is one index
// lookup regardless of base size. The index holds one row per source and
// target, so a block referencing the topic several times appears once.
func (r *Repo) BlocksReferencingTopic(ctx context.Context, titleKey string) ([]knowledge.Block, error) {
	const q = `
SELECT ` + blockColumns + ` FROM blocks
JOIN refs ON refs.source_id = blocks.id
WHERE refs.kind = 'topic' AND refs.target_key = ?
ORDER BY blocks.id`

	rows, err := r.db.QueryContext(ctx, q, titleKey)
	if err != nil {
		return nil, fmt.Errorf("read blocks referencing %q: %w", titleKey, translate(err))
	}
	defer rows.Close()

	blocks, err := scanBlocks(rows)
	if err != nil {
		return nil, fmt.Errorf("read blocks referencing %q: %w", titleKey, translate(err))
	}
	return blocks, nil
}
