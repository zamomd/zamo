// Package mcp is an entry point: the MCP server. It serves the tools of
// internal/tools — topic_map, search, get_subtree, get_backlinks and
// write_blocks — to an external agent.
package mcp

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/tools"
)

// serverName identifies this server in the MCP handshake.
const serverName = "zamo"

// originMCP marks operations that arrived over MCP. The server stamps it on
// every op so an MCP client cannot claim a more trusted provenance.
const originMCP = "mcp"

// Server exposes the base over MCP: read tools plus direct writes.
type Server struct {
	tools   []tools.Tool
	version string
}

// NewServer assembles the server from read and write access to the base (in
// production both are the same knowledge.Service) and the build version to
// report in the MCP handshake. Writes land immediately, AI-authored and
// flagged for the owner's review.
func NewServer(base tools.Reader, writer tools.Writer, version string) *Server {
	stamp := tools.Stamp{
		Origin: originMCP,
		Meta: func(title string) knowledge.WriteMeta {
			return knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: title}
		},
	}
	return &Server{
		tools:   append(tools.Read(base, tools.Limits{}), tools.Write(writer, stamp)),
		version: version,
	}
}

// Run serves MCP over stdio until the client disconnects or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer().Run(ctx, &mcpsdk.StdioTransport{})
}

// mcpServer builds the SDK server. Kept separate from Run so tests can drive
// it over an in-memory transport instead of stdio.
func (s *Server) mcpServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: s.version}, nil)
	for _, tool := range s.tools {
		srv.AddTool(&mcpsdk.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: json.RawMessage(tool.Schema),
		}, handler(tool))
	}
	return srv
}

// handler adapts a tool to the SDK. A failure is reported as a tool error
// carrying only its text, so a client never mistakes it for a result.
func handler(tool tools.Tool) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		out, err := tool.Call(ctx, jsontext.Value(req.Params.Arguments))
		if err != nil {
			return failed(err), nil
		}
		raw, err := jsonv2.Marshal(out)
		if err != nil {
			return failed(err), nil
		}
		return &mcpsdk.CallToolResult{
			Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}},
			StructuredContent: json.RawMessage(raw),
		}, nil
	}
}

func failed(err error) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: err.Error()}},
	}
}
