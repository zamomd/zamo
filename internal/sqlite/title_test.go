package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func titleKeyOf(t *testing.T, repo *Repo, id knowledge.ID) string {
	t.Helper()
	var key any
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT title_key FROM blocks WHERE id = ?`, string(id)).Scan(&key), "read title_key of %s", id)
	if key == nil {
		return ""
	}
	s, ok := key.(string)
	require.True(t, ok, "title_key of %s is %T, want string or NULL", id, key)
	return s
}

func ownerOps(t *testing.T, repo *Repo, group knowledge.ID, ops ...knowledge.Op) error {
	t.Helper()
	return repo.ApplyOps(t.Context(), group, ownerMeta("test"), ops)
}

func TestTitleKeyOnlyForRoots(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "## Local-First  Architecture"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01CHILD", Parent: "01ROOT", Content: "A child"},
	))
	assert.Equal(t, "local-first architecture", titleKeyOf(t, repo, "01ROOT"), "root")
	assert.Empty(t, titleKeyOf(t, repo, "01CHILD"), "child")
}

func TestTitleKeyFollowsEdits(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Old name"},
	), "add")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01ROOT", Content: "New name"},
	), "edit")
	assert.Equal(t, "new name", titleKeyOf(t, repo, "01ROOT"))
}

func TestTitleKeyFollowsMoves(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Host"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "Guest"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01KID", Parent: "01R1", Content: "Promoted"},
	), "apply")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpMove, BlockID: "01R2", Parent: "01R1"},
	), "demote")
	assert.Empty(t, titleKeyOf(t, repo, "01R2"), "demoted root")
	require.NoError(t, ownerOps(t, repo, "01G3",
		knowledge.Op{Kind: knowledge.OpMove, BlockID: "01KID"},
	), "promote")
	assert.Equal(t, "promoted", titleKeyOf(t, repo, "01KID"), "promoted child")
}

func TestDuplicateTopicNameRejected(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Local-first"},
	))
	err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "  LOCAL-FIRST  "},
	)
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic)
	var n int
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM blocks WHERE id = '01R2'`).Scan(&n))
	assert.Zero(t, n, "rejected add left rows")
}

// A bracket cannot be written inside [[...]], so a root carrying one is not
// referenceable — which is the state an empty name already has, and the design
// admits: the root is stored and renders, it just has no key. Refusing the name
// instead would kill a whole batch over ordinary titles like "- [ ] Ship v1".
func TestBracketedTopicNameIsUnnamed(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		repo := testRepo(t)
		require.NoError(t, ownerOps(t, repo, "01G1",
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "- [ ] Ship v1"}))
		_, _, content, ok := blockRow(t, repo, "01ROOT")
		assert.True(t, ok)
		assert.Equal(t, "- [ ] Ship v1", content, "want the title stored verbatim")
		assert.Empty(t, titleKeyOf(t, repo, "01ROOT"), "nothing can reference this name")
	})

	t.Run("edit", func(t *testing.T) {
		repo := testRepo(t)
		require.NoError(t, ownerOps(t, repo, "01G1",
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Local-first"}), "add")
		require.NoError(t, ownerOps(t, repo, "01G2",
			knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01ROOT", Content: "Reading list [2026]"}), "edit")
		assert.Empty(t, titleKeyOf(t, repo, "01ROOT"))
	})

	t.Run("move to root level", func(t *testing.T) {
		repo := testRepo(t)
		require.NoError(t, ownerOps(t, repo, "01G1",
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Local-first"},
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01KID", Parent: "01ROOT", Content: "See [x] below"},
		), "add")
		require.NoError(t, ownerOps(t, repo, "01G2", knowledge.Op{Kind: knowledge.OpMove, BlockID: "01KID"}), "promote")
		parent, _, _, ok := blockRow(t, repo, "01KID")
		assert.True(t, ok)
		assert.Empty(t, parent, "want a root after the move")
		assert.Empty(t, titleKeyOf(t, repo, "01KID"))
	})

	// NULL is not constrained by the partial unique index, so several such
	// roots coexist exactly as several unnamed ones do.
	t.Run("several coexist", func(t *testing.T) {
		repo := testRepo(t)
		require.NoError(t, ownerOps(t, repo, "01G1",
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Notes [WIP]"},
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "TODO: fix [bug]"},
			knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R3", Content: "[Zig](https://ziglang.org)"},
		))
	})
}

// The rule is about names, and only a root has one.
func TestBracketedChildContentIsFine(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Local-first"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01KID", Parent: "01ROOT", Content: "- [ ] read [[Zig]]"},
	))
	assert.Empty(t, titleKeyOf(t, repo, "01KID"), "child")
}

func TestUnnamedRootsCoexist(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "###"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "   "},
	))
	assert.Empty(t, titleKeyOf(t, repo, "01R1"), "marker-only root")
}

func TestTitleKeyRevertsWithUndoneRename(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Old name"},
	), "add")
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01ROOT", Content: "New name"},
	), "rename")
	require.NoError(t, repo.UndoGroup(t.Context(), "01G2"), "undo")
	_, _, content, ok := blockRow(t, repo, "01ROOT")
	require.True(t, ok)
	require.Equal(t, "Old name", content, "content after undo")
	assert.Equal(t, "old name", titleKeyOf(t, repo, "01ROOT"), "after undo")
}

func TestTitleKeyRestoredWithTopic(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Local-first"},
	))
	require.NoError(t, repo.ApplyOps(t.Context(), "01G2", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01ROOT"},
	}), "ai delete")
	require.NoError(t, repo.RejectBlock(t.Context(), "01ROOT"), "reject")
	assert.Equal(t, "local-first", titleKeyOf(t, repo, "01ROOT"), "after restore")
}
