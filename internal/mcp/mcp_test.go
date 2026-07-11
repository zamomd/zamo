package mcp

import (
	"context"
	"encoding/json"
	"errors"
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
	subCalls    int
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
	f.subCalls++
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
	// The queued proposal's identity comes back to the client.
	var out proposeOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.ProposalID != "01PROP" || out.Status != string(knowledge.StatusPending) {
		t.Errorf("output = %+v, want the pending proposal 01PROP", out)
	}
}

func TestProposeStampsOriginServerSide(t *testing.T) {
	// Provenance is the server's to assign: the tool schema has no origin field
	// (a client cannot even supply one), and every forwarded op carries the
	// fixed mcp marker. The client's Rationale — the "why" the reviewer reads —
	// is preserved.
	fake := &fakeBase{subProposal: knowledge.Proposal{ID: "01PROP", Status: knowledge.StatusPending}}
	cs := connect(t, NewServer(fake, fake, fake))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "propose_blocks",
		Arguments: map[string]any{
			"title": "Contexts in Go",
			"ops": []map[string]any{{
				"op":        "add",
				"temp_id":   "t1",
				"content":   "# Contexts in Go",
				"rationale": "the topic lacks a root",
			}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("propose_blocks reported a tool error: %+v", res.Content)
	}
	if len(fake.subOps) != 1 {
		t.Fatalf("ops not forwarded: %+v", fake.subOps)
	}
	if got := fake.subOps[0].Origin; got != "mcp" {
		t.Errorf("Origin = %q, want the server's fixed mcp marker (client cannot forge provenance)", got)
	}
	if got := fake.subOps[0].Rationale; got != "the topic lacks a root" {
		t.Errorf("Rationale = %q, want the client's rationale preserved", got)
	}
}

func TestProposeRejectsForgedOrigin(t *testing.T) {
	// The schema is closed: an op field the client must not set (origin) is
	// refused outright as invalid params, never silently accepted.
	fake := &fakeBase{subProposal: knowledge.Proposal{ID: "01PROP", Status: knowledge.StatusPending}}
	cs := connect(t, NewServer(fake, fake, fake))

	_, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "propose_blocks",
		Arguments: map[string]any{
			"title": "x",
			"ops":   []map[string]any{{"op": "add", "temp_id": "t1", "origin": "gui_edit"}},
		},
	})
	if err == nil {
		t.Fatal("a client-supplied origin must be rejected as invalid params")
	}
	if fake.subOps != nil {
		t.Errorf("no proposal must be submitted on a rejected call, got ops %+v", fake.subOps)
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

func TestToolSurface(t *testing.T) {
	// Exactly four tools, each described; the only write tool is propose_blocks —
	// MCP can never mutate the base directly.
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake))

	got := map[string]bool{}
	for tool, err := range cs.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		got[tool.Name] = true
	}
	want := []string{"propose_blocks", "search", "get_subtree", "topic_map"}
	if len(got) != len(want) {
		t.Errorf("tools = %v, want exactly %v (no extra write tools)", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestReadToolsNeverWrite(t *testing.T) {
	// The read tools must not be able to reach the write path: exercising every
	// one leaves the proposal queue untouched. Guards against a read handler
	// being wired to the wrong capability.
	fake := &fakeBase{
		searchBlocks: []knowledge.Block{{ID: "01A"}},
		subtree:      []knowledge.Block{{ID: "01ROOT"}},
		topic:        []knowledge.Block{{ID: "01T"}},
	}
	cs := connect(t, NewServer(fake, fake, fake))

	callBlocks(t, cs, "search", map[string]any{"query": "x"})
	callBlocks(t, cs, "get_subtree", map[string]any{"root": "01ROOT"})
	callBlocks(t, cs, "topic_map", map[string]any{})

	if fake.subCalls != 0 {
		t.Errorf("read tools submitted %d proposals, want 0", fake.subCalls)
	}
}

func TestToolSurfacesDomainError(t *testing.T) {
	// Every tool must turn a capability failure into a tool error the model can
	// read, not a transport crash.
	boom := errors.New("base unavailable")
	cases := []struct {
		tool string
		fake *fakeBase
		args map[string]any
	}{
		{"search", &fakeBase{searchErr: boom}, map[string]any{"query": "x"}},
		{"get_subtree", &fakeBase{treeErr: boom}, map[string]any{"root": "01A"}},
		{"topic_map", &fakeBase{treeErr: boom}, map[string]any{}},
		{"propose_blocks", &fakeBase{subErr: boom}, map[string]any{
			"title": "t", "ops": []map[string]any{{"op": "add", "temp_id": "t1", "content": "x"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			cs := connect(t, NewServer(tc.fake, tc.fake, tc.fake))
			res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatalf("unexpected transport error: %v", err)
			}
			if !res.IsError {
				t.Errorf("%s must report IsError on a capability failure", tc.tool)
			}
		})
	}
}
