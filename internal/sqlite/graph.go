package sqlite

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// topicRoots pairs every block with the topic root it descends from; topicNodes
// builds on it. UNION, not UNION ALL: on a database whose parent_id somehow
// became cyclic, UNION discards the repeated row once a block is already in
// roots, so the walk terminates instead of looping forever — the same guard
// GetSubtree uses.
const topicRoots = `
WITH RECURSIVE roots(id, root) AS (
    SELECT id, id FROM blocks WHERE parent_id IS NULL
    UNION
    SELECT b.id, r.root FROM blocks b JOIN roots r ON b.parent_id = r.id
)`

// GetTopicGraph implements knowledge.Repository: the topic-level map, every
// topic paired with the edges between them.
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
// is how TopicGraph's invariant — every edge's endpoints are present in
// Topics — is kept.
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

// topicEdges returns the edges between topics. There is currently no source
// for a topic-to-topic connection, so it always reports none.
func (r *Repo) topicEdges(ctx context.Context) ([]knowledge.TopicEdge, error) {
	return nil, nil
}
