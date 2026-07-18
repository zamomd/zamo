package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// IDGenerator produces a new unique ID. The implementation lives outside
// the domain and is injected here so this package stays
// on the standard library alone.
type IDGenerator func() ID

// FileWriter persists a rendered export. Writing to disk is an effect outside
// the database, so the domain does not do it: the implementation lives outside
// this package and is injected like IDGenerator, keeping knowledge on the
// standard library alone and the export use case testable without a disk.
type FileWriter interface {
	// WriteFiles writes each file into dir. Names are unique within files
	// (RenderMarkdown guarantees it), so order does not matter.
	WriteFiles(ctx context.Context, dir string, files []ExportFile) error
}

// Service holds the product's use cases. It is the only place where the
// logic lives; every input calls its methods.
type Service struct {
	llm    LLMClient
	repo   Repository
	newID  IDGenerator
	writer FileWriter
	now    func() time.Time
}

// NewService wires the domain to its boundaries: the LLM provider,
// storage, and the injected ID, file-writer, and time sources.
func NewService(llm LLMClient, repo Repository, newID IDGenerator, writer FileWriter, now func() time.Time) *Service {
	return &Service{llm: llm, repo: repo, newID: newID, writer: writer, now: now}
}

// ExpandTopic expands a topic into a block tree and puts the result into
// the review queue.
func (s *Service) ExpandTopic(ctx context.Context, topic, hint string) (Proposal, error) {
	ops, err := s.llm.GenerateTree(ctx, topic, hint)
	if err != nil {
		return Proposal{}, err
	}
	return s.SubmitProposal(ctx, SourceGUIExpand, topic, ops)
}

// ProposeEdit turns a user's text command ("split this block into more
// detail") into a structure-edit proposal — through the same review queue.
// The scope subtree is read from storage and handed to the model as context;
// the resulting operations are queued as a pending gui_edit proposal.
func (s *Service) ProposeEdit(ctx context.Context, scope ID, instruction string) (Proposal, error) {
	blocks, err := s.repo.GetSubtree(ctx, scope)
	if err != nil {
		return Proposal{}, err
	}
	ops, err := s.llm.GenerateEdit(ctx, blocks, instruction)
	if err != nil {
		return Proposal{}, err
	}
	return s.SubmitProposal(ctx, SourceGUIEdit, instruction, ops)
}

// SubmitProposal puts ready-made operations into the review queue.
// The entry point for sources that bring their own operations.
func (s *Service) SubmitProposal(ctx context.Context, source Source, title string, ops []Op) (Proposal, error) {
	p, err := NewProposal(s.newID(), source, title, ops, s.now())
	if err != nil {
		return Proposal{}, err
	}
	if err := s.repo.SaveProposal(ctx, p); err != nil {
		return Proposal{}, err
	}
	return p, nil
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
	// Only a pending proposal can be applied; a decided or unknown id is not in
	// this queue. Storage re-checks under the write lock, so this read need not be
	// transactional.
	p, err := s.pendingProposal(ctx, id)
	if err != nil {
		return err
	}

	ops, err := assemble(p, selected, s.newID)
	if err != nil {
		return err
	}
	return s.repo.ApplyProposal(ctx, id, ops)
}

// pendingProposal finds a proposal in the pending queue. A decided or unknown
// id is not there: ErrNotPending.
func (s *Service) pendingProposal(ctx context.Context, id ID) (Proposal, error) {
	pending, err := s.repo.ListProposals(ctx, StatusPending)
	if err != nil {
		return Proposal{}, err
	}
	for _, cand := range pending {
		if cand.ID == id {
			return cand, nil
		}
	}
	return Proposal{}, fmt.Errorf("proposal %s: %w", id, ErrNotPending)
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

// LinkedBlocks returns the block's typed links hydrated with the neighbour
// blocks. An edge whose neighbour vanished mid-read is dropped rather than
// reported: the next read no longer sees the edge either.
func (s *Service) LinkedBlocks(ctx context.Context, id ID) ([]LinkedBlock, error) {
	edges, err := s.repo.GetEdges(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := make([]ID, len(edges))
	for i, e := range edges {
		ids[i] = e.To
		if e.From != id {
			ids[i] = e.From
		}
	}
	blocks, err := s.repo.GetBlocks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]LinkedBlock, 0, len(edges))
	for i, e := range edges {
		b, ok := blocks[ids[i]]
		if !ok {
			continue
		}
		out = append(out, LinkedBlock{Relation: e.Relation, Outgoing: e.From == id, Block: b})
	}
	return out, nil
}

