package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cominddev/comind/internal/knowledge"
)

// fakeBase satisfies Searcher, TreeReader and ProposalSubmitter — the three
// narrow capabilities the server needs — and records what it was asked.
type fakeBase struct {
	searchQuery  string
	searchBlocks []knowledge.Block
	searchErr    error

	subtreeRoot knowledge.ID
	subtree     []knowledge.Block
	topic       []knowledge.Block
	treeErr     error

	subSource   knowledge.Source
	subTitle    string
	subOps      []knowledge.Op
	subProposal knowledge.Proposal
	subErr      error
}

func (f *fakeBase) Search(_ context.Context, q string) ([]knowledge.Block, error) {
	f.searchQuery = q
	return f.searchBlocks, f.searchErr
}

func (f *fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) {
	return f.topic, f.treeErr
}

func (f *fakeBase) GetSubtree(_ context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	f.subtreeRoot = root
	return f.subtree, f.treeErr
}

func (f *fakeBase) SubmitProposal(
	_ context.Context, source knowledge.Source, title string, ops []knowledge.Op,
) (knowledge.Proposal, error) {
	f.subSource, f.subTitle, f.subOps = source, title, ops
	return f.subProposal, f.subErr
}

// connect wires an in-memory client to the server under test.
func connect(t *testing.T, srv *Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx := t.Context()
	st, ct := mcpsdk.NewInMemoryTransports()
	if _, err := srv.mcpServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestProposeBlocksSubmitsAsMCP(t *testing.T) {
	fake := &fakeBase{subProposal: knowledge.Proposal{ID: "01PROP", Status: knowledge.StatusPending}}
	cs := connect(t, NewServer(fake, fake, fake))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "propose_blocks",
		Arguments: map[string]any{
			"title": "Contexts in Go",
			"ops": []map[string]any{
				{"op": "add", "temp_id": "t1", "content": "# Contexts in Go"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("propose_blocks reported a tool error: %+v", res.Content)
	}
	// The write path submits with a fixed mcp source — MCP never picks the source.
	if fake.subSource != knowledge.SourceMCP {
		t.Errorf("source = %q, want mcp", fake.subSource)
	}
	if fake.subTitle != "Contexts in Go" {
		t.Errorf("title = %q, want the given title", fake.subTitle)
	}
	if len(fake.subOps) != 1 || fake.subOps[0].Kind != knowledge.OpAdd || fake.subOps[0].TempID != "t1" {
		t.Errorf("ops not forwarded intact: %+v", fake.subOps)
	}
}
