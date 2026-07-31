package knowledge

import (
	"context"
	"fmt"
)

// The owner's structural editing operations: what the keyboard does to the
// outline. Each emits one atomic op batch through ApplyOps with
// Author: AuthorOwner, so every one of them is born reviewed — the review
// queue guards against the model, not against the owner.

// ownerTitle labels every owner-authored batch in revision_groups. ApplyOps
// refuses an empty title.
const ownerTitle = "manual edit"

// ownerMeta is the provenance every outline operation writes with.
func ownerMeta() WriteMeta {
	return WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: ownerTitle}
}

// OutlineChange identifies an outline operation for undo and caret placement.
// BlockID is empty when the operation does not create a block.
type OutlineChange struct {
	GroupID ID
	BlockID ID
}

// blockByID reads one block, reporting ErrNotFound rather than an empty value
// so every caller below can refuse before it builds a batch.
func (s *Service) blockByID(ctx context.Context, id ID) (Block, error) {
	found, err := s.repo.GetBlocks(ctx, []ID{id})
	if err != nil {
		return Block{}, err
	}
	b, ok := found[id]
	if !ok {
		return Block{}, fmt.Errorf("block %s: %w", id, ErrNotFound)
	}
	return b, nil
}

// SplitBlock is Enter with a caret inside a block: the block keeps head, and a
// new sibling directly below it takes tail.
//
// The two halves are siblings even when the block has children: a split
// produces two equal parts, and the tail must not land a level deeper than the
// head.
func (s *Service) SplitBlock(ctx context.Context, id ID, head, tail string) (OutlineChange, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return OutlineChange{}, err
	}
	if b.IsTopic() {
		return OutlineChange{}, fmt.Errorf("split %s: a topic root has no siblings: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpEdit, BlockID: id, Content: head},
		{Kind: OpAdd, TempID: "tail", Parent: string(b.ParentID), After: string(id), Content: tail},
	})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID, BlockID: applied.IDs["tail"]}, nil
}

// InsertBefore is Enter with the caret at the very start of a non-empty block:
// an empty sibling appears above and the block itself is not touched, so it
// keeps its id and with it its revision history, its review state and its
// links.
func (s *Service) InsertBefore(ctx context.Context, id ID, content string) (OutlineChange, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return OutlineChange{}, err
	}
	if b.IsTopic() {
		return OutlineChange{}, fmt.Errorf("insert before %s: a topic root has no siblings: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpAdd, TempID: "head", Parent: string(b.ParentID), Before: string(id), Content: content},
	})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID, BlockID: applied.IDs["head"]}, nil
}

// siblingsOf returns the ordered children of b's parent together with b's
// index among them. The parent's subtree comes back in pre-order with each
// parent's children already sorted by position (see the Repository contract),
// so filtering direct children preserves their order.
//
// b must not be a topic root: roots live under no parent, and no outline
// operation is defined on them.
func (s *Service) siblingsOf(ctx context.Context, b Block) ([]Block, int, error) {
	subtree, err := s.repo.GetSubtree(ctx, b.ParentID)
	if err != nil {
		return nil, 0, err
	}
	siblings := make([]Block, 0, len(subtree))
	for _, sib := range subtree {
		if sib.ParentID == b.ParentID {
			siblings = append(siblings, sib)
		}
	}
	for i, sib := range siblings {
		if sib.ID == b.ID {
			return siblings, i, nil
		}
	}
	return nil, 0, fmt.Errorf("block %s is not among the children of %s: %w",
		b.ID, b.ParentID, ErrNotFound)
}

// IndentBlock is Tab: the block becomes the last child of the sibling above
// it, carrying its own children along. The first child of any parent has
// nothing to be indented under, and that is a refusal, not a silent no-op —
// the caller decides whether to show it.
func (s *Service) IndentBlock(ctx context.Context, id ID) (OutlineChange, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return OutlineChange{}, err
	}
	if b.IsTopic() {
		return OutlineChange{}, fmt.Errorf("indent %s: a topic root cannot be indented: %w", id, ErrInvalidOps)
	}
	siblings, i, err := s.siblingsOf(ctx, b)
	if err != nil {
		return OutlineChange{}, err
	}
	if i == 0 {
		return OutlineChange{}, fmt.Errorf("indent %s: no previous sibling: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpMove, BlockID: id, Parent: string(siblings[i-1].ID)},
	})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID}, nil
}

