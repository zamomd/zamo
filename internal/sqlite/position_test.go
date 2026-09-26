package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func seedChildren(t *testing.T, repo *Repo, parent string, contents ...string) []string {
	t.Helper()
	ops := make([]knowledge.Op, 0, len(contents))
	ids := make([]string, 0, len(contents))
	for _, c := range contents {
		id := knowledge.ID(c + "-id")
		ids = append(ids, string(id))
		ops = append(ops, knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: id, Parent: parent, Content: c,
		})
	}
	require.NoError(t, repo.ApplyOps(t.Context(), knowledge.ID("g-"+parent), aiMeta("seed"), ops),
		"seed children of %q", parent)
	return ids
}

func TestAppendPositionsUseTheStride(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b", "c")

	for i, id := range []string{"a-id", "b-id", "c-id"} {
		_, pos, _, ok := blockRow(t, repo, id)
		require.True(t, ok, "block %s missing", id)
		assert.Equal(t, i*positionStride, pos, "position of %s", id)
	}
}

func TestInsertAfterMiddleSiblingTakesTheMidpoint(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072

	require.NoError(t, repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			After: "b-id", Content: "between b and c",
		}}), "insert after")

	_, pos, _, ok := blockRow(t, repo, "mid")
	require.True(t, ok, "inserted block missing")
	assert.Equal(t, positionStride+positionStride/2, pos, "midpoint of 65536 and 131072")
	for id, want := range map[string]int{"b-id": positionStride, "c-id": 2 * positionStride} {
		_, p, _, _ := blockRow(t, repo, id)
		assert.Equal(t, want, p, "neighbour %s must stay untouched", id)
	}
}

func TestInsertAfterLastSiblingAppends(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	require.NoError(t, repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "tail", Parent: "root",
			After: "b-id", Content: "after b",
		}}), "insert after last")
	_, pos, _, _ := blockRow(t, repo, "tail")
	assert.Equal(t, 2*positionStride, pos)
}

func TestInsertAfterForeignSiblingIsRefused(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{
			{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
			{Kind: knowledge.OpAdd, BlockID: "other", Content: "Other root"},
		}))
	seedChildren(t, repo, "root", "a")

	err := repo.ApplyOps(t.Context(), "g-bad", aiMeta("bad"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "x", Parent: "root",
			After: "other", Content: "nope",
		}})
	require.ErrorIs(t, err, knowledge.ErrInvalidOps)
	_, _, _, ok := blockRow(t, repo, "x")
	assert.False(t, ok, "refused batch still wrote the block")
}

func TestInsertBeforeFirstSiblingGoesAhead(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	require.NoError(t, repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "head", Parent: "root",
			Before: "a-id", Content: "before a",
		}}), "insert before first")
	_, pos, _, _ := blockRow(t, repo, "head")
	assert.Equal(t, -positionStride, pos)
	_, pos, _, _ = blockRow(t, repo, "a-id")
	assert.Zero(t, pos, "first child must stay untouched")
}

func TestInsertIntoExhaustedGapRespaces(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072
	for id, pos := range map[string]int{"a-id": 0, "b-id": 1, "c-id": 2} {
		_, err := repo.db.ExecContext(t.Context(),
			`UPDATE blocks SET position = ? WHERE id = ?`, pos, id)
		require.NoError(t, err)
	}

	require.NoError(t, repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			After: "b-id", Content: "squeezed",
		}}), "insert into exhausted gap")

	want := []struct {
		id  string
		pos int
	}{
		{"a-id", 0},
		{"b-id", positionStride},
		{"mid", positionStride + positionStride/2},
		{"c-id", 2 * positionStride},
	}
	for _, w := range want {
		_, pos, _, ok := blockRow(t, repo, w.id)
		assert.True(t, ok, "%s missing", w.id)
		assert.Equal(t, w.pos, pos, "position of %s", w.id)
	}
}

