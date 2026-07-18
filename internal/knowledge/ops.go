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
	OpLink   OpKind = "link"
)

// Op is a single operation of a write batch. One format for every source —
// topic expansion, AI edits, MCP tools, the owner's manual edits — each
// emitting whichever of the five kinds it needs. New blocks reference each
// other via TempID; real IDs are assigned on apply.
type Op struct {
	Kind    OpKind `json:"op"`
	TempID  string `json:"temp_id,omitzero"`  // temporary ID of a block being added
	Parent  string `json:"parent,omitzero"`   // TempID or ID of an existing block; empty means topic root
	BlockID ID     `json:"block_id,omitzero"` // existing block (edit/move/delete)
	Content string `json:"content,omitzero"`

	// Link fields (OpLink only). From/To are TempIDs or existing IDs.
	From     string   `json:"from,omitzero"`
	To       string   `json:"to,omitzero"`
	Relation Relation `json:"relation,omitzero"`

	// Provenance: why the AI makes this change and where it came from.
	// The review screen shows both fields.
	Rationale string `json:"rationale,omitzero"`
	Origin    string `json:"origin,omitzero"`
}

// validate checks the op's shape: the kind is known and its required fields
// are present. Existence and cycle checks depend on the base's state and
// belong to prepareOps.
func (op Op) validate() error {
	switch op.Kind {
	case OpAdd:
		if op.TempID == "" {
			return fmt.Errorf("add op with empty temp id: %w", ErrInvalidOps)
		}
	case OpEdit, OpMove, OpDelete:
		if op.BlockID == "" {
			return fmt.Errorf("%s op with empty block id: %w", op.Kind, ErrInvalidOps)
		}
	case OpLink:
		if op.From == "" || op.To == "" {
			return fmt.Errorf("link op with empty from/to: %w", ErrInvalidOps)
		}
	default:
		return fmt.Errorf("unknown op kind %q: %w", op.Kind, ErrInvalidOps)
	}
	return nil
}
