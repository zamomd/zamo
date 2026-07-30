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
