package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
)

func ownerMetaFor(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: title}
}

// scopedTree builds root > (kid, other) and a separate topic, all reviewed.
func scopedTree(t *testing.T, repo *Repo) {
	t.Helper()
	require.NoError(t, applyOps(t.Context(), repo, "g-setup", ownerMetaFor("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Go releases"},
		{Kind: knowledge.OpAdd, BlockID: "kid", Parent: "root", Content: "1.25"},
		{Kind: knowledge.OpAdd, BlockID: "other", Parent: "root", Content: "1.26"},
		{Kind: knowledge.OpAdd, BlockID: "elsewhere", Content: "Rust editions"},
	}), "setup")
}

func refusing(scope knowledge.ID) knowledge.WriteMeta {
	meta := aiMeta("scheduled refresh")
	meta.RefuseOverUnreviewedIn = scope
	return meta
}

func TestApplyOpsRefusesOverUnreviewedEditInScope(t *testing.T) {
	repo := testRepo(t)
	scopedTree(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "g-ai", aiMeta("pending"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "kid", Content: "1.25.1"},
	}), "pending edit")

	err := applyOps(t.Context(), repo, "g-cron", refusing("root"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "other", Content: "1.26.2"},
	})

	require.ErrorIs(t, err, knowledge.ErrUnreviewedInScope)
	var scoped *knowledge.UnreviewedInScopeError
	if assert.ErrorAs(t, err, &scoped) {
		assert.Equal(t, 1, scoped.Count)
	}
	_, _, content, _ := blockRow(t, repo, "other")
	assert.Equal(t, "1.26", content, "untouched by the refused batch")
	assert.Equal(t, 2, countRows(t, repo, "revision_groups"), "the refused batch leaves none")
}

func TestApplyOpsRefusesOverPendingDeletionInScope(t *testing.T) {
	repo := testRepo(t)
	scopedTree(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "g-ai", aiMeta("pending delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "kid"},
	}), "pending delete")

	err := applyOps(t.Context(), repo, "g-cron", refusing("root"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "other", Content: "1.26.2"},
	})

	require.ErrorIs(t, err, knowledge.ErrUnreviewedInScope, "a ghost in scope")
}

func TestApplyOpsIgnoresUnreviewedOutsideScope(t *testing.T) {
	repo := testRepo(t)
	scopedTree(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "g-ai", aiMeta("pending elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "elsewhere", Content: "Rust editions 2027"},
	}), "pending edit")

	require.NoError(t, applyOps(t.Context(), repo, "g-cron", refusing("root"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "other", Content: "1.26.2"},
	}), "nothing is pending under root")
}

func TestApplyOpsWithoutRefusalStacksAsBefore(t *testing.T) {
	repo := testRepo(t)
	scopedTree(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "g-ai", aiMeta("pending"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "kid", Content: "1.25.1"},
	}), "pending edit")

	require.NoError(t, applyOps(t.Context(), repo, "g-ai-2", aiMeta("second"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "kid", Content: "1.25.2"},
	}), "a batch without the refusal must apply")
}

func TestCountUnreviewedUnder(t *testing.T) {
	repo := testRepo(t)
	scopedTree(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "g-ai", aiMeta("pending"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "kid", Content: "1.25.1"},
		{Kind: knowledge.OpDelete, BlockID: "other"},
		{Kind: knowledge.OpEdit, BlockID: "elsewhere", Content: "Rust"},
	}), "pending")

	n, err := repo.CountUnreviewedUnder(t.Context(), "root")
	require.NoError(t, err)
	assert.Equal(t, 2, n, "the edit and the ghost, not the other topic")
}
