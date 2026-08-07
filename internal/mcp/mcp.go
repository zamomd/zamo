// Package mcp is an entry point: the MCP server. Its tools are topic_map,
// search, get_subtree, get_backlinks and write_blocks.
package mcp

import (
	"context"
	"errors"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cominddev/comind/internal/knowledge"
)

// serverName identifies this server in the MCP handshake.
const serverName = "comind"

// Searcher runs full-text search over the base.
type Searcher interface {
	Search(ctx context.Context, query string) ([]knowledge.SearchHit, error)
}

// TreeReader serves the topic map (base overview), subtrees, and backlinks.
type TreeReader interface {
	TopicMap(ctx context.Context) ([]knowledge.Block, error)
	GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error)
	Backlinks(ctx context.Context, id knowledge.ID) ([]knowledge.Backlink, error)
}

// Writer applies a write batch directly — the only write path over MCP.
// Changes land immediately, flagged for the owner's retroactive review.
type Writer interface {
	ApplyOps(ctx context.Context, meta knowledge.WriteMeta, ops []knowledge.Op) (knowledge.Applied, error)
}

// Server exposes the base over MCP: read tools plus direct writes.
type Server struct {
	search  Searcher
	tree    TreeReader
	writer  Writer
	version string
}

// NewServer assembles the server from its three narrow capabilities
// (in production all three are the same knowledge.Service) and the build
// version to report in the MCP handshake.
func NewServer(search Searcher, tree TreeReader, writer Writer, version string) *Server {
	return &Server{search: search, tree: tree, writer: writer, version: version}
}

// Run serves MCP over stdio until the client disconnects or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer().Run(ctx, &mcpsdk.StdioTransport{})
}

// mcpServer builds the SDK server with the tools bound to this server's
// capabilities. Kept separate from Run so tests can drive it over an in-memory
// transport instead of stdio.
func (s *Server) mcpServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: s.version}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "write_blocks",
		Description: "Apply block operations directly. Changes are live " +
			"immediately but every touched block is flagged for the human's " +
			"review and can be rolled back — write carefully and set " +
			"rationale on every op.\n" +
			"A topic is either an outline (a block tree) or a document (one " +
			"root holding the whole markdown page, no children). An add or " +
			"edit of a root may carry \"mode\": \"outline\" | \"doc\"; omit " +
			"it to leave the shape alone.",
	}, s.writeBlocks)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search",
		Description: "Full-text search over block content; returns matching blocks with a snippet around each match.",
	}, s.searchBlocks)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_subtree",
		Description: "Fetch a block and all of its descendants, in tree order.",
	}, s.getSubtree)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "topic_map",
		Description: "The map of the base: root topics and their first level, for orientation.",
	}, s.topicMap)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_backlinks",
		Description: "Every block referencing this block or topic, with the topic each one lives under.",
	}, s.getBacklinks)
	return srv
}

// blockView is the block shape exposed over MCP: the fields a reader needs,
// without internal timestamps or metadata.
type blockView struct {
	ID      string `json:"id"`
	Parent  string `json:"parent,omitempty"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"`
}

// blocksOutput is the return shape shared by the read tools.
type blocksOutput struct {
	Blocks []blockView `json:"blocks"`
}

// toBlockView maps one domain block onto its wire shape. Mode is only
// meaningful on a topic root; storage stamps every child ModeOutline
// regardless, so gating on IsTopic (not just omitempty) is what actually
// keeps it off children on the wire. A root's mode defaults to outline, the
// same default CreateTopic resolves an unset mode to, so the field is never
// blank on a root.
func toBlockView(b knowledge.Block) blockView {
	v := blockView{ID: string(b.ID), Parent: string(b.ParentID), Content: b.Content}
	if b.IsTopic() {
		mode := b.Mode
		if mode == "" {
			mode = knowledge.ModeOutline
		}
		v.Mode = string(mode)
	}
	return v
}

func toBlocksOutput(blocks []knowledge.Block) blocksOutput {
	views := make([]blockView, len(blocks))
	for i, b := range blocks {
		views[i] = toBlockView(b)
	}
	return blocksOutput{Blocks: views}
}

type searchInput struct {
	Query string `json:"query" jsonschema:"the full-text query"`
}

// hitView is one search match on the wire: the block plus an excerpt around
// the matching words, since a block's content alone (a whole document, for
// one) doesn't say where in it the match landed.
type hitView struct {
	blockView
	Snippet string `json:"snippet"`
}

type searchOutput struct {
	Hits []hitView `json:"hits"`
}

func (s *Server) searchBlocks(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput,
) (*mcpsdk.CallToolResult, searchOutput, error) {
	hits, err := s.search.Search(ctx, in.Query)
	if err != nil {
		return nil, searchOutput{}, err
	}
	views := make([]hitView, len(hits))
	for i, h := range hits {
		views[i] = hitView{blockView: toBlockView(h.Block), Snippet: h.Snippet}
	}
	return nil, searchOutput{Hits: views}, nil
}

type subtreeInput struct {
	Root string `json:"root" jsonschema:"the id of the block whose subtree to fetch"`
}

func (s *Server) getSubtree(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in subtreeInput,
) (*mcpsdk.CallToolResult, blocksOutput, error) {
	blocks, err := s.tree.GetSubtree(ctx, knowledge.ID(in.Root))
	if err != nil {
		return nil, blocksOutput{}, err
	}
	return nil, toBlocksOutput(blocks), nil
}

