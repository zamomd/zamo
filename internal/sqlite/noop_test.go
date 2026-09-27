package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
)

func TestApplyOpsIdenticalEditWritesNothing(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()
	require.NoError(t, applyOps(ctx, repo, "grp-1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "same"},
	}), "add")

	changed, err := repo.ApplyOps(ctx, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "same"},
	})

	require.NoError(t, err)
	require.False(t, changed, "identical edit must change nothing")
	assert.Len(t, revisionsOf(t, repo, "b1"), 1, "want the creation revision only")
	assert.Equal(t, 1, countRows(t, repo, "revision_groups"), "an empty batch leaves no group")
}

func TestApplyOpsSkipsIdenticalEditInsideEffectiveBatch(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()
	require.NoError(t, applyOps(ctx, repo, "grp-1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "same"},
		{Kind: knowledge.OpAdd, BlockID: "b2", Content: "old"},
	}), "add")

	require.NoError(t, applyOps(ctx, repo, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "same"},
		{Kind: knowledge.OpEdit, BlockID: "b2", Content: "new"},
	}), "edit")

	assert.Len(t, revisionsOf(t, repo, "b1"), 1, "want none added for the identical edit")
	assert.Len(t, revisionsOf(t, repo, "b2"), 2, "want the effective edit recorded")
}

func TestApplyOpsEditChangingOnlyModeIsEffective(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()
	require.NoError(t, applyOps(ctx, repo, "grp-1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "topic"},
	}), "add")

	require.NoError(t, applyOps(ctx, repo, "grp-2", aiMeta("convert"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "topic", Mode: knowledge.ModeDoc},
	}), "mode edit")

	assert.Len(t, revisionsOf(t, repo, "b1"), 2, "want the mode change recorded")
}
