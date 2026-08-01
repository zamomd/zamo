package knowledge

import "context"

// Repository is the boundary with storage. The interface is declared on
// the consumer side (this package); the implementation is internal/sqlite.
type Repository interface {
	GetTopics(ctx context.Context) ([]Block, error)
	GetSubtree(ctx context.Context, root ID) ([]Block, error)
	GetBlocks(ctx context.Context, ids []ID) (map[ID]Block, error)
	// GetTopicGraph returns every topic with its block count plus the edges
	// between topics.
	GetTopicGraph(ctx context.Context) (TopicGraph, error)
	// GetBacklinks returns every block referencing the target, whether by
	// topic name or by block id. An unknown target yields an empty slice.
	GetBacklinks(ctx context.Context, target ID) ([]Backlink, error)
	// BlocksReferencingTopic returns every block whose content carries a
	// [[Topic]] reference resolving to titleKey, so a rename can rewrite
	// them. Ordered by id, for a deterministic batch.
	BlocksReferencingTopic(ctx context.Context, titleKey string) ([]Block, error)

	// ApplyOps applies a prepared, ordered batch in one transaction,
	// writing one revision per touched block plus the projection.
	//
	// An op may carry an optimistic guard — Op.ExpectedContent, and for a
	// rename Op.ExpectedTopicRefs — stating what the domain read while
	// preparing the batch. An implementation must check every guard inside
	// that same transaction and refuse the batch with ErrConcurrentWrite
	// when the base has moved on, or a rename silently overwrites the write
	// that landed between the preparation and the apply.
	ApplyOps(ctx context.Context, groupID ID, meta WriteMeta, ops []Op) error

	ListRevisions(ctx context.Context, blockID ID) ([]Revision, error)
	DeleteRevision(ctx context.Context, blockID, revID ID) error
	// RejectBlock deletes the block's contiguous unreviewed head
	// revisions and rebuilds the projection.
	RejectBlock(ctx context.Context, blockID ID) error
	// UndoGroup rolls back an entire revision group. It reports
	// ErrGroupWrittenOver when a later change sits on one of its blocks,
	// ErrInvalidRollback for a rollback the tree cannot take, and ErrNotFound
	// when the group does not exist.
	UndoGroup(ctx context.Context, groupID ID) error
	RejectPage(ctx context.Context, topicID ID) error
	MarkBlockReviewed(ctx context.Context, blockID ID) error
	MarkGroupReviewed(ctx context.Context, groupID ID) error
	MarkPageReviewed(ctx context.Context, topicID ID) error
	ListUnreviewed(ctx context.Context) (UnreviewedChanges, error)

	Search(ctx context.Context, query string) ([]Block, error)
}
