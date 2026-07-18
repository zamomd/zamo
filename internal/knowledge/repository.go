package knowledge

import "context"

// Repository is the boundary with storage. The interface is declared on
// the consumer side (this package); the implementation is internal/sqlite.
type Repository interface {
	GetTopics(ctx context.Context) ([]Block, error)
	GetSubtree(ctx context.Context, root ID) ([]Block, error)
	GetBlocks(ctx context.Context, ids []ID) (map[ID]Block, error)

	// ApplyOps applies a prepared, ordered batch in one transaction,
	// writing one revision per touched block plus the projection.
	ApplyOps(ctx context.Context, groupID ID, meta WriteMeta, ops []Op) error

	ListRevisions(ctx context.Context, blockID ID) ([]Revision, error)
	DeleteRevision(ctx context.Context, blockID, revID ID) error
	// RejectBlock deletes the block's contiguous unreviewed head
	// revisions and rebuilds the projection.
	RejectBlock(ctx context.Context, blockID ID) error
	RejectPage(ctx context.Context, topicID ID) error
	MarkBlockReviewed(ctx context.Context, blockID ID) error
	MarkGroupReviewed(ctx context.Context, groupID ID) error
	MarkPageReviewed(ctx context.Context, topicID ID) error
	ListUnreviewed(ctx context.Context) (UnreviewedChanges, error)

	GetEdges(ctx context.Context, id ID) ([]Edge, error)
	AddEdge(ctx context.Context, from, to ID, rel Relation, meta WriteMeta) error
	RemoveEdge(ctx context.Context, from, to ID, rel Relation) error
	MarkEdgeReviewed(ctx context.Context, from, to ID, rel Relation) error
	RejectEdge(ctx context.Context, from, to ID, rel Relation) error

	Search(ctx context.Context, query string) ([]Block, error)
}
