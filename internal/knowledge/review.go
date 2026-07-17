package knowledge

import "context"

// OpReview is one operation annotated for the review screen.
type OpReview struct {
	Op             Op
	CurrentContent string // current text of an edit target, for the diff
	Stale          bool   // the op references an existing block that is gone
	// DeletesAlso lists the descendants a delete will also remove, so the
	// review can show the true scope of the delete. Empty for other kinds and
	// for a delete of a childless or vanished block. The parallel op list is
	// unchanged: the scope hangs off the delete, it is not its own op.
	DeletesAlso []Block
}

// ProposalReview is a pending proposal annotated for the review screen:
// Ops is parallel to Proposal.Ops.
type ProposalReview struct {
	Proposal Proposal
	Ops      []OpReview
}

// ReviewProposal annotates a pending proposal for review: each op carries a
// stale flag (it references an existing block that vanished) and, for edits,
// the target's current content so the screen can diff against it. References
// to blocks created inside the proposal (temp ids) are never stale.
func (s *Service) ReviewProposal(ctx context.Context, id ID) (ProposalReview, error) {
	p, err := s.pendingProposal(ctx, id)
	if err != nil {
		return ProposalReview{}, err
	}

	temps := proposalTempIDs(p.Ops)
	ids := make([]ID, 0, len(p.Ops))
	seen := make(map[ID]bool)
	refsByOp := make([][]ID, len(p.Ops))
	for i, op := range p.Ops {
		refsByOp[i] = existingRefs(op, temps)
		for _, ref := range refsByOp[i] {
			if !seen[ref] {
				seen[ref] = true
				ids = append(ids, ref)
			}
		}
	}
	blocks, err := s.repo.GetBlocks(ctx, ids)
	if err != nil {
		return ProposalReview{}, err
	}

	review := make([]OpReview, len(p.Ops))
	for i, op := range p.Ops {
		r := OpReview{Op: op}
		for _, ref := range refsByOp[i] {
			if _, ok := blocks[ref]; !ok {
				r.Stale = true
			}
		}
		if op.Kind == OpEdit {
			r.CurrentContent = blocks[op.BlockID].Content
		}
		review[i] = r
	}
	return ProposalReview{Proposal: p, Ops: review}, nil
}

// existingRefs lists the existing-block ids an op depends on: its target, its
// parent, its link ends — skipping temp ids (blocks the proposal itself
// creates) and the empty parent (the topic root).
func existingRefs(op Op, temps map[string]bool) []ID {
	var refs []ID
	add := func(raw string) {
		if raw != "" && !temps[raw] {
			refs = append(refs, ID(raw))
		}
	}
	switch op.Kind {
	case OpAdd:
		add(op.Parent)
	case OpEdit, OpDelete:
		add(string(op.BlockID))
	case OpMove:
		add(string(op.BlockID))
		add(op.Parent)
	case OpLink:
		add(op.From)
		add(op.To)
	}
	return refs
}
