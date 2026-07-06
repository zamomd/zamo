// Package mcp is an entry point: the MCP server (tools map, search,
// get_subtree, propose_blocks).
package mcp

import (
	"context"

	"github.com/cominddev/comind/internal/knowledge"
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
	SubmitProposal(ctx context.Context, source knowledge.Source, title string, ops []knowledge.Op) (knowledge.Proposal, error)
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

// Run serves MCP over stdio until the context is cancelled.
// The implementation will sit on top of the official Go SDK for MCP.
func (s *Server) Run(ctx context.Context) error {
	return knowledge.ErrNotImplemented
}
