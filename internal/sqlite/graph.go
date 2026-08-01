package sqlite

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// topicRoots pairs every block with the topic root it descends from. Every
// query that needs a block's topic builds on it — the two graph queries here
// and GetBacklinks, which needs the source block's topic for context.
// UNION, not UNION ALL: on a database whose parent_id
// somehow became cyclic, UNION discards the repeated row once a block is
// already in roots, so the walk terminates instead of looping forever — the
// same guard GetSubtree uses.
const topicRoots = `
WITH RECURSIVE roots(id, root) AS (
    SELECT id, id FROM blocks WHERE parent_id IS NULL
    UNION
    SELECT b.id, r.root FROM blocks b JOIN roots r ON b.parent_id = r.id
)`

// GetTopicGraph implements knowledge.Repository: the topic-level map. Two
// queries rather than one join — a block count per topic and an edge set have
// different grains, and multiplying them together in SQL only to divide them
// back out in Go would be the more fragile shape. The price is that the two
// reads are unsynchronized, which pruneDanglingEdges pays.
func (r *Repo) GetTopicGraph(ctx context.Context) (knowledge.TopicGraph, error) {
	topics, err := r.topicNodes(ctx)
	if err != nil {
		return knowledge.TopicGraph{}, err
	}
	edges, err := r.topicEdges(ctx)
	if err != nil {
		return knowledge.TopicGraph{}, err
	}
	return knowledge.TopicGraph{Topics: topics, Edges: pruneDanglingEdges(topics, edges)}, nil
}

// pruneDanglingEdges drops any edge whose endpoint is not among topics, which
// is how TopicGraph's invariant is kept: a write landing between the two reads
// — comind mcp deleting a topic after the node read and before the edge read —
// otherwise yields an edge naming a topic that is not there. A read
// transaction would close the window instead, but the DSN sets
// _txlock=immediate (see Open), so it would take a write lock to draw a map.
func pruneDanglingEdges(topics []knowledge.TopicNode, edges []knowledge.TopicEdge) []knowledge.TopicEdge {
	present := make(map[knowledge.ID]bool, len(topics))
	for _, t := range topics {
		present[t.ID] = true
	}
	var out []knowledge.TopicEdge
	for _, e := range edges {
		if present[e.From] && present[e.To] {
			out = append(out, e)
		}
	}
	return out
}

// topicNodes reads every topic root with the size of its subtree. Ordered by
// id — ULID order, so creation order — because the layout seeds node positions
// by index and an unstable order would reshuffle the map between reads.
func (r *Repo) topicNodes(ctx context.Context) ([]knowledge.TopicNode, error) {
	const q = topicRoots + `
SELECT r.root, t.content, COUNT(*)
FROM roots r JOIN blocks t ON t.id = r.root
GROUP BY r.root, t.content
ORDER BY r.root`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("read topic nodes: %w", translate(err))
	}
	defer rows.Close()

	var out []knowledge.TopicNode
	for rows.Next() {
		var n knowledge.TopicNode
		if err := rows.Scan(&n.ID, &n.Title, &n.Blocks); err != nil {
			return nil, fmt.Errorf("read topic nodes: %w", translate(err))
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read topic nodes: %w", translate(err))
	}
	return out, nil
}

// topicEdges reads the undirected topic-level edge set from the reference
// index. A reference contributes an edge between the root of the block that
// carries it and the root it points at: a block reference resolves through
// its target's root, a topic reference resolves against title_key. MIN/MAX
// collapse direction so each pair appears once, and the inequality drops
// references that stay inside one topic — they say nothing about the map.
func (r *Repo) topicEdges(ctx context.Context) ([]knowledge.TopicEdge, error) {
	const q = topicRoots + `,
resolved(source_root, target_root) AS (
    SELECT sr.root, tr.root
    FROM refs f
    JOIN roots sr ON sr.id = f.source_id
    JOIN blocks tb ON tb.id = f.target_key
    JOIN roots tr ON tr.id = tb.id
    WHERE f.kind = 'block'
    UNION
    SELECT sr.root, t.id
    FROM refs f
    JOIN roots sr ON sr.id = f.source_id
    JOIN blocks t ON t.title_key = f.target_key AND t.parent_id IS NULL
    WHERE f.kind = 'topic'
)
SELECT DISTINCT MIN(source_root, target_root), MAX(source_root, target_root)
FROM resolved
WHERE source_root <> target_root
ORDER BY 1, 2`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("read topic edges: %w", translate(err))
	}
	defer rows.Close()

	var out []knowledge.TopicEdge
	for rows.Next() {
		var e knowledge.TopicEdge
		if err := rows.Scan(&e.From, &e.To); err != nil {
			return nil, fmt.Errorf("read topic edges: %w", translate(err))
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read topic edges: %w", translate(err))
	}
	return out, nil
}
