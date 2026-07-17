package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ops, err = s.expandDeletes(ctx, ops)
	if err != nil {
		return err
	}
	return s.repo.ApplyProposal(ctx, id, ops)
}

// expandDeletes replaces every delete of an existing block with deletes of its
// whole subtree, descendants first. The parent_id foreign key has no ON DELETE
// CASCADE, so a block cannot be removed while a child still points at it; the
// domain owns the rule that deleting a block deletes what hangs off it, and
// makes it explicit in the op list rather than leaving it to storage. The
// cascade runs against the post-move tree (see deletedBlocks), so a block the
// same proposal moves out of the subtree is spared. Ids already scheduled are
// not repeated; a delete of a block that is already gone passes through so
// storage still surfaces it as stale.
//
// ponytail: one GetSubtree per delete (N+1). Deletes are rare and small; batch
// if a mass delete ever shows up.
func (s *Service) expandDeletes(ctx context.Context, ops []Op) ([]Op, error) {
	moves := collectMoves(ops)
	out := make([]Op, 0, len(ops))
	scheduled := make(map[ID]bool)
	for _, op := range ops {
		if op.Kind != OpDelete {
			out = append(out, op)
			continue
		}
		blocks, err := s.deletedBlocks(ctx, op.BlockID, moves)
		if errors.Is(err, ErrNotFound) {
			out = append(out, op) // already gone; storage surfaces it as stale
			continue
		}
		if err != nil {
			return nil, err
		}
		// deletedBlocks is pre-order (parents first); reverse it so every child
		// is deleted before the parent it references.
		for i := len(blocks) - 1; i >= 0; i-- {
			id := blocks[i].ID
			if scheduled[id] {
				continue
			}
			scheduled[id] = true
			out = append(out, Op{Kind: OpDelete, BlockID: id})
		}
	}
	return out, nil
}

// collectMoves indexes a proposal's move ops as block id → new parent, so the
// delete cascade can be computed against where blocks end up, not where they
// currently sit.
func collectMoves(ops []Op) map[ID]ID {
	moves := make(map[ID]ID)
	for _, op := range ops {
		if op.Kind == OpMove {
			moves[op.BlockID] = ID(op.Parent)
		}
	}
	return moves
}

// deletedBlocks returns the blocks a delete of target actually removes, in
// pre-order and including target itself. The cascade is computed against the
// tree as the proposal's moves leave it: a block reparented out of target's
// subtree (its post-move parent chain no longer reaches target) escapes, and
// so does everything under it. Returns ErrNotFound if target is already gone.
func (s *Service) deletedBlocks(ctx context.Context, target ID, moves map[ID]ID) ([]Block, error) {
	sub, err := s.repo.GetSubtree(ctx, target)
	if err != nil {
		return nil, err
	}
	parent := make(map[ID]ID, len(sub))
	for _, b := range sub {
		p := b.ParentID
		if np, moved := moves[b.ID]; moved {
			p = np
		}
		parent[b.ID] = p
	}
	// A block is removed iff its post-move parent chain still leads to target.
	// Iterate to a fixpoint over the flat subtree; pre-order means one pass
	// usually settles it, but a move can reorder ancestry, so loop until stable.
	removed := map[ID]bool{target: true}
	for changed := true; changed; {
		changed = false
		for _, b := range sub {
			if !removed[b.ID] && removed[parent[b.ID]] {
				removed[b.ID] = true
				changed = true
			}
		}
	}
	out := make([]Block, 0, len(sub))
	for _, b := range sub { // keep pre-order
		if removed[b.ID] {
			out = append(out, b)
		}
	}
	return out, nil
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

// EditBlockContent applies a manual text edit directly, without the
// review queue: review guards the base against AI, not against its owner.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	return s.repo.EditBlockContent(ctx, id, content)
}

// ExportMarkdown exports the base: one markdown file per topic, block nesting
// rendered as nested lists (RenderMarkdown). It reads every topic's full
// subtree, renders the set, then writes the files under dir. All reads complete
// before any write, so a read failure aborts with nothing written; a write
// failure part-way (disk full, permissions) can still leave earlier files on
// disk. Cancellation is honoured between writes.
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

	for _, f := range RenderMarkdown(subtrees) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
