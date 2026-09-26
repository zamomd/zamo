package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
)

func ownerMeta(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: title}
}

// modeOf reads a block's stored mode.
func modeOf(t *testing.T, repo *Repo, id string) knowledge.Mode {
	t.Helper()
	var mode knowledge.Mode
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT mode FROM blocks WHERE id = ?`, id).Scan(&mode), "read mode of %s", id)
	return mode
}

func TestDeleteHeadRevisionRevertsProjection(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "original"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "edited"},
	}))
	revs := revisionsOf(t, repo, "b1")
	require.Len(t, revs, 2)
	editID := "rev-002"

	require.NoError(t, repo.DeleteRevision(ctx, "b1", knowledge.ID(editID)))

	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok)
	assert.Equal(t, "original", content)
	assert.Len(t, revisionsOf(t, repo, "b1"), 1)
}

func TestDeleteMiddleRevisionKeepsState(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("edit1"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-3", aiMeta("edit2"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v3"},
	}))

	require.NoError(t, repo.DeleteRevision(ctx, "b1", knowledge.ID("rev-002")))

	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok)
	assert.Equal(t, "v3", content)
	revs := revisionsOf(t, repo, "b1")
	require.Len(t, revs, 2)
	assert.Equal(t, "v3", revs[len(revs)-1].content, "surviving head content")
}

func TestDeleteLastRevisionRemovesBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "leaf", Parent: "root", Content: "leaf"},
	}))

	require.NoError(t, repo.DeleteRevision(ctx, "leaf", knowledge.ID("rev-002")))

	_, _, _, ok := blockRow(t, repo, "leaf")
	assert.False(t, ok, "leaf still present in blocks after deleting its only revision")
	assert.Empty(t, revisionsOf(t, repo, "leaf"))
}

func TestDeleteHeadTombstoneResurrects(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "alive text"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "b1"},
	}))
	_, _, _, ok := blockRow(t, repo, "b1")
	require.False(t, ok, "b1 still present after tombstoning")

	require.NoError(t, repo.DeleteRevision(ctx, "b1", knowledge.ID("rev-002")))

	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok)
	assert.Equal(t, "alive text", content)
	hits, err := repo.Search(ctx, "alive")
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, knowledge.ID("b1"), hits[0].Block.ID)
}

func TestDeleteRevisionOrphanGuard(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "child", Parent: "root", Content: "child"},
	}))

	err := repo.DeleteRevision(ctx, "root", knowledge.ID("rev-001"))
	require.ErrorIs(t, err, knowledge.ErrInvalidRollback)
	_, _, _, ok := blockRow(t, repo, "root")
	assert.True(t, ok, "root removed from blocks despite the refused rollback")
	assert.Len(t, revisionsOf(t, repo, "root"), 1)
}

func TestDeleteRevisionParentGuard(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P"},
		{Kind: knowledge.OpAdd, BlockID: "x", Parent: "p", Content: "X"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("delete x"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "x"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-3", aiMeta("delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}))

	err := repo.DeleteRevision(ctx, "x", knowledge.ID("rev-003"))
	require.ErrorIs(t, err, knowledge.ErrInvalidRollback)
	_, _, _, ok := blockRow(t, repo, "x")
	assert.False(t, ok, "x resurrected in blocks despite its parent p being gone")
}

func TestRejectRestoresTheModeTheBlockHad(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", owner, add))
	convert := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert))
	require.Equal(t, knowledge.ModeDoc, modeOf(t, repo, "t1"), "before reject")
	require.NoError(t, repo.RejectBlock(t.Context(), "t1"))
	assert.Equal(t, knowledge.ModeOutline, modeOf(t, repo, "t1"), "after reject")
}

func TestUndoGroupRestoresTheModeTheBlockHad(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", owner, add))
	convert := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", owner, convert))
	require.Equal(t, knowledge.ModeDoc, modeOf(t, repo, "t1"), "before undo")
	require.NoError(t, repo.UndoGroup(t.Context(), "g2"))
	assert.Equal(t, knowledge.ModeOutline, modeOf(t, repo, "t1"), "after undo")
}

func TestRejectLeavesADocumentADocument(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", owner, add))
	// A content-only AI edit: it names no mode, so the surviving revision is
	// the only place the mode can come back from. A projectHead that writes the
	// column's default instead of the snapshot's value silently turns the
	// document into an outline here, and nothing else in the system complains.
	edit := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic\n\nBody."}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("edit"), edit))
	require.NoError(t, repo.RejectBlock(t.Context(), "t1"))
	assert.Equal(t, knowledge.ModeDoc, modeOf(t, repo, "t1"), "after reject")
}

// convertedToDocument builds an outline topic t1 with a child c1, both
// owner-written and reviewed, then applies the conversion an MCP client sends:
// one AI batch that makes the root a document and deletes the child. The batch
// is legal — no child is left under a document at its end — and unreviewed, so
// both halves of it are still open to a per-block review decision.
func convertedToDocument(t *testing.T, repo *Repo) {
	t.Helper()
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", ownerMeta("build"), build))
	convert := []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic\n\n- child", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpDelete, BlockID: "c1"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert))
}

func TestRejectRefusesToResurrectAChildUnderADocument(t *testing.T) {
	repo := testRepo(t)
	convertedToDocument(t, repo)

	err := repo.RejectBlock(t.Context(), "c1")
	require.ErrorIs(t, err, knowledge.ErrInvalidRollback)
	_, _, _, ok := blockRow(t, repo, "c1")
	assert.False(t, ok, "the refused reject brought the child back anyway")
	require.NoError(t, repo.ApplyOps(t.Context(), "g3", ownerMeta("elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t2", Content: "Other"},
	}), "write after the refusal")
}

func TestRejectPageUndoesADocumentConversionWhole(t *testing.T) {
	repo := testRepo(t)
	convertedToDocument(t, repo)

	// The page holds both halves — the root's edit and the child's ghost — so
	// the projection order the child needs (its parent back to an outline
	// first) exists and projectFixpoint has to find it.
	require.NoError(t, repo.RejectPage(t.Context(), "t1"))
	assert.Equal(t, knowledge.ModeOutline, modeOf(t, repo, "t1"), "after reject page")
	_, _, _, ok := blockRow(t, repo, "c1")
	assert.True(t, ok, "the child was not restored")
}

// expandedFromDocument builds a document topic t1, then applies one legal batch
// taking it back to an outline and giving it a child — the reverse conversion,
// as an unreviewed AI batch.
func expandedFromDocument(t *testing.T, repo *Repo) {
	t.Helper()
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", ownerMeta("add"), add))
	expand := []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic", Mode: knowledge.ModeOutline},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("expand"), expand))
}

func TestRejectRefusesToRestoreADocumentOverLiveChildren(t *testing.T) {
	repo := testRepo(t)
	expandedFromDocument(t, repo)

	err := repo.RejectBlock(t.Context(), "t1")
	require.ErrorIs(t, err, knowledge.ErrInvalidRollback)
	assert.Equal(t, knowledge.ModeOutline, modeOf(t, repo, "t1"), "after the refused reject")
	require.NoError(t, repo.ApplyOps(t.Context(), "g3", ownerMeta("elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t2", Content: "Other"},
	}), "write after the refusal")
}

func TestRejectPageUndoesAnOutlineConversionWhole(t *testing.T) {
	repo := testRepo(t)
	expandedFromDocument(t, repo)

	require.NoError(t, repo.RejectPage(t.Context(), "t1"))
	assert.Equal(t, knowledge.ModeDoc, modeOf(t, repo, "t1"), "after reject page")
	_, _, _, ok := blockRow(t, repo, "c1")
	assert.False(t, ok, "the child added by the rejected batch is still live")
}

// entryFor returns the queue entry for a block, which is how the Changes
// screen sees it.
func entryFor(t *testing.T, repo *Repo, blockID knowledge.ID) knowledge.UnreviewedBlock {
	t.Helper()
	changes, err := repo.ListUnreviewed(t.Context())
	require.NoError(t, err)
	for _, b := range changes.Blocks {
		if b.Head.BlockID == blockID {
			return b
		}
	}
	require.FailNow(t, "block is not awaiting review", "block %s", blockID)
	return knowledge.UnreviewedBlock{}
}

// buriedBatch applies a two-block AI batch g2 and then a second AI batch g3
// over one of its blocks. g2 is written over — UndoGroup refuses it — but
// nothing in b2's own entry says so: its pending revision is g2's, its head is
// that same revision, and the burying happened on b1, which the queue reports
// under g3 because an unreviewed revision becomes the group it reports.
func buriedBatch(t *testing.T, repo *Repo) {
	t.Helper()
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "one"},
		{Kind: knowledge.OpAdd, BlockID: "b2", Content: "two"},
	}))
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("batch"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "one by g2"},
		{Kind: knowledge.OpEdit, BlockID: "b2", Content: "two by g2"},
	}))
	require.NoError(t, repo.ApplyOps(t.Context(), "g3", aiMeta("later"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "one by g3"},
	}))
}

func TestUnreviewedReportsABatchBuriedByAnUnreviewedOne(t *testing.T) {
	repo := testRepo(t)
	buriedBatch(t, repo)

	entry := entryFor(t, repo, "b2")
	require.Equal(t, knowledge.ID("g2"), entry.GroupID)
	// The comparison a client could make on the payload alone says nothing
	// here — which is why the answer has to be computed where the revisions
	// are. Asserted, not assumed: without it the case under test is a
	// different one.
	require.Equal(t, entry.GroupID, entry.Head.GroupID,
		"head group differs from the pending group, so this is not the case under test")
	assert.True(t, entry.WrittenOver, "g2 reports as still undoable, so the screen keeps offering to reject it")
	assert.ErrorIs(t, repo.UndoGroup(t.Context(), "g2"), knowledge.ErrGroupWrittenOver)
}

func TestUnreviewedLeavesAnUnburiedBatchAlone(t *testing.T) {
	repo := testRepo(t)
	buriedBatch(t, repo)

	// The batch on top of g2 has nothing over it in turn, so its own controls
	// must stay: over-reporting hides a decision the user can still make.
	entry := entryFor(t, repo, "b1")
	require.Equal(t, knowledge.ID("g3"), entry.GroupID)
	assert.False(t, entry.WrittenOver, "g3 reports as written over, hiding a reject that would have worked")
	assert.NoError(t, repo.UndoGroup(t.Context(), "g3"))
}

func TestRejectBlockDropsUnreviewedChain(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "owner text"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("edit1"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "ai edit 1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-3", aiMeta("edit2"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "ai edit 2"},
	}))

	require.NoError(t, repo.RejectBlock(ctx, "b1"))
	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok)
	assert.Equal(t, "owner text", content)
	assert.Len(t, revisionsOf(t, repo, "b1"), 1)

	require.NoError(t, repo.RejectBlock(ctx, "b1"), "no-op case")
	assert.Len(t, revisionsOf(t, repo, "b1"), 1, "after no-op RejectBlock")
}

func TestRejectBlockOnAIAddRemovesBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "leaf", Content: "ai text"},
	}))

	require.NoError(t, repo.RejectBlock(ctx, "leaf"))
	_, _, _, ok := blockRow(t, repo, "leaf")
	assert.False(t, ok, "leaf still present after rejecting its only (AI) revision")
	assert.Empty(t, revisionsOf(t, repo, "leaf"))
}

func TestMarkReviewedVariants(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-root", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
	}))

	require.NoError(t, repo.ApplyOps(ctx, "grp-b", aiMeta("ai add b"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "topic", Content: "b text"},
	}))
	require.NoError(t, repo.MarkBlockReviewed(ctx, "b"))
	assertNotInUnreviewed(t, repo, "b")

	require.NoError(t, repo.ApplyOps(ctx, "grp-g", aiMeta("ai add g"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "topic", Content: "g text"},
	}))
	require.NoError(t, repo.MarkGroupReviewed(ctx, "grp-g"))
	assertNotInUnreviewed(t, repo, "g")

	require.NoError(t, repo.ApplyOps(ctx, "grp-c", aiMeta("ai add c"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "topic", Content: "c text"},
	}))
	require.NoError(t, repo.MarkBlockReviewed(ctx, "c"))
	require.NoError(t, repo.ApplyOps(ctx, "grp-del-c", aiMeta("ai delete c"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "c"},
	}))
	assertInUnreviewed(t, repo, "c")

	require.NoError(t, repo.MarkPageReviewed(ctx, "topic"))
	assertNotInUnreviewed(t, repo, "c")
}

func assertInUnreviewed(t *testing.T, repo *Repo, id string) {
	t.Helper()
	assert.True(t, blockIsUnreviewed(t, repo, id), "block %s missing from ListUnreviewed, want present", id)
}

func assertNotInUnreviewed(t *testing.T, repo *Repo, id string) {
	t.Helper()
	assert.False(t, blockIsUnreviewed(t, repo, id), "block %s still in ListUnreviewed, want absent", id)
}

func blockIsUnreviewed(t *testing.T, repo *Repo, id string) bool {
	t.Helper()
	changes, err := repo.ListUnreviewed(t.Context())
	require.NoError(t, err)
	for _, ub := range changes.Blocks {
		if string(ub.Head.BlockID) == id {
			return true
		}
	}
	return false
}

func TestListUnreviewedReturnsHeadsTopicsAndGhosts(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-root", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "live", Parent: "root", Content: "live v1"},
		{Kind: knowledge.OpAdd, BlockID: "gone", Parent: "root", Content: "gone v1"},
		{Kind: knowledge.OpAdd, BlockID: "buried", Parent: "root", Content: "buried v1"},
	}))
	for _, id := range []string{"live", "gone", "buried"} {
		require.NoError(t, repo.MarkBlockReviewed(ctx, knowledge.ID(id)), "mark %s reviewed", id)
	}

	require.NoError(t, repo.ApplyOps(ctx, "grp-edit", aiMeta("ai edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "live", Content: "live v2"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "gone"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-ai-buried", aiMeta("ai edit buried"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "buried", Content: "buried v2 ai"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-owner-buried", ownerMeta("owner edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "buried", Content: "buried v3 owner"},
	}))

	changes, err := repo.ListUnreviewed(ctx)
	require.NoError(t, err)
	require.Len(t, changes.Blocks, 3)

	byID := make(map[string]knowledge.UnreviewedBlock, 3)
	for _, ub := range changes.Blocks {
		byID[string(ub.Head.BlockID)] = ub
	}

	require.Contains(t, byID, "live")
	live := byID["live"]
	assert.False(t, live.Head.Deleted, "live head")
	assert.Equal(t, "live v2", live.Head.Content, "live head")
	assert.Equal(t, knowledge.ID("root"), live.TopicID, "live TopicID")
	assert.Equal(t, "ai edit", live.GroupTitle, "live GroupTitle")
	assert.Equal(t, knowledge.ID("grp-edit"), live.GroupID, "live GroupID")

	require.Contains(t, byID, "gone")
	gone := byID["gone"]
	assert.True(t, gone.Head.Deleted, "gone head")
	assert.Equal(t, "gone v1", gone.Head.Content, "gone head")
	assert.Equal(t, "ai delete", gone.GroupTitle, "gone GroupTitle")
	assert.Equal(t, knowledge.ID("grp-del"), gone.GroupID, "gone GroupID")
	assert.Equal(t, knowledge.ID("root"), gone.TopicID, "gone TopicID")

	require.Contains(t, byID, "buried")
	buried := byID["buried"]
	assert.False(t, buried.Head.Deleted, "buried head")
	assert.Equal(t, "buried v3 owner", buried.Head.Content, "buried head")
	assert.Equal(t, knowledge.ID("grp-owner-buried"), buried.Head.GroupID,
		"buried Head.GroupID should be the head's own, reviewed group")
	assert.Equal(t, "ai edit buried", buried.GroupTitle, "buried GroupTitle")
	assert.Equal(t, knowledge.ID("grp-ai-buried"), buried.GroupID, "buried GroupID")
}

func TestBlocksCarryUnreviewedFlag(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "root", Content: "v1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("ai edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b", Content: "v2"},
	}))

	blocks, err := repo.GetSubtree(ctx, "root")
	require.NoError(t, err)
	var b knowledge.Block
	for _, blk := range blocks {
		if blk.ID == "b" {
			b = blk
		}
	}
	assert.True(t, b.Unreviewed, "after AI edit")

	require.NoError(t, repo.MarkBlockReviewed(ctx, "b"))
	blocks, err = repo.GetSubtree(ctx, "root")
	require.NoError(t, err)
	for _, blk := range blocks {
		if blk.ID == "b" {
			b = blk
		}
	}
	assert.False(t, b.Unreviewed, "after MarkBlockReviewed")
}

func TestRejectPageResurrectsNestedSubtree(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "c", Content: "G text"},
	}))

	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}))
	for _, id := range []string{"p", "c", "g"} {
		_, _, _, ok := blockRow(t, repo, id)
		require.False(t, ok, "%s still present after tombstoning", id)
	}

	require.NoError(t, repo.RejectPage(ctx, "p"))

	want := map[string]string{"p": "P text", "c": "C text", "g": "G text"}
	for id, content := range want {
		_, _, got, ok := blockRow(t, repo, id)
		assert.True(t, ok, "%s after RejectPage", id)
		assert.Equal(t, content, got, "%s after RejectPage", id)
	}
	hits, err := repo.Search(ctx, "text")
	require.NoError(t, err)
	assert.Len(t, hits, 3, "FTS resynced for the whole subtree")
}

func TestRejectPageMixedAddAndDelete(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "old", Parent: "topic", Content: "old text"},
		{Kind: knowledge.OpAdd, BlockID: "oldchild", Parent: "old", Content: "oldchild text"},
	}))

	require.NoError(t, repo.ApplyOps(ctx, "grp-add", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "new", Parent: "topic", Content: "new text"},
		{Kind: knowledge.OpAdd, BlockID: "newchild", Parent: "new", Content: "newchild text"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "old"},
	}))

	require.NoError(t, repo.RejectPage(ctx, "topic"))

	for _, id := range []string{"new", "newchild"} {
		_, _, _, ok := blockRow(t, repo, id)
		assert.False(t, ok, "%s still present after RejectPage, want removed (unreviewed AI add rejected)", id)
	}
	restored := map[string]string{"old": "old text", "oldchild": "oldchild text"}
	for id, content := range restored {
		_, _, got, ok := blockRow(t, repo, id)
		assert.True(t, ok, "%s after RejectPage", id)
		assert.Equal(t, content, got, "%s after RejectPage", id)
	}
}

func TestAcceptDeletedSubtreeCascades(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
	}))
	require.NoError(t, repo.MarkBlockReviewed(ctx, "p"))
	require.NoError(t, repo.MarkBlockReviewed(ctx, "c"))

	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}))
	assertInUnreviewed(t, repo, "p")
	assertInUnreviewed(t, repo, "c")

	require.NoError(t, repo.MarkBlockReviewed(ctx, "p"))

	changes, err := repo.ListUnreviewed(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes.Blocks, "after accepting the deleted subtree's root")

	for _, id := range []string{"p", "c"} {
		for _, rev := range revisionsOf(t, repo, id) {
			assert.True(t, rev.reviewed, "%s revision %+v still unreviewed after MarkBlockReviewed(p)", id, rev)
		}
	}
}

func TestAcceptDeletedSubtreeCascadesMultiLevel(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "c", Content: "G text"},
	}))
	for _, id := range []string{"p", "c", "g"} {
		require.NoError(t, repo.MarkBlockReviewed(ctx, knowledge.ID(id)), "mark %s reviewed", id)
	}

	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}))
	for _, id := range []string{"p", "c", "g"} {
		assertInUnreviewed(t, repo, id)
	}

	require.NoError(t, repo.MarkBlockReviewed(ctx, "p"))

	changes, err := repo.ListUnreviewed(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes.Blocks, "after accepting the deleted subtree's root")
	for _, id := range []string{"p", "c", "g"} {
		for _, rev := range revisionsOf(t, repo, id) {
			assert.True(t, rev.reviewed, "%s revision %+v still unreviewed after MarkBlockReviewed(p)", id, rev)
		}
	}
}

func TestAcceptDeletedSubtreeCascadeDoesNotCrossSiblings(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "p1", Parent: "topic", Content: "P1 text"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "p1", Content: "C1 text"},
		{Kind: knowledge.OpAdd, BlockID: "p2", Parent: "topic", Content: "P2 text"},
		{Kind: knowledge.OpAdd, BlockID: "c2", Parent: "p2", Content: "C2 text"},
	}))
	for _, id := range []string{"topic", "p1", "c1", "p2", "c2"} {
		require.NoError(t, repo.MarkBlockReviewed(ctx, knowledge.ID(id)), "mark %s reviewed", id)
	}

	require.NoError(t, repo.ApplyOps(ctx, "grp-del1", aiMeta("ai delete p1"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-del2", aiMeta("ai delete p2"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p2"},
	}))

	require.NoError(t, repo.MarkBlockReviewed(ctx, "p1"))

	assertNotInUnreviewed(t, repo, "p1")
	assertNotInUnreviewed(t, repo, "c1")
	assertInUnreviewed(t, repo, "p2")
	assertInUnreviewed(t, repo, "c2")

	changes, err := repo.ListUnreviewed(ctx)
	require.NoError(t, err)
	assert.Len(t, changes.Blocks, 2, "want exactly p2 and c2")
}

func TestUndoGroupTakesBackASplit(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "head", Parent: "root", Content: "head tail"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "head", Content: "head"},
		{Kind: knowledge.OpAdd, BlockID: "tail", Parent: "root", After: "head", Content: " tail"},
	}))

	require.NoError(t, repo.UndoGroup(ctx, "grp-2"))

	_, _, content, ok := blockRow(t, repo, "head")
	assert.True(t, ok)
	assert.Equal(t, "head tail", content)
	_, _, _, ok = blockRow(t, repo, "tail")
	assert.False(t, ok, "tail still present after undoing the split that created it")
	assert.Empty(t, revisionsOf(t, repo, "tail"))
	assert.Len(t, revisionsOf(t, repo, "head"), 1)
}

func TestUndoGroupResurrectsAMergedBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "target", Parent: "root", Content: "head"},
		{Kind: knowledge.OpAdd, BlockID: "source", Parent: "root", After: "target", Content: " tail"},
		{Kind: knowledge.OpAdd, BlockID: "kid", Parent: "source", Content: "kid"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "target", Content: "head tail"},
		{Kind: knowledge.OpMove, BlockID: "kid", Parent: "target"},
		{Kind: knowledge.OpDelete, BlockID: "source"},
	}))

	require.NoError(t, repo.UndoGroup(ctx, "grp-2"))

	_, _, content, ok := blockRow(t, repo, "target")
	assert.True(t, ok, "target")
	assert.Equal(t, "head", content, "target")
	_, _, content, ok = blockRow(t, repo, "source")
	assert.True(t, ok, "source")
	assert.Equal(t, " tail", content, "source")
	parent, _, _, ok := blockRow(t, repo, "kid")
	assert.True(t, ok, "kid")
	assert.Equal(t, "source", parent, "kid parent")
}

func TestUndoGroupRefusesAGroupWithNewerRevisions(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-3", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v3"},
	}))

	err := repo.UndoGroup(ctx, "grp-2")

	require.ErrorIs(t, err, knowledge.ErrGroupWrittenOver)
	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok)
	assert.Equal(t, "v3", content)
	assert.Len(t, revisionsOf(t, repo, "b1"), 3, "want all 3 intact")
}

func TestUndoGroupReportsAnUnknownGroup(t *testing.T) {
	repo := testRepo(t)

	err := repo.UndoGroup(t.Context(), "never-written")

	require.ErrorIs(t, err, knowledge.ErrNotFound)
}
