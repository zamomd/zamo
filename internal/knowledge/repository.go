package knowledge

import "context"

// Repository is the boundary with storage. The interface is declared on
// the consumer side (this package); the implementation is internal/sqlite.
type Repository interface {
	GetTopics(ctx context.Context) ([]Block, error)
	GetSubtree(ctx context.Context, root ID) ([]Block, error)
	GetBlocks(ctx context.Context, ids []ID) (map[ID]Block, error)
	EditBlockContent(ctx context.Context, id ID, content string) error

	SaveProposal(ctx context.Context, p Proposal) error
	ListProposals(ctx context.Context, status Status) ([]Proposal, error)

	ApplyProposal(ctx context.Context, id ID, ops []Op) error
	RejectProposal(ctx context.Context, id ID) error

	Search(ctx context.Context, query string) ([]Block, error)
}
