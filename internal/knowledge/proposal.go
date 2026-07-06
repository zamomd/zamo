package knowledge

import "time"

// Source says where a proposal came from. The set is open: the scheduler,
// chat import and other future inputs will add their own values.
type Source string

// The proposal sources of the MVP.
const (
	SourceGUIExpand Source = "gui_expand" // topic expansion from the app
	SourceGUIEdit   Source = "gui_edit"   // AI edit command from the app
	SourceMCP       Source = "mcp"        // proposal from an MCP tool
)

// Status is the state of a proposal in the review queue.
type Status string

// The proposal lifecycle: pending until the user decides.
const (
	StatusPending  Status = "pending"
	StatusApplied  Status = "applied"
	StatusRejected Status = "rejected"
)

// OpKind is the kind of a single operation inside a proposal.
type OpKind string

// The operation kinds of the proposal format.
const (
	OpAdd    OpKind = "add"
	OpEdit   OpKind = "edit"
	OpMove   OpKind = "move"
	OpDelete OpKind = "delete"
	OpLink   OpKind = "link"
)

// Op is a single operation of a proposal. One format for every source:
// topic expansion emits only add; AI edits emit add/edit/move/delete;
// MCP emits add/link. New blocks reference each other via TempID;
// real IDs are assigned on apply.
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

	// Provenance: why the AI proposes this and where it came from.
	// The review screen shows both fields.
	Rationale string `json:"rationale,omitzero"`
	Origin    string `json:"origin,omitzero"`
}

// Proposal is a change proposal: the only way any AI (from any input)
// modifies the base. Only an explicit user action applies it; manual
// human edits bypass the queue.
type Proposal struct {
	ID        ID
	Source    Source
	Status    Status
	Title     string
	Ops       []Op
	CreatedAt time.Time
	DecidedAt time.Time // zero while the proposal is pending
}
