package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// GetBacklinks implements knowledge.Repository. Both reference kinds resolve
// in one query: a block reference matches the target id directly, a topic
// reference matches any name the target answers to (see topicNames), which
// only a root has. A reference naming a topic that does not exist matches
// nothing, which is how an unresolved [[Topic]] stays out of the result
// without a special case.
// A source that references the target by both name and id matches the WHERE
// twice with an otherwise identical row, so the SELECT is DISTINCT. Ordered
// by source id — ULID order, so creation order.
func (r *Repo) GetBacklinks(ctx context.Context, target knowledge.ID) ([]knowledge.Backlink, error) {
	const q = topicRoots + `
SELECT DISTINCT s.id, s.parent_id, s.position, s.content, ` + createdByOpen + `s` + createdByClose + `,
	s.created_at, s.updated_at,
	EXISTS(SELECT 1 FROM block_revisions rv WHERE rv.block_id = s.id AND rv.reviewed = 0),
	r.root, t.content
FROM refs f
JOIN blocks s ON s.id = f.source_id
JOIN roots r ON r.id = s.id
JOIN blocks t ON t.id = r.root
JOIN blocks target ON target.id = ?
WHERE (f.kind = 'block' AND f.target_key = target.id)
   OR (f.kind = 'topic' AND f.target_key IN (SELECT name_key FROM ` + topicNames + ` WHERE topic_id = target.id))
ORDER BY s.id`

	names, err := queryIDs(ctx, r.db,
		`SELECT name_key FROM `+topicNames+` WHERE topic_id = ?`, string(target))
	if err != nil {
		return nil, fmt.Errorf("get backlinks of %s: %w", target, err)
	}
	rows, err := r.db.QueryContext(ctx, q, string(target))
	if err != nil {
		return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
	}
	defer rows.Close()

	var out []knowledge.Backlink
	for rows.Next() {
		var b knowledge.Backlink
		var parent sql.NullString
		var created, updated string
		var topicContent string
		if err := rows.Scan(&b.Block.ID, &parent, &b.Block.Position, &b.Block.Content,
			&b.Block.CreatedBy, &created, &updated, &b.Block.Unreviewed, &b.TopicID, &topicContent); err != nil {
			return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
		}
		if err := decodeBlockFields(&b.Block, parent, created, updated); err != nil {
			return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
		}
		b.TopicTitle = knowledge.FirstLine(topicContent)
		b.Excerpt = knowledge.BacklinkExcerpt(b.Block.Content, target, names...)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get backlinks of %s: %w", target, translate(err))
	}
	return out, nil
}