// topicMap takes no arguments; the empty struct yields an empty input schema.
func (s *Server) topicMap(
	ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{},
) (*mcpsdk.CallToolResult, blocksOutput, error) {
	blocks, err := s.tree.TopicMap(ctx)
	if err != nil {
		return nil, blocksOutput{}, err
	}
	return nil, toBlocksOutput(blocks), nil
}

type backlinksInput struct {
	ID string `json:"id" jsonschema:"the id of the block or topic to find references to"`
}

// backlinkView is the shape of one incoming reference exposed over MCP: the
// referencing block plus the topic it lives under, which is the context a
// caller needs to place it without a second lookup.
type backlinkView struct {
	ID         string `json:"id"`
	Parent     string `json:"parent,omitempty"`
	Content    string `json:"content"`
	TopicID    string `json:"topic_id"`
	TopicTitle string `json:"topic_title"`
}

type backlinksOutput struct {
	Backlinks []backlinkView `json:"backlinks"`
}

func (s *Server) getBacklinks(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in backlinksInput,
) (*mcpsdk.CallToolResult, backlinksOutput, error) {
	links, err := s.tree.Backlinks(ctx, knowledge.ID(in.ID))
	if err != nil {
		return nil, backlinksOutput{}, err
	}
	views := make([]backlinkView, len(links))
	for i, l := range links {
		views[i] = backlinkView{
			ID:         string(l.Block.ID),
			Parent:     string(l.Block.ParentID),
			Content:    l.Block.Content,
			TopicID:    string(l.TopicID),
			TopicTitle: l.TopicTitle,
		}
	}
	return nil, backlinksOutput{Backlinks: views}, nil
}

// originMCP marks operations that arrived over MCP. The server stamps it
// on every op so an MCP client cannot claim a more trusted provenance.
const originMCP = "mcp"

// opInput is the MCP-facing shape of one operation. It deliberately mirrors
// the domain op but omits Origin: provenance is the server's to assign, never
// the client's. Keeping it separate also decouples the tool's wire schema from
// the domain type, so domain-only fields never leak into the public contract.
type opInput struct {
	Kind      string `json:"op" jsonschema:"the operation: add/edit/move/delete"`
	TempID    string `json:"temp_id,omitzero" jsonschema:"temporary id of a block being added"`
	Parent    string `json:"parent,omitzero" jsonschema:"temp_id or existing id; empty means topic root"`
	BlockID   string `json:"block_id,omitzero" jsonschema:"existing block to edit/move/delete"`
	Content   string `json:"content,omitzero" jsonschema:"the block content"`
	Mode      string `json:"mode,omitzero" jsonschema:"outline or doc; only on a topic root, empty leaves the shape alone"`
	Rationale string `json:"rationale,omitzero" jsonschema:"why the AI makes this change"`
}

// toOp maps the MCP input onto a domain op, stamping the fixed mcp origin.
func (in opInput) toOp() knowledge.Op {
	return knowledge.Op{
		Kind:      knowledge.OpKind(in.Kind),
		TempID:    in.TempID,
		Parent:    in.Parent,
		BlockID:   knowledge.ID(in.BlockID),
		Content:   in.Content,
		Mode:      knowledge.Mode(in.Mode),
		Rationale: in.Rationale,
		Origin:    originMCP,
	}
}

// writeOutput reports what the batch did: the group tying its revisions
// together and the real id assigned to every temp id.
type writeOutput struct {
	GroupID string            `json:"group_id"`
	IDs     map[string]string `json:"ids,omitzero"`
}

type writeInput struct {
	Title string    `json:"title" jsonschema:"a short description of this change batch, shown to the reviewer"`
	Ops   []opInput `json:"ops" jsonschema:"the block operations to apply (add/edit/move/delete)"`
}

// explain turns a domain error into something an AI client can act on. The
// document refusal names the write that would have worked, because the model
// has no other way to learn the topic's shape mid-batch.
func explain(err error) string {
	if errors.Is(err, knowledge.ErrDocumentHasChildren) {
		return err.Error() + "; edit its root content instead of adding children"
	}
	return err.Error()
}

// writeBlocks applies the operations as an mcp-sourced, AI-authored batch. The
// source and author are fixed here: an MCP client never chooses its own
// provenance.
func (s *Server) writeBlocks(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in writeInput,
) (*mcpsdk.CallToolResult, writeOutput, error) {
	ops := make([]knowledge.Op, len(in.Ops))
	for i, o := range in.Ops {
		ops[i] = o.toOp()
	}
	applied, err := s.writer.ApplyOps(ctx,
		knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: in.Title}, ops)
	if err != nil {
		// A non-nil Go error, not a hand-built result: the SDK's own error
		// path (SetError) is what skips marshaling writeOutput{} into
		// StructuredContent. Returning a result here instead, with a nil
		// error, defeats that and leaves a spurious {"group_id":""} riding
		// alongside IsError on every failure.
		return nil, writeOutput{}, errors.New(explain(err))
	}
	ids := make(map[string]string, len(applied.IDs))
	for temp, id := range applied.IDs {
		ids[temp] = string(id)
	}
	return nil, writeOutput{GroupID: string(applied.GroupID), IDs: ids}, nil
}
