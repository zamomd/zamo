// Package knowledge is the domain layer: blocks, links, proposals
// and the rules around them. Package rule: imports nothing but
// the standard library.
package knowledge

import "time"

// ID identifies a block or a proposal (ULID: sorts by creation time).
// ID generation lives outside the domain and is injected into Service.
type ID string

// Relation is the type of a non-hierarchical link between blocks.
// The hierarchy (contains/part-of) is not stored as links: it lives
// in Block.ParentID (one source of truth per fact).
type Relation string

// The two non-hierarchical relations.
const (
	RelationExample Relation = "example" // code example ↔ theory
	RelationRelated Relation = "related" // loosely related (default type)
)

// Metadata holds extensible block metadata: authorship (human/AI),
// provenance, model, etc. Stored in the database as JSON.
type Metadata map[string]any

// MetaKeyAuthor is the Metadata key holding an Author.
const MetaKeyAuthor = "author"

// Author names who wrote a block's current content. It tracks the last writer,
// not the creator: whoever replaces the content takes authorship of it.
type Author string

// The authors a block's content can be attributed to.
const (
	AuthorHuman Author = "human" // written by the owner, by hand
	AuthorAI    Author = "ai"    // written by the AI, via an accepted proposal
)

// Block is the unit of knowledge: a short markdown fragment in a topic
// tree. Portability rule: Content is pure markdown; all structure lives
// in fields, never as service markup inside the text.
type Block struct {
	ID        ID
	ParentID  ID // empty means the block is a topic root
	Position  int
	Content   string
	Meta      Metadata
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsTopic reports whether the block is a topic root.
func (b Block) IsTopic() bool { return b.ParentID == "" }

// Edge is a typed link between two blocks from different branches of the tree.
type Edge struct {
	From     ID
	To       ID
	Relation Relation
}
