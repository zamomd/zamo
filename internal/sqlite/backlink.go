package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// GetBacklinks implements knowledge.Repository. Both reference kinds resolve
// in one query: a block reference matches the target id directly, a topic
// reference matches the target's title_key, which is non-NULL only for roots.
// A reference naming a topic that does not exist matches nothing, which is
// how an unresolved [[Topic]] stays out of the result without a special case.
// Ordered by source id — ULID order, so creation order.
func (r *Repo) GetBacklinks(ctx context.Context, target knowledge.ID) ([]knowledge.Backlink, error) {
	const q = topicRoots + `
SELECT s.id, s.parent_id, s.position, s.content, r.root, t.content
FROM refs f
JOIN blocks s ON s.id = f.source_id
JOIN roots r ON r.id = s.id
JOIN blocks t ON t.id = r.root
JOIN blocks target ON target.id = ?
WHERE (f.kind = 'block' AND f.target_key = target.id)
   OR (f.kind = 'topic' AND target.title_key IS NOT NULL AND f.target_key = target.title_key)
ORDER BY s.id`

	rows, err := r.db.QueryContext(ctx, q, string(target))
	if err != nil {
		return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
	}
	defer rows.Close()

	var out []knowledge.Backlink
	for rows.Next() {
		var b knowledge.Backlink
		var parent sql.NullString
		var topicContent string
		if err := rows.Scan(&b.Block.ID, &parent, &b.Block.Position, &b.Block.Content,
			&b.TopicID, &topicContent); err != nil {
			return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
		}
		b.Block.ParentID = knowledge.ID(parent.String)
		b.TopicTitle = knowledge.FirstLine(topicContent)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
	}
	return out, nil
}