// CreateLink records a typed link directly, without the review queue: review
// guards the base against AI, not against its owner (like EditBlockContent).
// An empty relation defaults to related, mirroring the link op.
func (s *Service) CreateLink(ctx context.Context, from, to ID, rel Relation) error {
	if from == to {
		return fmt.Errorf("link %s to itself: %w", from, ErrInvalidLink)
	}
	if rel == "" {
		rel = RelationRelated
	}
	if rel != RelationExample && rel != RelationRelated {
		return fmt.Errorf("link %s->%s relation %q: %w", from, to, rel, ErrInvalidLink)
	}
	return s.repo.AddEdge(ctx, from, to, rel)
}

// RemoveLink deletes a typed link directly — the owner's counterpart of
// CreateLink. Removing an absent link is a no-op.
func (s *Service) RemoveLink(ctx context.Context, from, to ID, rel Relation) error {
	return s.repo.RemoveEdge(ctx, from, to, rel)
}

// EditBlockContent applies a manual text edit directly, without the
// review queue: review guards the base against AI, not against its owner.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	return s.repo.EditBlockContent(ctx, id, content)
}

// ExportMarkdown exports the base: one markdown file per topic, block nesting
// rendered as nested lists (RenderMarkdown). It reads every topic's full
// subtree, renders the set, then hands the files to the injected writer. All
// reads complete before any write, so a read failure aborts with nothing
// written; how a mid-write failure is handled is the writer's concern.
//
// ponytail: one GetSubtree query per topic (N+1). Fine for a personal base;
// upgrade to a single bulk "all subtrees" read if export ever gets slow.
func (s *Service) ExportMarkdown(ctx context.Context, dir string) error {
	topics, err := s.repo.GetTopics(ctx)
	if err != nil {
		return err
	}

	var subtrees [][]Block
	for _, t := range topics {
		if !t.IsTopic() { // GetTopics also returns first-level children; skip them
			continue
		}
		sub, err := s.repo.GetSubtree(ctx, t.ID)
		if err != nil {
			return err
		}
		subtrees = append(subtrees, sub)
	}

	links, err := s.exportLinks(ctx, subtrees)
	if err != nil {
		return err
	}
	return s.writer.WriteFiles(ctx, dir, RenderMarkdown(subtrees, links))
}

// exportLinks collects each block's outgoing typed links for the export,
// hydrated with the first line of the target block so the connection reads in
// plain markdown. Edges whose target vanished mid-read are dropped, like
// LinkedBlocks. Only outgoing edges are rendered, so a link appears once, under
// its source.
//
// ponytail: one GetEdges per block (N+1). Fine for a personal base; batch into
// a single edges read if export ever gets slow (same corner as the subtree N+1
// above).
func (s *Service) exportLinks(ctx context.Context, subtrees [][]Block) (map[ID][]ExportLink, error) {
	outgoing := make(map[ID][]Edge)
	targets := make(map[ID]bool)
	for _, sub := range subtrees {
		for _, block := range sub {
			edges, err := s.repo.GetEdges(ctx, block.ID)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				if e.From != block.ID {
					continue // render a link once, under its source
				}
				outgoing[block.ID] = append(outgoing[block.ID], e)
				targets[e.To] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	ids := make([]ID, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	blocks, err := s.repo.GetBlocks(ctx, ids)
	if err != nil {
		return nil, err
	}
	links := make(map[ID][]ExportLink, len(outgoing))
	for blockID, edges := range outgoing {
		for _, e := range edges {
			target, ok := blocks[e.To]
			if !ok {
				continue // the neighbour vanished mid-read
			}
			first, _, _ := strings.Cut(target.Content, "\n")
			links[blockID] = append(links[blockID], ExportLink{Relation: e.Relation, Target: first})
		}
	}
	return links, nil
}
