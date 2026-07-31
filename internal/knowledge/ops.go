package knowledge

import "fmt"

// Source says where a write came from. The set is open: the scheduler,
// chat import and other future inputs will add their own values.
type Source string

// The write sources of the MVP.
const (
	SourceGUIExpand Source = "gui_expand" // topic expansion from the app
	SourceGUIEdit   Source = "gui_edit"   // AI edit command from the app
	SourceMCP       Source = "mcp"        // write from an MCP tool
)

// SourceGUIManual marks the owner's direct edits from the app.
const SourceGUIManual Source = "gui_manual"

// OpKind is the kind of a single operation inside a write batch.
type OpKind string

// The operation kinds of the write batch format.
const (
	OpAdd    OpKind = "add"
	OpEdit   OpKind = "edit"
	OpMove   OpKind = "move"
	OpDelete OpKind = "delete"
)

// Op is a single operation of a write batch. One format for every source —
// topic expansion, AI edits, MCP tools, the owner's manual edits — each
// emitting whichever of the four kinds it needs. New blocks reference each
// other via TempID; real IDs are assigned on apply.
type Op struct {
	Kind    OpKind `json:"op"`
	TempID  string `json:"temp_id,omitzero"`  // temporary ID of a block being added
	Parent  string `json:"parent,omitzero"`   // TempID or ID of an existing block; empty means topic root
	BlockID ID     `json:"block_id,omitzero"` // existing block (edit/move/delete)
	Content string `json:"content,omitzero"`

	// Anchor: where among its siblings a new or moved block lands. At most
	// one may be set; both empty appends at the end. A TempID of this batch
	// or an existing block ID, resolved like Parent. Needed because position
	// is part of every revision snapshot: inserting by shifting the
	// neighbours would leave their head revisions remembering a position they
	// no longer hold.
	//
	// Two ops in the same batch anchored on the same sibling land in reverse
	// batch order: the second takes the midpoint of the gap the first op just
	// created, which sits between the first op's block and the shared anchor.
	// To insert a run of siblings in order, chain the anchors instead — each
	// op anchored on the previous op's TempID — which tempRefs already orders
	// correctly.
	After  string `json:"after,omitzero"`
	Before string `json:"before,omitzero"`

	// Provenance: why the AI makes this change and where it came from.
	// The review screen shows both fields.
	Rationale string `json:"rationale,omitzero"`
	Origin    string `json:"origin,omitzero"`
}

// validate checks the op's shape: the kind is known and its required fields
// are present. Existence and cycle checks depend on the base's state and
// belong to prepareOps.
func (op Op) validate() error {
	if op.After != "" && op.Before != "" {
		return fmt.Errorf("%s op anchored both after %q and before %q: %w",
			op.Kind, op.After, op.Before, ErrInvalidOps)
	}
	if op.After != "" || op.Before != "" {
		switch op.Kind {
		case OpAdd, OpMove:
			// The only two kinds that place a block among siblings.
		default:
			return fmt.Errorf("%s op carries an anchor, which it ignores: %w", op.Kind, ErrInvalidOps)
		}
	}
	switch op.Kind {
	case OpAdd:
		if op.TempID == "" {
			return fmt.Errorf("add op with empty temp id: %w", ErrInvalidOps)
		}
	case OpEdit, OpMove, OpDelete:
		if op.BlockID == "" {
			return fmt.Errorf("%s op with empty block id: %w", op.Kind, ErrInvalidOps)
		}
	default:
		return fmt.Errorf("unknown op kind %q: %w", op.Kind, ErrInvalidOps)
	}
	return nil
}
