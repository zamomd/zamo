package knowledge

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outlineService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	blocks := map[ID]Block{
		"root":   {ID: "root", Content: "Root"},
		"first":  {ID: "first", ParentID: "root", Position: 0, Content: "First"},
		"second": {ID: "second", ParentID: "root", Position: 65536, Content: "Second"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"root": {blocks["root"], blocks["first"], blocks["second"]},
		},
	}
	return NewService(nil, repo, func() ID { return "new-1" }), repo
}

func TestSplitBlockEditsHeadAndAnchorsTailAfterIt(t *testing.T) {
	svc, repo := outlineService(t)

	change, err := svc.SplitBlock(t.Context(), "first", "Fi", "rst")
	require.NoError(t, err)
	assert.Equal(t, ID("new-1"), change.BlockID)
	assert.Equal(t, AuthorOwner, repo.applyMeta.Author, "born reviewed")
	assert.Equal(t, SourceGUIManual, repo.applyMeta.Source, "born reviewed")
	require.Len(t, repo.applyOps, 2, "want an edit and an add")
	edit, add := repo.applyOps[0], repo.applyOps[1]
	assert.Equal(t, OpEdit, edit.Kind)
	assert.Equal(t, ID("first"), edit.BlockID)
	assert.Equal(t, "Fi", edit.Content)
	assert.Equal(t, OpAdd, add.Kind)
	assert.Equal(t, "root", add.Parent)
	assert.Equal(t, "first", add.After)
	assert.Equal(t, "rst", add.Content)
	assert.Empty(t, add.Before, "want only After")
}

func TestInsertBeforeAnchorsAheadOfTheBlock(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.InsertBefore(t.Context(), "first", "")
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1, "want a single add")
	add := repo.applyOps[0]
	assert.Equal(t, OpAdd, add.Kind)
	assert.Equal(t, "root", add.Parent)
	assert.Equal(t, "first", add.Before)
	assert.Empty(t, add.Content)
}

func TestSplitBlockRefusesATopicRoot(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.SplitBlock(t.Context(), "root", "Ro", "ot")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "refused before any write")
}

func TestSplitBlockReportsAMissingBlock(t *testing.T) {
	svc, _ := outlineService(t)

	_, err := svc.SplitBlock(t.Context(), "ghost", "a", "b")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestIndentBlockMovesItUnderThePreviousSibling(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.IndentBlock(t.Context(), "second")
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1, "want a single move")
	move := repo.applyOps[0]
	assert.Equal(t, OpMove, move.Kind)
	assert.Equal(t, ID("second"), move.BlockID)
	assert.Equal(t, "first", move.Parent)
	assert.Empty(t, move.After, "want no anchor (appended)")
	assert.Empty(t, move.Before, "want no anchor (appended)")
}

func TestIndentFirstChildIsRefused(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.IndentBlock(t.Context(), "first")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls)
}

func TestOutdentBlockMovesItAfterItsParent(t *testing.T) {
	blocks := map[ID]Block{
		"root":  {ID: "root", Content: "Root"},
		"mid":   {ID: "mid", ParentID: "root", Position: 0, Content: "Mid"},
		"leaf":  {ID: "leaf", ParentID: "mid", Position: 0, Content: "Leaf"},
		"after": {ID: "after", ParentID: "root", Position: 65536, Content: "After"},
	}
	repo := &fakeRepo{
		blocks: blocks,
	}
	svc := NewService(nil, repo, func() ID { return "new-1" })

	_, err := svc.OutdentBlock(t.Context(), "leaf")
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1, "want a single move (following siblings untouched)")
	move := repo.applyOps[0]
	assert.Equal(t, OpMove, move.Kind)
	assert.Equal(t, ID("leaf"), move.BlockID)
	assert.Equal(t, "root", move.Parent)
	assert.Equal(t, "mid", move.After)
}

func TestOutdentChildOfTopicRootIsRefused(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.OutdentBlock(t.Context(), "first")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls)
}

func TestMergeBlocksMovesChildrenBeforeDeletingTheSource(t *testing.T) {
	blocks := map[ID]Block{
		"root":   {ID: "root", Content: "Root"},
		"target": {ID: "target", ParentID: "root", Position: 0, Content: "Head "},
		"source": {ID: "source", ParentID: "root", Position: 65536, Content: "tail"},
		"kid-a":  {ID: "kid-a", ParentID: "source", Position: 0, Content: "A"},
		"kid-b":  {ID: "kid-b", ParentID: "source", Position: 65536, Content: "B"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"source": {blocks["source"], blocks["kid-a"], blocks["kid-b"]},
		},
	}
	svc := NewService(nil, repo, func() ID { return "new-1" })

	_, err := svc.MergeBlocks(t.Context(), "target", "source")
	require.NoError(t, err)

	kinds := make([]OpKind, 0, len(repo.applyOps))
	for _, op := range repo.applyOps {
		kinds = append(kinds, op.Kind)
	}
	want := []OpKind{OpEdit, OpMove, OpMove, OpDelete}
	require.Equal(t, want, kinds)
	assert.Equal(t, "Head tail", repo.applyOps[0].Content)
	for _, op := range repo.applyOps[1:3] {
		assert.Equal(t, "target", op.Parent, "child move %+v", op)
	}
	assert.Equal(t, ID("source"), repo.applyOps[3].BlockID)
}

