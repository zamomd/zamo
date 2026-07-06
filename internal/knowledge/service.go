package knowledge

import (
	"context"
	"time"
)

// IDGenerator produces a new unique ID. The implementation lives outside
// the domain and is injected here so this package stays
// on the standard library alone.
type IDGenerator func() ID

// Service holds the product's use cases. It is the only place where the
// logic lives; every input calls its methods.
type Service struct {
	llm   LLMClient
	repo  Repository
	newID IDGenerator
	now   func() time.Time
}

// NewService wires the domain to its boundaries: the LLM provider,
// storage, and the injected ID and time sources.
func NewService(llm LLMClient, repo Repository, newID IDGenerator, now func() time.Time) *Service {
	return &Service{llm: llm, repo: repo, newID: newID, now: now}
}

// ExpandTopic expands a topic into a block tree and puts the result into
// the review queue.
func (s *Service) ExpandTopic(ctx context.Context, topic, hint string) (Proposal, error) {
	return Proposal{}, ErrNotImplemented
}

// ProposeEdit turns a user's text command ("split this block into more
// detail") into a structure-edit proposal — through the same review queue.
func (s *Service) ProposeEdit(ctx context.Context, scope ID, instruction string) (Proposal, error) {
	return Proposal{}, ErrNotImplemented
}

// SubmitProposal puts ready-made operations into the review queue.
// The entry point for sources that bring their own operations.
func (s *Service) SubmitProposal(ctx context.Context, source Source, title string, ops []Op) (Proposal, error) {
	return Proposal{}, ErrNotImplemented
}

// PendingProposals returns the review queue: proposals awaiting
// the user's decision.
func (s *Service) PendingProposals(ctx context.Context) ([]Proposal, error) {
	return s.repo.ListProposals(ctx, StatusPending)
}

// ApplyProposal applies a proposal: the domain validates and orders the
// operations filtered by the user's selection (selected holds indexes
// into Proposal.Ops; operations depending on removed ones are dropped
// in cascade), then storage applies them in a single transaction.
func (s *Service) ApplyProposal(ctx context.Context, id ID, selected []int) error {
	return ErrNotImplemented
}

// RejectProposal declines a pending proposal; nothing reaches the base.
func (s *Service) RejectProposal(ctx context.Context, id ID) error {
	return s.repo.RejectProposal(ctx, id)
}

// Search runs a full-text search over block content.
func (s *Service) Search(ctx context.Context, query string) ([]Block, error) {
	return s.repo.Search(ctx, query)
}

// TopicMap returns the map of the base — a compact topic tree for
// orientation.
func (s *Service) TopicMap(ctx context.Context) ([]Block, error) {
	return s.repo.GetTopics(ctx)
}

// GetSubtree returns a block together with all of its descendants.
func (s *Service) GetSubtree(ctx context.Context, root ID) ([]Block, error) {
	return s.repo.GetSubtree(ctx, root)
}

// EditBlockContent applies a manual text edit directly, without the
// review queue: review guards the base against AI, not against its owner.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	return s.repo.EditBlockContent(ctx, id, content)
}

// ExportMarkdown exports the base: one markdown file per topic,
// block nesting rendered as nested lists.
func (s *Service) ExportMarkdown(ctx context.Context, dir string) error {
	return ErrNotImplemented
}
