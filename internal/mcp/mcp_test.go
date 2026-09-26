package mcp

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
)

type fakeBase struct {
	searchQuery string
	searchHits  []knowledge.SearchHit
	searchErr   error

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

func (f *fakeBase) Search(_ context.Context, q string) ([]knowledge.SearchHit, error) {
	f.searchQuery = q
	return f.searchHits, f.searchErr
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
	require.NoError(t, err, "server connect")
	t.Cleanup(func() { _ = ss.Close() })
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err, "client connect")
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestHandshakeReportsInjectedVersion(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	got := cs.InitializeResult().ServerInfo
	assert.Equal(t, serverName, got.Name)
	assert.Equal(t, testVersion, got.Version)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "write_blocks reported a tool error: %+v", res.Content)
	require.Equal(t, 1, fake.writeCalls, "ApplyOps calls")
	assert.Equal(t, knowledge.AuthorAI, fake.writeMeta.Author)
	assert.Equal(t, knowledge.SourceMCP, fake.writeMeta.Source)
	assert.Equal(t, "Contexts in Go", fake.writeMeta.Title)
	require.Len(t, fake.writeOps, 2)
	assert.Equal(t, knowledge.OpAdd, fake.writeOps[0].Kind)
	assert.Equal(t, "t1", fake.writeOps[0].TempID)
	for _, op := range fake.writeOps {
		assert.Equal(t, "mcp", op.Origin, "on every op")
	}
	var out writeOutput
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out))
	want := writeOutput{GroupID: "01GRP", IDs: map[string]string{"t1": "01ROOT", "t2": "01CHILD"}}
	assert.Equal(t, want, out)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "write_blocks reported a tool error: %+v", res.Content)
	require.Len(t, fake.writeOps, 1)
	assert.Equal(t, "mcp", fake.writeOps[0].Origin, "client cannot forge provenance")
	assert.Equal(t, "the topic lacks a root", fake.writeOps[0].Rationale)
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
	require.NoError(t, err)
	require.True(t, res.IsError, "a client-supplied origin must be rejected as a tool error")
	assert.Nil(t, fake.writeOps, "no write must be applied on a rejected call")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "write_blocks reported a tool error: %+v", res.Content)
	require.Len(t, fake.writeOps, 1)
	assert.Equal(t, knowledge.ModeDoc, fake.writeOps[0].Mode)
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
	require.NoError(t, err)
	require.True(t, res.IsError, "write_blocks accepted a child under a document")
	assert.Contains(t, contentText(t, res), "edit its root content instead of adding children")
}

func TestWriteBlocksReportsNoStructuredContentOnFailure(t *testing.T) {
	fake := &fakeBase{writeErr: errors.New("base unavailable")}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "write_blocks",
		Arguments: map[string]any{
			"title": "t",
			"ops":   []map[string]any{{"op": "add", "temp_id": "t1", "content": "x"}},
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "write_blocks must report IsError on a capability failure")
	assert.Nil(t, res.StructuredContent,
		"a client that reads it before IsError must not see a fake empty success")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "get_subtree reported a tool error: %+v", res.Content)
	var out blocksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out))
	want := blocksOutput{Blocks: []blockView{{ID: "01ROOT", Content: "# Doc", Mode: "doc"}}}
	assert.Equal(t, want, out)
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
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	assert.Equal(t, 1, strings.Count(string(raw), `"mode"`), "result = %s, want the mode reported for the root alone", raw)
}

func callBlocks(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) blocksOutput {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, name)
	require.False(t, res.IsError, "%s reported a tool error: %+v", name, res.Content)
	var out blocksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out), "decode %s output", name)
	return out
}