func TestMergeBlocksMovesOnlyDirectChildren(t *testing.T) {
	blocks := map[ID]Block{
		"root":     {ID: "root", Content: "Root"},
		"target":   {ID: "target", ParentID: "root", Position: 0, Content: "Head "},
		"source":   {ID: "source", ParentID: "root", Position: 65536, Content: "tail"},
		"kid-a":    {ID: "kid-a", ParentID: "source", Position: 0, Content: "A"},
		"kid-b":    {ID: "kid-b", ParentID: "source", Position: 65536, Content: "B"},
		"grandkid": {ID: "grandkid", ParentID: "kid-b", Position: 0, Content: "G"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"source": {blocks["source"], blocks["kid-a"], blocks["kid-b"], blocks["grandkid"]},
		},
	}
	svc := NewService(nil, repo, func() ID { return "new-1" })

	_, err := svc.MergeBlocks(t.Context(), "target", "source")
	require.NoError(t, err)

	var moved []ID
	for _, op := range repo.applyOps {
		if op.Kind == OpMove {
			moved = append(moved, op.BlockID)
		}
	}
	assert.NotContains(t, moved, ID("grandkid"), "grandkid must not be reparented directly")
	want := []ID{"kid-a", "kid-b"}
	require.Equal(t, want, moved, "want only source's direct children")
}

func TestMergeBlocksWithNoChildrenEmitsNoMoves(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MergeBlocks(t.Context(), "first", "second")
	require.NoError(t, err)

	kinds := make([]OpKind, 0, len(repo.applyOps))
	for _, op := range repo.applyOps {
		kinds = append(kinds, op.Kind)
	}
	want := []OpKind{OpEdit, OpDelete}
	require.Equal(t, want, kinds)
}

func TestMergeBlocksRefusesMergingIntoItself(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MergeBlocks(t.Context(), "first", "first")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls)
}

func TestMergeBlocksRefusesATopicRootAsSource(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MergeBlocks(t.Context(), "first", "root")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "refused before any write")
}

func TestMergeBlocksRefusesATopicRootAsTarget(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MergeBlocks(t.Context(), "root", "first")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "refused before any write")
}

func TestDeleteBlockEmitsOneDelete(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.DeleteBlock(t.Context(), "second")
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1)
	assert.Equal(t, OpDelete, repo.applyOps[0].Kind)
	assert.Equal(t, ID("second"), repo.applyOps[0].BlockID)
}

func TestMoveBlockPassesTheAnchorThrough(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MoveBlock(t.Context(), "second", "root", "first")
	require.NoError(t, err)
	move := repo.applyOps[0]
	assert.Equal(t, OpMove, move.Kind)
	assert.Equal(t, ID("second"), move.BlockID)
	assert.Equal(t, "root", move.Parent)
	assert.Equal(t, "first", move.After)
}

func TestMoveBlockRefusesATopicRoot(t *testing.T) {
	svc, repo := outlineService(t)

	_, err := svc.MoveBlock(t.Context(), "root", "first", "")
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "refused before any write")
}

func sequencedOutlineService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	svc, repo := outlineService(t)
	n := 0
	svc.newID = func() ID { n++; return ID(fmt.Sprintf("id-%d", n)) }
	return svc, repo
}

func TestOutlineGesturesReportTheBatchTheyWrote(t *testing.T) {
	t.Run("split also reports the block it created", func(t *testing.T) {
		svc, repo := sequencedOutlineService(t)

		change, err := svc.SplitBlock(t.Context(), "first", "Fi", "rst")
		require.NoError(t, err)
		assert.Equal(t, repo.applyGroupID, change.GroupID)
		assert.Equal(t, ID("id-1"), change.BlockID, "want the new tail")
	})

	t.Run("a gesture that creates nothing still reports its batch", func(t *testing.T) {
		svc, repo := sequencedOutlineService(t)

		change, err := svc.IndentBlock(t.Context(), "second")
		require.NoError(t, err)
		assert.Equal(t, repo.applyGroupID, change.GroupID)
		assert.Empty(t, change.BlockID, "indent creates no block")
	})
}
