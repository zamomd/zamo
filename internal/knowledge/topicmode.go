package knowledge

import (
	"context"
	"fmt"
	"strconv"
)

// SetTopicMode converts a topic between its two shapes. The batch is atomic:
// the root's new content is only true together with the children the conversion
// removes or creates, so review and undo take it whole.
//
// Both directions keep the root's first line exactly as it was, so the topic
// keeps its name and no [[Topic]] reference is touched. Converting a topic to
// the mode it already has is not a write.
func (s *Service) SetTopicMode(ctx context.Context, id ID, mode Mode) (Applied, error) {
	if !mode.valid() {
		return Applied{}, fmt.Errorf("set mode of %s: unknown mode %q: %w", id, mode, ErrInvalidOps)
	}
	sub, err := s.repo.GetSubtree(ctx, id)
	if err != nil {
		return Applied{}, fmt.Errorf("set mode of %s: %w", id, err)
	}
	root := sub[0]
	if !root.IsTopic() {
		return Applied{}, fmt.Errorf("set mode of %s: not a topic root: %w", id, ErrInvalidOps)
	}
	if root.Mode == mode {
		return Applied{}, nil
	}

	var ops []Op
	if mode == ModeDoc {
		ops = collapseToDocument(sub)
	} else {
		ops = expandToOutline(root)
	}
	meta := WriteMeta{
		Author: AuthorOwner,
		Source: SourceGUIManual,
		Title:  fmt.Sprintf("Convert topic to %s", mode),
		Atomic: true,
	}
	return s.ApplyOps(ctx, meta, ops)
}

// collapseToDocument writes the whole subtree into the root and removes every
// child. Children are deleted from the root's direct ones only: applyDelete
// tombstones each one's subtree, so naming a deeper block as well would delete
// it twice.
func collapseToDocument(sub []Block) []Op {
	ops := []Op{{
		Kind:    OpEdit,
		BlockID: sub[0].ID,
		Content: RenderDocument(sub),
		Mode:    ModeDoc,
	}}
	for _, b := range sub[1:] {
		if b.ParentID == sub[0].ID {
			ops = append(ops, Op{Kind: OpDelete, BlockID: b.ID})
		}
	}
	return ops
}

// expandToOutline cuts the document into blocks under the root. Each child is
// anchored to its parent by temp id, so the batch builds the tree in one pass
// and prepareOps orders the adds parents-first.
func expandToOutline(root Block) []Op {
	line, children := SplitDocument(root.Content)
	ops := []Op{{Kind: OpEdit, BlockID: root.ID, Content: line, Mode: ModeOutline}}
	// open is the chain of ancestors the next block can hang from, deepest last.
	// Popping every entry at or below the block's own depth leaves the nearest
	// block the document is still inside, which is its parent; an empty chain
	// means the root. Popping rather than indexing by depth is what makes both
	// irregular shapes come out right: a document that skips a level going
	// deeper — an h2 followed by an h4, or a list some editor indented four
	// spaces — finds the ancestor it does have, and one that pulls back out
	// cannot reach a block in the branch it has left.
	type ancestor struct {
		depth int
		temp  string
	}
	var open []ancestor
	for i, child := range children {
		for len(open) > 0 && open[len(open)-1].depth >= child.Depth {
			open = open[:len(open)-1]
		}
		parent := string(root.ID)
		if len(open) > 0 {
			parent = open[len(open)-1].temp
		}
		temp := "b" + strconv.Itoa(i)
		ops = append(ops, Op{Kind: OpAdd, TempID: temp, Parent: parent, Content: child.Text})
		open = append(open, ancestor{depth: child.Depth, temp: temp})
	}
	return ops
}
