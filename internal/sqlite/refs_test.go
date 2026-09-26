package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func refsOf(t *testing.T, repo *Repo, source knowledge.ID) []string {
	t.Helper()
	rows, err := repo.db.QueryContext(t.Context(),
		`SELECT kind || ':' || target_key FROM refs WHERE source_id = ? ORDER BY kind, target_key`,
		string(source))
	require.NoError(t, err, "read refs of %s", source)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s), "scan refs of %s", source)
		out = append(out, s)
	}
	require.NoError(t, rows.Err(), "read refs of %s", source)
	return out
}

func TestRefsIndexedOnAdd(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT",
			Content: "Compare [[Rust]] with ((01JT4X9K2M8P0QRSTVWXYZ2345|a claim)).",
		},
	), "apply")
	want := []string{"block:01JT4X9K2M8P0QRSTVWXYZ2345", "topic:rust"}
	assert.Equal(t, want, refsOf(t, repo, "01NOTE"))
}

func TestRefsRebuiltOnEdit(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	), "apply")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "See [[Go]] instead."},
	), "edit")
	assert.Equal(t, []string{"topic:go"}, refsOf(t, repo, "01NOTE"))
}

func TestRefsClearedOnDelete(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	), "apply")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	), "delete")
	assert.Empty(t, refsOf(t, repo, "01NOTE"))
}

func TestRefsClearedForDeletedSubtree(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01DEEP", Parent: "01NOTE", Content: "And [[Go]]."},
	), "apply")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	), "delete")
	assert.Empty(t, refsOf(t, repo, "01DEEP"))
}

func TestRefsRestoredWithBlock(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	), "apply")
	require.NoError(t, repo.ApplyOps(t.Context(), "01G2", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	}), "ai delete")
	require.NoError(t, repo.RejectBlock(t.Context(), "01NOTE"), "reject")
	assert.Equal(t, []string{"topic:rust"}, refsOf(t, repo, "01NOTE"))
}

func TestRefsClearedWhenRejectedAddVanishes(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
	), "apply")
	require.NoError(t, repo.ApplyOps(t.Context(), "01G2", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	}), "ai add")
	require.NoError(t, repo.RejectBlock(t.Context(), "01NOTE"), "reject")
	_, _, _, ok := blockRow(t, repo, "01NOTE")
	require.False(t, ok, "rejected add left a block row")
	assert.Empty(t, refsOf(t, repo, "01NOTE"))
}

func TestRefsRevertWithUndoneGroup(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	), "apply")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "See [[Go]] instead."},
	), "edit")
	require.NoError(t, repo.UndoGroup(t.Context(), "01G2"), "undo")
	assert.Equal(t, []string{"topic:rust"}, refsOf(t, repo, "01NOTE"))
}
