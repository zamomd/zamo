package knowledge

// TopicNode is one node of the topic graph: a topic root and how many blocks
// its subtree holds, the root included. The count drives node size, which is
// what keeps the map readable on a base that has no links at all.
type TopicNode struct {
	ID     ID
	Title  string
	Blocks int
}

// TopicEdge is an undirected link between two topics: it exists when any block
// of one is linked to any block of the other. Multiplicity collapses — ten
// links between two topics are one edge — and so does direction, so From is
// always the smaller id. A topic never edges to itself: a link between two
// blocks of the same topic says nothing about the map.
type TopicEdge struct {
	From ID
	To   ID
}

// TopicGraph is the whole map in one read. Topics come in id order, which is
// ULID order, which is creation order — the graph layout seeds node positions
// by index, so a stable order is what keeps the map from reshuffling when a
// topic is added.
type TopicGraph struct {
	Topics []TopicNode
	Edges  []TopicEdge
}
