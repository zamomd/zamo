// Package vaultimport is the feature module that brings an Obsidian vault or
// a Logseq graph into the base: a format adapter parses the directory into a
// neutral model, the planner turns the model into one write batch, and the
// user sees the plan before anything is written.
//
// Adapter is the contract a format implementation targets. An adapter reads
// files, names pages, parses their structure, and rewrites link syntax into
// Comind's forms; it resolves nothing, because it knows neither the base's
// topics nor the ids the import will receive. That is the planner's job.
package vaultimport

import (
	"context"

	"github.com/cominddev/comind/internal/knowledge"
)

// Adapter parses one vault format into the neutral model.
type Adapter interface {
	// Format names the format for reports and the batch title: "Obsidian
	// vault", "Logseq graph".
	Format() string
	// Detect reports whether dir holds a vault of this format.
	Detect(dir string) bool
	// Parse reads every page of the vault under dir.
	Parse(ctx context.Context, dir string) (Vault, error)
}

// Vault is a parsed vault: its pages, in path order.
type Vault struct {
	Format string
	Pages  []Page
}

// Page is one page of the vault, in the shape its format gives it: a
// document holds its whole markdown in Doc, an outline holds its block tree
// in Blocks.
type Page struct {
	Name     string         // page name, which becomes the topic name
	Path     string         // slash-separated path relative to the vault
	Mode     knowledge.Mode // ModeDoc or ModeOutline
	Doc      string         // Mode == ModeDoc: the whole converted markdown
	Root     string         // Mode == ModeOutline: the root block's content when it is more than Name
	Blocks   []Node         // Mode == ModeOutline: the top-level blocks
	Warnings []Warning
}

// Node is one block of an outline page.
type Node struct {
	Content  string
	Children []Node
	RefID    string // the format's own block id, for block-reference resolution
}

// Warning reports content the import converted lossily or could not convert.
type Warning struct {
	Page   string `json:"page"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Conflict reports a page the import leaves out and why.
type Conflict struct {
	Page   string `json:"page"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// The warning kinds.
const (
	WarnFrontmatter        = "frontmatter"          // YAML frontmatter kept as text
	WarnAlias              = "alias"                // [[Page|alias]] lost its alias
	WarnAnchor             = "anchor"               // [[Page#heading]] lost its anchor
	WarnEmbed              = "embed"                // ![[file]] kept as a plain reference
	WarnPath               = "path"                 // [[folder/Page]] lost its folder path
	WarnProperties         = "properties"           // key:: value properties kept as text
	WarnProseBeforeBullets = "prose-before-bullets" // text above the first bullet became a block
	WarnBrokenLink         = "broken-link"          // [[Page]] names no page or topic
	WarnBrokenBlockRef     = "broken-block-ref"     // ((id)) names no imported block
)

// The conflict kinds.
const (
	ConflictUnnamed   = "unnamed"   // the name cannot be a topic name
	ConflictExists    = "exists"    // a topic of that name is already in the base
	ConflictDuplicate = "duplicate" // another page of the vault claimed the name first
)
