package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// The bounds of one write. A batch must stay reviewable by a human, so one
// that is too large or too deep is refused rather than applied.
const (
	// MaxOps is the most operations in a batch.
	MaxOps = 60
	// MaxDepth is the most levels of new blocks nested inside one another.
	MaxDepth = 5
)

// Writer applies a write batch through the single write path.
type Writer interface {
	ApplyOps(ctx context.Context, meta knowledge.WriteMeta, ops []knowledge.Op) (knowledge.Applied, error)
}

// Stamp is the provenance a caller puts on every write made through its
// tool. The model chooses neither: it supplies only the batch title.
type Stamp struct {
	// Origin is recorded on each operation.
	Origin string
	// Meta builds the batch metadata around the model's title.
	Meta func(title string) knowledge.WriteMeta
}

// opInput is the model-facing shape of one operation. It mirrors the domain
// op but omits Origin: provenance is the caller's to assign.
type opInput struct {
	Kind      string `json:"op" jsonschema:"the operation: add/edit/move/delete"`
	TempID    string `json:"temp_id,omitzero" jsonschema:"temporary id of a block being added"`
	Parent    string `json:"parent,omitzero" jsonschema:"temp_id or existing id; empty means topic root"`
	BlockID   string `json:"block_id,omitzero" jsonschema:"existing block to edit/move/delete"`
	Content   string `json:"content,omitzero" jsonschema:"the block content"`
	Mode      string `json:"mode,omitzero" jsonschema:"outline or doc; only on a topic root, empty leaves the shape alone"`
	After     string `json:"after,omitzero" jsonschema:"sibling (temp_id or id) an added or moved block lands after"`
	Before    string `json:"before,omitzero" jsonschema:"sibling (temp_id or id) an added or moved block lands before"`
	Rationale string `json:"rationale,omitzero" jsonschema:"why the AI makes this change"`
}

type writeInput struct {
	Title string    `json:"title" jsonschema:"a short description of this change batch, shown to the reviewer"`
	Ops   []opInput `json:"ops" jsonschema:"the block operations to apply (add/edit/move/delete)"`
}

// WriteOutput reports what a batch did: the group tying its revisions
// together and the real id assigned to every temp id.
type WriteOutput struct {
	GroupID string            `json:"group_id"`
	IDs     map[string]string `json:"ids,omitzero"`
}

// Write returns write_blocks bound to w: the one tool that changes the base.
func Write(w Writer, stamp Stamp) Tool {
	return define("write_blocks",
		"Apply block operations directly. Changes are live immediately but every touched block is "+
			"flagged for the human's review and can be rolled back — write carefully and set "+
			"rationale on every op.\n"+
			"A topic is either an outline (a block tree) or a document (one root holding the whole "+
			"markdown page, no children). An add or edit of a root may carry \"mode\": \"outline\" | "+
			"\"doc\"; omit it to leave the shape alone.\n"+
			fmt.Sprintf("One batch holds at most %d operations and nests new blocks at most %d deep.",
				MaxOps, MaxDepth),
		func(ctx context.Context, in writeInput) (any, error) {
			ops := make([]knowledge.Op, len(in.Ops))
			for i, o := range in.Ops {
				ops[i] = knowledge.Op{
					Kind:      knowledge.OpKind(o.Kind),
					TempID:    o.TempID,
					Parent:    o.Parent,
					BlockID:   knowledge.ID(o.BlockID),
					Content:   o.Content,
					Mode:      knowledge.Mode(o.Mode),
					After:     o.After,
					Before:    o.Before,
					Rationale: o.Rationale,
					Origin:    stamp.Origin,
				}
			}
			if err := checkCaps(ops); err != nil {
				return nil, err
			}
			applied, err := w.ApplyOps(ctx, stamp.Meta(in.Title), ops)
			if err != nil {
				return nil, explain(err)
			}
			ids := make(map[string]string, len(applied.IDs))
			for temp, id := range applied.IDs {
				ids[temp] = string(id)
			}
			return WriteOutput{GroupID: string(applied.GroupID), IDs: ids}, nil
		})
}

func checkCaps(ops []knowledge.Op) error {
	if len(ops) > MaxOps {
		return fmt.Errorf("%d operations in one batch, at most %d: split the change: %w",
			len(ops), MaxOps, knowledge.ErrInvalidOps)
	}
	depth := make(map[string]int, len(ops))
	for _, op := range ops {
		if op.Kind != knowledge.OpAdd || op.TempID == "" {
			continue
		}
		d := depth[op.Parent] + 1
		if d > MaxDepth {
			return fmt.Errorf("new blocks nested more than %d deep under %q: flatten the tree: %w",
				MaxDepth, op.TempID, knowledge.ErrInvalidOps)
		}
		depth[op.TempID] = d
	}
	return nil
}

// explain turns a domain error into something a model can act on. The
// document refusal names the write that would have worked, because the model
// has no other way to learn the topic's shape mid-batch.
func explain(err error) error {
	if errors.Is(err, knowledge.ErrDocumentHasChildren) {
		return fmt.Errorf("%w; edit its root content instead of adding children", err)
	}
	return err
}
