package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/require"
)

func TestWriteVisibleAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")

	writer, err := Open(t.Context(), path)
	require.NoError(t, err, "Open writer")
	t.Cleanup(func() { writer.Close() })

	reader, err := Open(t.Context(), path)
	require.NoError(t, err, "Open reader")
	t.Cleanup(func() { reader.Close() })

	before, err := r(reader).GetBlocks(t.Context(), []knowledge.ID{"01MCP"})
	require.NoError(t, err)
	require.Empty(t, before, "reader saw blocks before any write")

	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "mcp write"}
	require.NoError(t, r(writer).ApplyOps(t.Context(), "01GRP", meta, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01MCP", Content: "from mcp"},
	}))

	after, err := r(reader).GetBlocks(t.Context(), []knowledge.ID{"01MCP"})
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "from mcp", after["01MCP"].Content)
}
