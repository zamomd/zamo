// Package knowledge is the domain layer: blocks, references, revisions
// and the rules around them. Package rule: imports nothing but
// the standard library.
package knowledge

import "time"

// ID identifies a block, a revision, or a write-batch group (ULID: sorts
// by creation time).
// ID generation lives outside the domain and is injected into Service.
type ID string

// Metadata holds extensible block metadata: authorship (human/AI),
// provenance, model, etc. Stored in the database as JSON.
type Metadata map[string]any

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

	// Unreviewed reports that the block has revisions awaiting review.
	// Derived by storage on read; never written by callers.
	Unreviewed bool
}

// IsTopic reports whether the block is a topic root.
func (b Block) IsTopic() bool { return b.ParentID == "" }

// Backlink is one incoming reference, hydrated for display: the block that
// carries the reference plus the topic it lives under, which is the context a
// reader needs to place it without navigating. The source block's own parent
// is reachable as Block.ParentID.
type Backlink struct {
	Block      Block
	TopicID    ID
	TopicTitle string
}