// OutdentBlock is Shift+Tab: the block becomes its parent's next sibling,
// carrying its own children along. Its former following siblings stay where
// they are — the gesture moves one block, it does not re-cut the branch.
//
// A child of a topic root is refused: outdenting it would turn it into a
// second topic, which is a different act entirely.
func (s *Service) OutdentBlock(ctx context.Context, id ID) (OutlineChange, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return OutlineChange{}, err
	}
	if b.IsTopic() {
		return OutlineChange{}, fmt.Errorf("outdent %s: a topic root cannot be outdented: %w", id, ErrInvalidOps)
	}
	parent, err := s.blockByID(ctx, b.ParentID)
	if err != nil {
		return OutlineChange{}, err
	}
	if parent.IsTopic() {
		return OutlineChange{}, fmt.Errorf("outdent %s: would create a second topic: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpMove, BlockID: id, Parent: string(parent.ParentID), After: string(parent.ID)},
	})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID}, nil
}

// MergeBlocks is Backspace at the start of a block: target absorbs source's
// text, source's children become target's children, and source is gone. The
// caret goes to the seam, which the caller computes as the length of target's
// old content.
//
// The op order is load-bearing. applyDelete tombstones the whole subtree of
// what it deletes, so source's children must be reparented before the delete
// runs. prepareOps preserves this order because every id in the batch is
// real: with no temp references every op has indegree zero, and
// orderParentsFirst is a stable Kahn traversal, so it emits them as given.
func (s *Service) MergeBlocks(ctx context.Context, target, source ID) (OutlineChange, error) {
	if target == source {
		return OutlineChange{}, fmt.Errorf("merge %s into itself: %w", target, ErrInvalidOps)
	}
	dst, err := s.blockByID(ctx, target)
	if err != nil {
		return OutlineChange{}, err
	}
	src, err := s.blockByID(ctx, source)
	if err != nil {
		return OutlineChange{}, err
	}
	if src.IsTopic() {
		return OutlineChange{}, fmt.Errorf("merge %s: a topic root has no siblings: %w", source, ErrInvalidOps)
	}
	if dst.IsTopic() {
		return OutlineChange{}, fmt.Errorf("merge into %s: a topic root cannot absorb text: %w", target, ErrInvalidOps)
	}
	subtree, err := s.repo.GetSubtree(ctx, source)
	if err != nil {
		return OutlineChange{}, err
	}
	ops := make([]Op, 0, len(subtree)+2)
	ops = append(ops, Op{Kind: OpEdit, BlockID: target, Content: dst.Content + src.Content})
	for _, b := range subtree {
		if b.ParentID == source {
			ops = append(ops, Op{Kind: OpMove, BlockID: b.ID, Parent: string(target)})
		}
	}
	ops = append(ops, Op{Kind: OpDelete, BlockID: source})
	applied, err := s.ApplyOps(ctx, ownerMeta(), ops)
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID}, nil
}

// DeleteBlock removes the block and everything beneath it, through storage's
// cascade — including a whole topic when id names a topic root. There is no
// guard here: unlike indent or split, delete is not meaningless on a root,
// and collapsing a topic is a legitimate act. The caller decides when that is
// appropriate.
func (s *Service) DeleteBlock(ctx context.Context, id ID) (OutlineChange, error) {
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{{Kind: OpDelete, BlockID: id}})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID}, nil
}

// MoveBlock reparents a block, landing it after the named sibling or at the
// end when after is empty. Indent and outdent are the keyboard's two fixed
// cases; this is the general form, for a future drag.
//
// A topic root is refused as the block being moved: reparenting a topic
// under a block would un-root it, which is really merging two topics — a
// different act than this method's job.
func (s *Service) MoveBlock(ctx context.Context, id, parent, after ID) (OutlineChange, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return OutlineChange{}, err
	}
	if b.IsTopic() {
		return OutlineChange{}, fmt.Errorf("move %s: a topic root cannot be moved: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpMove, BlockID: id, Parent: string(parent), After: string(after)},
	})
	if err != nil {
		return OutlineChange{}, err
	}
	return OutlineChange{GroupID: applied.GroupID}, nil
}
