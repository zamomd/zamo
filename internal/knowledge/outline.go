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
// new sibling directly below it takes tail. It returns the new block's id so
// the caller can move the caret there.
//
// The two halves are siblings even when the block has children: a split
// produces two equal parts, and the tail must not land a level deeper than the
// head.
func (s *Service) SplitBlock(ctx context.Context, id ID, head, tail string) (ID, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return "", err
	}
	if b.IsTopic() {
		return "", fmt.Errorf("split %s: a topic root has no siblings: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpEdit, BlockID: id, Content: head},
		{Kind: OpAdd, TempID: "tail", Parent: string(b.ParentID), After: string(id), Content: tail},
	})
	if err != nil {
		return "", err
	}
	return applied.IDs["tail"], nil
}

// InsertBefore is Enter with the caret at the very start of a non-empty block:
// an empty sibling appears above and the block itself is not touched, so it
// keeps its id and with it its revision history, its review state and its
// links.
func (s *Service) InsertBefore(ctx context.Context, id ID, content string) (ID, error) {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return "", err
	}
	if b.IsTopic() {
		return "", fmt.Errorf("insert before %s: a topic root has no siblings: %w", id, ErrInvalidOps)
	}
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpAdd, TempID: "head", Parent: string(b.ParentID), Before: string(id), Content: content},
	})
	if err != nil {
		return "", err
	}
	return applied.IDs["head"], nil
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
func (s *Service) IndentBlock(ctx context.Context, id ID) error {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return err
	}
	if b.IsTopic() {
		return fmt.Errorf("indent %s: a topic root cannot be indented: %w", id, ErrInvalidOps)
	}
	siblings, i, err := s.siblingsOf(ctx, b)
	if err != nil {
		return err
	}
	if i == 0 {
		return fmt.Errorf("indent %s: no previous sibling: %w", id, ErrInvalidOps)
	}
	_, err = s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpMove, BlockID: id, Parent: string(siblings[i-1].ID)},
	})
	return err
}

// OutdentBlock is Shift+Tab: the block becomes its parent's next sibling,
// carrying its own children along. Its former following siblings stay where
// they are — the gesture moves one block, it does not re-cut the branch.
//
// A child of a topic root is refused: outdenting it would turn it into a
// second topic, which is a different act entirely.
func (s *Service) OutdentBlock(ctx context.Context, id ID) error {
	b, err := s.blockByID(ctx, id)
	if err != nil {
		return err
	}
	if b.IsTopic() {
		return fmt.Errorf("outdent %s: a topic root cannot be outdented: %w", id, ErrInvalidOps)
	}
	parent, err := s.blockByID(ctx, b.ParentID)
	if err != nil {
		return err
	}
	if parent.IsTopic() {
		return fmt.Errorf("outdent %s: would create a second topic: %w", id, ErrInvalidOps)
	}
	_, err = s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpMove, BlockID: id, Parent: string(parent.ParentID), After: string(parent.ID)},
	})
	return err
}
