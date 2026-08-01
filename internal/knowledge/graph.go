package knowledge

// TopicNode is one node of the topic graph: a topic root and how many blocks
// its subtree holds, the root included. The count drives node size, which is
// what keeps the map readable on a base that has no links at all.
type TopicNode struct {
	ID     ID
	Title  string
	Blocks int
}

// TopicEdge is an undirected connection between two topics. Multiplicity
// collapses — many connections between two topics are one edge — and so does
// direction, so From is always the smaller id. A topic never edges to itself:
// a connection between two blocks of the same topic says nothing about the
// map.
type TopicEdge struct {
	From ID
	To   ID
}

// TopicGraph is the whole map in one read. Topics come in id order, which is
// ULID order, which is creation order — the graph layout seeds node positions
// by index, so a stable order is what keeps the map from reshuffling when a
// topic is added. Every edge's endpoints are present in Topics: Topics and
// Edges come from two unsynchronized reads, so a write landing between them
// could otherwise hand back an edge naming a topic the read missed; the
// storage implementation is responsible for filtering that case out before
// returning.
type TopicGraph struct {
	Topics []TopicNode
	Edges  []TopicEdge
}