func callSearch(t *testing.T, cs *mcpsdk.ClientSession, args map[string]any) searchOutput {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "search", Arguments: args})
	require.NoError(t, err)
	require.False(t, res.IsError, "search reported a tool error: %+v", res.Content)
	var out searchOutput
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out))
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
	fake := &fakeBase{searchHits: []knowledge.SearchHit{
		{Block: knowledge.Block{ID: "01A", Content: "context deadlines"}, Snippet: "…context deadlines…"},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	out := callSearch(t, cs, map[string]any{"query": "context"})
	assert.Equal(t, "context", fake.searchQuery)
	require.Len(t, out.Hits, 1)
	assert.Equal(t, "01A", out.Hits[0].ID)
	assert.Equal(t, "context deadlines", out.Hits[0].Content)
	assert.Equal(t, "…context deadlines…", out.Hits[0].Snippet)
}

func TestGetSubtreeTool(t *testing.T) {
	fake := &fakeBase{subtree: []knowledge.Block{
		{ID: "01ROOT", Content: "Root"},
		{ID: "01CHILD", ParentID: "01ROOT", Content: "Child"},
	}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	out := callBlocks(t, cs, "get_subtree", map[string]any{"root": "01ROOT"})
	assert.Equal(t, knowledge.ID("01ROOT"), fake.subtreeRoot)
	require.Len(t, out.Blocks, 2)
	assert.Equal(t, "01ROOT", out.Blocks[1].Parent)
}

func TestTopicMapTool(t *testing.T) {
	fake := &fakeBase{topic: []knowledge.Block{{ID: "01T", Content: "# Topic"}}}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	out := callBlocks(t, cs, "topic_map", map[string]any{})
	require.Len(t, out.Blocks, 1)
	assert.Equal(t, "01T", out.Blocks[0].ID)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "get_backlinks reported a tool error: %+v", res.Content)
	assert.Equal(t, knowledge.ID("01JT4X9K2M8P0QRSTVWXYZ2340"), fake.backlinksID)
	var out backlinksOutput
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Len(t, out.Backlinks, 1)
	got := out.Backlinks[0]
	assert.Equal(t, "01JT4X9K2M8P0QRSTVWXYZ2345", got.ID)
	assert.Contains(t, got.Content, "contexts", "want the referencing block's own content")
	assert.Equal(t, "01JT4X9K2M8P0QRSTVWXYZ2346", got.TopicID)
	assert.Equal(t, "Go", got.TopicTitle)
}

func TestToolSurface(t *testing.T) {
	fake := &fakeBase{}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	got := map[string]bool{}
	for tool, err := range cs.Tools(t.Context(), nil) {
		require.NoError(t, err, "list tools")
		assert.NotEmpty(t, tool.Description, "tool %q has no description", tool.Name)
		got[tool.Name] = true
	}
	want := []string{"write_blocks", "search", "get_subtree", "topic_map", "get_backlinks"}
	assert.Len(t, got, len(want), "tools = %v, want exactly %v (no extra write tools)", got, want)
	for _, name := range want {
		assert.True(t, got[name], "missing tool %q", name)
	}
}

func TestReadToolsNeverWrite(t *testing.T) {
	fake := &fakeBase{
		searchHits: []knowledge.SearchHit{{Block: knowledge.Block{ID: "01A"}}},
		subtree:    []knowledge.Block{{ID: "01ROOT"}},
		topic:      []knowledge.Block{{ID: "01T"}},
	}
	cs := connect(t, NewServer(fake, fake, fake, testVersion))

	callSearch(t, cs, map[string]any{"query": "x"})
	callBlocks(t, cs, "get_subtree", map[string]any{"root": "01ROOT"})
	callBlocks(t, cs, "topic_map", map[string]any{})
	_, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_backlinks", Arguments: map[string]any{"id": "01T"},
	})
	require.NoError(t, err)

	assert.Zero(t, fake.writeCalls, "read tools called ApplyOps")
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
			require.NoError(t, err, "unexpected transport error")
			assert.True(t, res.IsError, "%s must report IsError on a capability failure", tc.tool)
		})
	}
}
