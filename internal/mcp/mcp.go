// Package mcp is an entry point: the MCP server (tools map, search,
// get_subtree, propose_blocks).
package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cominddev/comind/internal/knowledge"
)

// serverName and serverVersion identify this server in the MCP handshake.
const (
	serverName    = "comind"
	serverVersion = "v0.1.0"
)

// Searcher runs full-text search over the base.
type Searcher interface {
	Search(ctx context.Context, query string) ([]knowledge.Block, error)
}

// TreeReader serves the topic map (base overview) and subtrees.
type TreeReader interface {
	TopicMap(ctx context.Context) ([]knowledge.Block, error)
	GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error)
}

// ProposalSubmitter puts a proposal into the review queue — the only
// write path available over MCP.
type ProposalSubmitter interface {
	SubmitProposal(
		ctx context.Context,
		source knowledge.Source,
		title string,
		ops []knowledge.Op,
	) (knowledge.Proposal, error)
}

// Server exposes the base over MCP: read tools plus proposal submission.
type Server struct {
	search   Searcher
	tree     TreeReader
	proposal ProposalSubmitter
}

// NewServer assembles the server from its three narrow capabilities
// (in production all three are the same knowledge.Service).
func NewServer(search Searcher, tree TreeReader, proposal ProposalSubmitter) *Server {
	return &Server{search: search, tree: tree, proposal: proposal}
}

// Run serves MCP over stdio until the client disconnects or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer().Run(ctx, &mcpsdk.StdioTransport{})
}

// mcpServer builds the SDK server with the tools bound to this server's
// capabilities. Kept separate from Run so tests can drive it over an in-memory
// transport instead of stdio.
func (s *Server) mcpServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "propose_blocks",
		Description: "Submit block operations as a proposal into the review queue — the only " +
			"write path; nothing reaches the base until the human approves it.",
	}, s.proposeBlocks)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search",
		Description: "Full-text search over block content; returns matching blocks.",
	}, s.searchBlocks)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_subtree",
		Description: "Fetch a block and all of its descendants, in tree order.",
	}, s.getSubtree)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "topic_map",
		Description: "The map of the base: root topics and their first level, for orientation.",
	}, s.topicMap)
	return srv
}

// blockView is the block shape exposed over MCP: the fields a reader needs,
// without internal timestamps or metadata.
type blockView struct {
	ID      string `json:"id"`
	Parent  string `json:"parent,omitempty"`
	Content string `json:"content"`
}

// blocksOutput is the return shape shared by the read tools.
type blocksOutput struct {
	Blocks []blockView `json:"blocks"`
}

func toBlocksOutput(blocks []knowledge.Block) blocksOutput {
	views := make([]blockView, len(blocks))
	for i, b := range blocks {
		views[i] = blockView{ID: string(b.ID), Parent: string(b.ParentID), Content: b.Content}
	}
	return blocksOutput{Blocks: views}
}

type searchInput struct {
	Query string `json:"query" jsonschema:"the full-text query"`
}

func (s *Server) searchBlocks(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput,
) (*mcpsdk.CallToolResult, blocksOutput, error) {
	blocks, err := s.search.Search(ctx, in.Query)
	if err != nil {
		return nil, blocksOutput{}, err
	}
	return nil, toBlocksOutput(blocks), nil
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

// proposeInput is the argument schema of the propose_blocks tool.
type proposeInput struct {
	Title string         `json:"title" jsonschema:"a short title describing the proposal"`
	Ops   []knowledge.Op `json:"ops" jsonschema:"the block operations to propose (add/edit/move/delete/link)"`
}

// proposeOutput is what propose_blocks returns: the queued proposal's identity.
type proposeOutput struct {
	ProposalID string `json:"proposal_id"`
	Status     string `json:"status"`
}

// proposeBlocks queues the operations as an mcp-sourced proposal. The source is
// fixed here: an MCP client never chooses its own provenance.
func (s *Server) proposeBlocks(
	ctx context.Context, _ *mcpsdk.CallToolRequest, in proposeInput,
) (*mcpsdk.CallToolResult, proposeOutput, error) {
	p, err := s.proposal.SubmitProposal(ctx, knowledge.SourceMCP, in.Title, in.Ops)
	if err != nil {
		return nil, proposeOutput{}, err
	}
	return nil, proposeOutput{ProposalID: string(p.ID), Status: string(p.Status)}, nil
}