func TestApplyOpsMovesChildBeforeDeletingItsOldParent(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "target", Parent: "root", Content: "Target"},
		{Kind: knowledge.OpAdd, BlockID: "source", Parent: "root", Content: "Source"},
		{Kind: knowledge.OpAdd, BlockID: "kid", Parent: "source", Content: "Kid"},
	}))

	require.NoError(t, repo.ApplyOps(t.Context(), "g-merge", aiMeta("merge"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "target", Content: "Target+Source"},
		{Kind: knowledge.OpMove, BlockID: "kid", Parent: "target"},
		{Kind: knowledge.OpDelete, BlockID: "source"},
	}), "apply merge-shaped batch")

	parent, _, content, ok := blockRow(t, repo, "kid")
	require.True(t, ok, "kid did not survive the batch: applyDelete's cascade removed it")
	assert.Equal(t, "target", parent)
	assert.Equal(t, "Kid", content)
	_, _, _, ok = blockRow(t, repo, "source")
	assert.False(t, ok, "source still present after its own delete op")
}

func TestApplyMoveWithAfterLandsOnTheMidpoint(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "mover", Content: "Mover"}, // a lone topic root to reparent in
	}))
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	require.NoError(t, repo.ApplyOps(t.Context(), "g-move", aiMeta("move"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "mover", Parent: "root", After: "a-id"},
	}), "anchored move")

	_, pos, _, ok := blockRow(t, repo, "mover")
	require.True(t, ok, "moved block missing")
	assert.Equal(t, positionStride/2, pos, "midpoint of 0 and %d", positionStride)
}

func TestInsertBeforeIntoExhaustedGapRespaces(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}))
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072
	for id, pos := range map[string]int{"a-id": 0, "b-id": 1, "c-id": 2} {
		_, err := repo.db.ExecContext(t.Context(),
			`UPDATE blocks SET position = ? WHERE id = ?`, pos, id)
		require.NoError(t, err)
	}

	require.NoError(t, repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			Before: "c-id", Content: "squeezed",
		}}), "insert before into exhausted gap")

	want := []struct {
		id  string
		pos int
	}{
		{"a-id", 0},
		{"b-id", positionStride},
		{"mid", positionStride + positionStride/2},
		{"c-id", 2 * positionStride},
	}
	for _, w := range want {
		_, pos, _, ok := blockRow(t, repo, w.id)
		assert.True(t, ok, "%s missing", w.id)
		assert.Equal(t, w.pos, pos, "position of %s", w.id)
	}
}

func TestRenumberKeepsEveryRevisionAlignedForRollback(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()
	owner := knowledge.WriteMeta{
		Author: knowledge.AuthorOwner,
		Source: knowledge.SourceGUIManual,
		Title:  "setup",
	}
	require.NoError(t, repo.ApplyOps(ctx, "g-rollback-setup", owner, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "rollback-root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "rollback-first", Parent: "rollback-root", Content: "first"},
		{Kind: knowledge.OpAdd, BlockID: "rollback-bystander", Parent: "rollback-root", Content: "before"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "g-rollback-edit", aiMeta("edit bystander"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "rollback-bystander", Content: "pending"},
	}), "edit bystander")

	tx, err := repo.db.BeginTx(ctx, nil)
	require.NoError(t, err, "begin renumber")
	err = renumber(ctx, tx, "rollback-root")
	if err != nil {
		tx.Rollback()
	}
	require.NoError(t, err, "renumber")
	require.NoError(t, tx.Commit(), "commit renumber")

	wantPosition := positionStride
	for _, rev := range revisionsOf(t, repo, "rollback-bystander") {
		assert.Equal(t, wantPosition, rev.position, "revision %d position", rev.seq)
	}
	require.NoError(t, repo.RejectBlock(ctx, "rollback-bystander"), "reject bystander")
	parent, position, content, ok := blockRow(t, repo, "rollback-bystander")
	assert.True(t, ok, "bystander missing after rollback")
	assert.Equal(t, "rollback-root", parent)
	assert.Equal(t, wantPosition, position)
	assert.Equal(t, "before", content)
}
