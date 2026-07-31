package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func TestWriteVisibleAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")

	writer, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })

	reader, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	before, err := r(reader).GetBlocks(t.Context(), []knowledge.ID{"01MCP"})
	if err != nil {
		t.Fatalf("GetBlocks before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("reader saw %d blocks before any write, want 0", len(before))
	}

	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "mcp write"}
	if err := r(writer).ApplyOps(t.Context(), "01GRP", meta, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01MCP", Content: "from mcp"},
	}); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}

	after, err := r(reader).GetBlocks(t.Context(), []knowledge.ID{"01MCP"})
	if err != nil {
		t.Fatalf("GetBlocks after: %v", err)
	}
	if len(after) != 1 || after["01MCP"].Content != "from mcp" {
		t.Fatalf("reader saw %v after cross-connection write, want one 01MCP", after)
	}
}
