package mcp

import (
	"context"
	"encoding/json"
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

// callBlocks calls a read tool and decodes its structured block list.
func callBlocks(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) blocksOutput {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s reported a tool error: %+v", name, res.Content)
	}
	var out blocksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s output: %v", name, err)
	}
	return out
}

func TestSearchTool(t *testing.T) {
	fake := &fakeBase{searchBlocks: []knowledge.Block{{ID: "01A", Content: "context deadlines"}}}
	cs := connect(t, NewServer(fake, fake, fake))

	out := callBlocks(t, cs, "search", map[string]any{"query": "context"})
	if fake.searchQuery != "context" {
		t.Errorf("query forwarded = %q, want context", fake.searchQuery)
	}
	if len(out.Blocks) != 1 || out.Blocks[0].ID != "01A" || out.Blocks[0].Content != "context deadlines" {
		t.Errorf("search result = %+v, want the block from the base", out.Blocks)
	}
}

func TestGetSubtreeTool(t *testing.T) {
	fake := &fakeBase{subtree: []knowledge.Block{
		{ID: "01ROOT", Content: "Root"},
		{ID: "01CHILD", ParentID: "01ROOT", Content: "Child"},
	}}
	cs := connect(t, NewServer(fake, fake, fake))

	out := callBlocks(t, cs, "get_subtree", map[string]any{"root": "01ROOT"})
	if fake.subtreeRoot != "01ROOT" {
		t.Errorf("root forwarded = %q, want 01ROOT", fake.subtreeRoot)
	}
	if len(out.Blocks) != 2 || out.Blocks[1].Parent != "01ROOT" {
		t.Errorf("subtree result = %+v, want the two blocks with parent linkage", out.Blocks)
	}
}

func TestTopicMapTool(t *testing.T) {
	fake := &fakeBase{topic: []knowledge.Block{{ID: "01T", Content: "# Topic"}}}
	cs := connect(t, NewServer(fake, fake, fake))

	out := callBlocks(t, cs, "topic_map", map[string]any{})
	if len(out.Blocks) != 1 || out.Blocks[0].ID != "01T" {
		t.Errorf("topic_map result = %+v, want the topic block", out.Blocks)
	}
}
