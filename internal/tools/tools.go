// Package tools defines the operations an AI is given over the base: what
// each is called, what it takes, and what it does. The MCP server and the
// agent loop both adapt this one set, so an external agent and the built-in
// one read and write through the same definitions.
package tools

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/zamomd/zamo/internal/knowledge"
)

// ErrBadArguments reports arguments that do not fit a tool's schema.
var ErrBadArguments = errors.New("arguments do not fit the tool")

// Tool is one operation offered to a model.
type Tool struct {
	Name        string
	Description string
	// Schema is the JSON Schema of the arguments object.
	Schema jsontext.Value
	// Call runs the tool on raw arguments and returns a value to encode as
	// JSON. Empty arguments mean an empty object.
	Call func(ctx context.Context, args jsontext.Value) (any, error)
}

// Reader is the read access the read tools need.
type Reader interface {
	Search(ctx context.Context, query string) ([]knowledge.SearchHit, error)
	TopicMap(ctx context.Context) ([]knowledge.Block, error)
	GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error)
	Backlinks(ctx context.Context, id knowledge.ID) ([]knowledge.Backlink, error)
}

// Limits bounds what one read returns, for a caller with a context budget.
// A zero field is no limit.
type Limits struct {
	// MaxContent is the most characters of one block's content.
	MaxContent int
	// MaxBlocks is the most blocks, hits or backlinks in one result.
	MaxBlocks int
}

// BlockView is a block as a model sees it: the fields a reader needs, without
// internal timestamps or derived state.
type BlockView struct {
	ID      string `json:"id"`
	Parent  string `json:"parent,omitempty"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"`
}

// BlocksOutput is the result of the tools that return blocks.
type BlocksOutput struct {
	Blocks []BlockView `json:"blocks"`
	// Truncated reports that a limit cut content or dropped blocks.
	Truncated bool `json:"truncated,omitzero"`
}

// HitView is one search match: the block plus an excerpt around the matching
// words, since a block's content alone (a whole document, for one) does not
// say where in it the match landed.
type HitView struct {
	BlockView
	Snippet string `json:"snippet"`
}

// SearchOutput is the result of search.
type SearchOutput struct {
	Hits      []HitView `json:"hits"`
	Truncated bool      `json:"truncated,omitzero"`
}

// BacklinkView is one incoming reference: the referencing block plus the
// topic it lives under, which is the context a caller needs to place it
// without a second lookup.
type BacklinkView struct {
	ID         string `json:"id"`
	Parent     string `json:"parent,omitempty"`
	Content    string `json:"content"`
	TopicID    string `json:"topic_id"`
	TopicTitle string `json:"topic_title"`
}

// BacklinksOutput is the result of get_backlinks.
type BacklinksOutput struct {
	Backlinks []BacklinkView `json:"backlinks"`
	Truncated bool           `json:"truncated,omitzero"`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"the full-text query"`
}

type subtreeInput struct {
	Root string `json:"root" jsonschema:"the id of the block whose subtree to fetch"`
}

type backlinksInput struct {
	ID string `json:"id" jsonschema:"the id of the block or topic to find references to"`
}

// Read returns the tools that only read the base.
func Read(base Reader, lim Limits) []Tool {
	return []Tool{
		define("search",
			"Full-text search over block content; returns matching blocks with a snippet around each match.",
			func(ctx context.Context, in searchInput) (any, error) {
				hits, err := base.Search(ctx, in.Query)
				if err != nil {
					return nil, err
				}
				hits, dropped := lim.capCount(hits)
				out := SearchOutput{Hits: make([]HitView, len(hits)), Truncated: dropped}
				for i, h := range hits {
					view, cut := lim.view(h.Block)
					out.Hits[i] = HitView{BlockView: view, Snippet: h.Snippet}
					out.Truncated = out.Truncated || cut
				}
				return out, nil
			}),
		define("get_subtree",
			"Fetch a block and all of its descendants, in tree order.",
			func(ctx context.Context, in subtreeInput) (any, error) {
				blocks, err := base.GetSubtree(ctx, knowledge.ID(in.Root))
				if err != nil {
					return nil, err
				}
				return lim.blocks(blocks), nil
			}),
		define("topic_map",
			"The map of the base: root topics and their first level, for orientation.",
			func(ctx context.Context, _ struct{}) (any, error) {
				blocks, err := base.TopicMap(ctx)
				if err != nil {
					return nil, err
				}
				return lim.blocks(blocks), nil
			}),
		define("get_backlinks",
			"Every block referencing this block or topic, with the topic each one lives under.",
			func(ctx context.Context, in backlinksInput) (any, error) {
				links, err := base.Backlinks(ctx, knowledge.ID(in.ID))
				if err != nil {
					return nil, err
				}
				links, dropped := lim.capCount(links)
				out := BacklinksOutput{Backlinks: make([]BacklinkView, len(links)), Truncated: dropped}
				for i, l := range links {
					view, cut := lim.view(l.Block)
					out.Backlinks[i] = BacklinkView{
						ID: view.ID, Parent: view.Parent, Content: view.Content,
						TopicID: string(l.TopicID), TopicTitle: l.TopicTitle,
					}
					out.Truncated = out.Truncated || cut
				}
				return out, nil
			}),
	}
}

// define binds a typed handler to a tool: the schema comes from In, and raw
// arguments are decoded into it strictly before the handler runs.
func define[In any](name, description string, handle func(context.Context, In) (any, error)) Tool {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tools: no schema for %s: %v", name, err))
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("tools: encode schema of %s: %v", name, err))
	}
	return Tool{
		Name:        name,
		Description: description,
		Schema:      raw,
		Call: func(ctx context.Context, args jsontext.Value) (any, error) {
			var in In
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in, json.RejectUnknownMembers(true)); err != nil {
					return nil, fmt.Errorf("%w %s: %w", ErrBadArguments, name, err)
				}
			}
			return handle(ctx, in)
		},
	}
}

func (l Limits) blocks(blocks []knowledge.Block) BlocksOutput {
	blocks, dropped := l.capCount(blocks)
	out := BlocksOutput{Blocks: make([]BlockView, len(blocks)), Truncated: dropped}
	for i, b := range blocks {
		view, cut := l.view(b)
		out.Blocks[i] = view
		out.Truncated = out.Truncated || cut
	}
	return out
}

func (l Limits) capCount[T any](items []T) ([]T, bool) {
	if l.MaxBlocks > 0 && len(items) > l.MaxBlocks {
		return items[:l.MaxBlocks], true
	}
	return items, false
}

// view maps a block onto its wire shape, cutting content past the limit and
// saying how much was cut. Mode is only meaningful on a topic root; storage
// stamps every child ModeOutline regardless, so gating on IsTopic is what
// keeps it off children. A root's mode defaults to outline, the same default
// CreateTopic resolves an unset mode to.
func (l Limits) view(b knowledge.Block) (BlockView, bool) {
	v := BlockView{ID: string(b.ID), Parent: string(b.ParentID), Content: b.Content}
	if b.IsTopic() {
		v.Mode = string(cmp.Or(b.Mode, knowledge.ModeOutline))
	}
	if l.MaxContent <= 0 {
		return v, false
	}
	total := utf8.RuneCountInString(b.Content)
	if total <= l.MaxContent {
		return v, false
	}
	kept := 0
	for i := range b.Content {
		if kept == l.MaxContent {
			v.Content = fmt.Sprintf("%s… [%d more characters not shown]", b.Content[:i], total-l.MaxContent)
			break
		}
		kept++
	}
	return v, true
}
