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
	// parents[d] is the temp id of the most recent block at depth d, which is
	// what a block at depth d+1 hangs from. Depth 0 hangs from the root itself.
	parents := map[int]string{}
	for i, child := range children {
		temp := "b" + strconv.Itoa(i)
		parent := string(root.ID)
		if child.Depth > 0 {
			if p, ok := parents[child.Depth-1]; ok {
				parent = p
			}
		}
		ops = append(ops, Op{Kind: OpAdd, TempID: temp, Parent: parent, Content: child.Text})
		parents[child.Depth] = temp
	}
	return ops
}
