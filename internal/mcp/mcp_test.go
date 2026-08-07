package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cominddev/comind/internal/knowledge"
)

type fakeBase struct {
	searchQuery  string
	searchBlocks []knowledge.Block
	searchErr    error

	subtreeRoot knowledge.ID
	subtree     []knowledge.Block
	topic       []knowledge.Block
	treeErr     error

	backlinksID     knowledge.ID
	backlinksResult []knowledge.Backlink
	backlinksErr    error

	writeMeta    knowledge.WriteMeta
	writeOps     []knowledge.Op
	writeCalls   int
	writeApplied knowledge.Applied
	writeErr     error
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

func (f *fakeBase) Backlinks(_ context.Context, id knowledge.ID) ([]knowledge.Backlink, error) {
	f.backlinksID = id
	return f.backlinksResult, f.backlinksErr
}

func (f *fakeBase) ApplyOps(
	_ context.Context, meta knowledge.WriteMeta, ops []knowledge.Op,
) (knowledge.Applied, error) {
	f.writeMeta, f.writeOps = meta, ops
	f.writeCalls++
	return f.writeApplied, f.writeErr
}

const testVersion = "v-test"

func connect(t *testing.T, srv *Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx := t.Context()
	st, ct := mcpsdk.NewInMemoryTransports()
	ss, err := srv.mcpServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestHandshakeReportsInjectedVersion(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	got := cs.InitializeResult().ServerInfo
	if got.Name != serverName || got.Version != testVersion {
		t.Errorf("handshake identity = %s %s, want %s %s", got.Name, got.Version, serverName, testVersion)
	}
}

func TestWriteBlocksAppliesImmediately(t *testing.T) {
	fake := &fakeBase{writeApplied: knowledge.Applied{
		GroupID: "01GRP",
		IDs:     map[string]knowledge.ID{"t1": "01ROOT", "t2": "01CHILD"},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
		Arguments: map[string]any{
			"title": "Contexts in Go",
			"ops": []map[string]any{
				{"op": "add", "temp_id": "t1", "content": "# Contexts in Go"},
				{"op": "add", "temp_id": "t2", "parent": "t1", "content": "## Cancellation"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("write_blocks reported a tool error: %+v", res.Content)
	}
	if fake.writeCalls != 1 {
		t.Fatalf("ApplyOps called %d times, want 1", fake.writeCalls)
	}
	if fake.writeMeta.Author != knowledge.AuthorAI {
		t.Errorf("Author = %q, want ai", fake.writeMeta.Author)
	}
	if fake.writeMeta.Source != knowledge.SourceMCP {
		t.Errorf("Source = %q, want mcp", fake.writeMeta.Source)
	}
	if fake.writeMeta.Title != "Contexts in Go" {
		t.Errorf("Title = %q, want the given title", fake.writeMeta.Title)
	}
	if len(fake.writeOps) != 2 || fake.writeOps[0].Kind != knowledge.OpAdd || fake.writeOps[0].TempID != "t1" {
		t.Errorf("ops not forwarded intact: %+v", fake.writeOps)
	}
	for _, op := range fake.writeOps {
		if op.Origin != "mcp" {
			t.Errorf("Origin = %q, want mcp on every op", op.Origin)
		}
	}
	var out writeOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	want := writeOutput{GroupID: "01GRP", IDs: map[string]string{"t1": "01ROOT", "t2": "01CHILD"}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("output = %+v, want %+v", out, want)
	}
}

func TestWriteStampsOriginServerSide(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
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
		t.Fatalf("write_blocks reported a tool error: %+v", res.Content)
	}
	if len(fake.writeOps) != 1 {
		t.Fatalf("ops not forwarded: %+v", fake.writeOps)
	}
	if got := fake.writeOps[0].Origin; got != "mcp" {
		t.Errorf("Origin = %q, want the server's fixed mcp marker (client cannot forge provenance)", got)
	}
	if got := fake.writeOps[0].Rationale; got != "the topic lacks a root" {
		t.Errorf("Rationale = %q, want the client's rationale preserved", got)
	}
}

func TestWriteRejectsForgedOrigin(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
		Arguments: map[string]any{
			"title": "x",
			"ops":   []map[string]any{{"op": "add", "temp_id": "t1", "origin": "gui_edit"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("a client-supplied origin must be rejected as a tool error")
	}
	if fake.writeOps != nil {
		t.Errorf("no write must be applied on a rejected call, got ops %+v", fake.writeOps)
	}
}

func TestWriteBlocksForwardsAModeOnARootAdd(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
		Arguments: map[string]any{
			"title": "Contexts in Go",
			"ops": []map[string]any{
				{"op": "add", "temp_id": "t1", "content": "# Contexts in Go", "mode": "doc"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("write_blocks reported a tool error: %+v", res.Content)
	}
	if len(fake.writeOps) != 1 {
		t.Fatalf("ops not forwarded: %+v", fake.writeOps)
	}
	if got := fake.writeOps[0].Mode; got != knowledge.ModeDoc {
		t.Errorf("Mode = %q, want %q", got, knowledge.ModeDoc)
	}
}

func TestWriteBlocksExplainsAChildUnderADocument(t *testing.T) {
	fake := &fakeBase{writeErr: fmt.Errorf("block c1: %w", knowledge.ErrDocumentHasChildren)}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
		Arguments: map[string]any{
			"title": "fill the doc",
			"ops": []map[string]any{
				{"op": "add", "temp_id": "c1", "parent": "01ROOT", "content": "child"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("write_blocks accepted a child under a document")
	}
	text := contentText(t, res)
	if !strings.Contains(text, "edit its root content instead of adding children") {
		t.Errorf("error text = %q, want it to name the write that would work", text)
	}
}

func TestGetSubtreeReportsTheMode(t *testing.T) {
	fake := &fakeBase{subtree: []knowledge.Block{
		{ID: "01ROOT", Content: "# Doc", Mode: knowledge.ModeDoc},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "get_subtree",
		Arguments: map[string]any{"root": "01ROOT"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_subtree reported a tool error: %+v", res.Content)
	}
	var out blocksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	want := blocksOutput{Blocks: []blockView{{ID: "01ROOT", Content: "# Doc", Mode: "doc"}}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("output = %+v, want %+v", out, want)
	}
}

func TestGetSubtreeLeavesAChildWithoutAMode(t *testing.T) {
	fake := &fakeBase{subtree: []knowledge.Block{
		{ID: "01ROOT", Content: "Topic"},
		{ID: "01KID", ParentID: "01ROOT", Content: "child"},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "get_subtree",
		Arguments: map[string]any{"root": "01ROOT"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if strings.Count(string(raw), `"mode"`) != 1 {
		t.Errorf("result = %s, want the mode reported for the root alone", raw)
	}
}

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

// contentText joins a tool result's text content, which is where a refused
// call's explanation lands.
func contentText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestSearchTool(t *testing.T) {
	fake := &fakeBase{searchBlocks: []knowledge.Block{{ID: "01A", Content: "context deadlines"}}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

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
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

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
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	out := callBlocks(t, cs, "topic_map", map[string]any{})
	if len(out.Blocks) != 1 || out.Blocks[0].ID != "01T" {
		t.Errorf("topic_map result = %+v, want the topic block", out.Blocks)
	}
}

func TestBacklinksTool(t *testing.T) {
	srcContent := "Go uses ((01JT4X9K2M8P0QRSTVWXYZ2340|contexts)) for cancellation."
	fake := &fakeBase{backlinksResult: []knowledge.Backlink{
		{
			Block: knowledge.Block{
				ID: "01JT4X9K2M8P0QRSTVWXYZ2345", ParentID: "01JT4X9K2M8P0QRSTVWXYZ2346", Content: srcContent,
			},
			TopicID:    "01JT4X9K2M8P0QRSTVWXYZ2346",
			TopicTitle: "Go",
		},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "get_backlinks",
		Arguments: map[string]any{"id": "01JT4X9K2M8P0QRSTVWXYZ2340"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_backlinks reported a tool error: %+v", res.Content)
	}
	if fake.backlinksID != "01JT4X9K2M8P0QRSTVWXYZ2340" {
		t.Errorf("id forwarded = %q, want the requested id", fake.backlinksID)
	}
	var out backlinksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode get_backlinks output: %v", err)
	}
	if len(out.Backlinks) != 1 {
		t.Fatalf("backlinks = %+v, want exactly one", out.Backlinks)
	}
	got := out.Backlinks[0]
	if got.ID != "01JT4X9K2M8P0QRSTVWXYZ2345" {
		t.Errorf("referencing block id = %q, want 01JT4X9K2M8P0QRSTVWXYZ2345", got.ID)
	}
	if !strings.Contains(got.Content, "contexts") {
		t.Errorf("content = %q, want the referencing block's own content", got.Content)
	}
	if got.TopicID != "01JT4X9K2M8P0QRSTVWXYZ2346" || got.TopicTitle != "Go" {
		t.Errorf("topic = %+v, want the topic the referencing block lives under", got)
	}
}

func TestToolSurface(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

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
	want := []string{"write_blocks", "search", "get_subtree", "topic_map", "get_backlinks"}
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
	fake := &fakeBase{
		searchBlocks: []knowledge.Block{{ID: "01A"}},
		subtree:      []knowledge.Block{{ID: "01ROOT"}},
		topic:        []knowledge.Block{{ID: "01T"}},
	}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	callBlocks(t, cs, "search", map[string]any{"query": "x"})
	callBlocks(t, cs, "get_subtree", map[string]any{"root": "01ROOT"})
	callBlocks(t, cs, "topic_map", map[string]any{})
	if _, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_backlinks", Arguments: map[string]any{"id": "01T"},
	}); err != nil {
		t.Fatalf("CallTool get_backlinks: %v", err)
	}

	if fake.writeCalls != 0 {
		t.Errorf("read tools called ApplyOps %d times, want 0", fake.writeCalls)
	}
}

func TestToolSurfacesDomainError(t *testing.T) {
	boom := errors.New("base unavailable")
	cases := []struct {
		tool string
		fake *fakeBase
		args map[string]any
	}{
		{"search", &fakeBase{searchErr: boom}, map[string]any{"query": "x"}},
		{"get_subtree", &fakeBase{treeErr: boom}, map[string]any{"root": "01A"}},
		{"topic_map", &fakeBase{treeErr: boom}, map[string]any{}},
		{"get_backlinks", &fakeBase{backlinksErr: boom}, map[string]any{"id": "01A"}},
		{"write_blocks", &fakeBase{writeErr: boom}, map[string]any{
			"title": "t", "ops": []map[string]any{{"op": "add", "temp_id": "t1", "content": "x"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			cs := connect(t, NewServer(tc.fake, tc.fake, tc.fake, testVersion))
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
