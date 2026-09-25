package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cominddev/comind/internal/knowledge"
)

func testRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	seq := 0
	return NewRepo(db, func() time.Time { return time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC) },
		func() knowledge.ID { seq++; return knowledge.ID(fmt.Sprintf("rev-%03d", seq)) })
}

// testService wires the domain to repo with a counting id generator, for the
// tests whose subject is a use case rather than a single storage call.
func testService(repo *Repo) *knowledge.Service {
	seq := 0
	return knowledge.NewService(nil, repo, func() knowledge.ID {
		seq++
		return knowledge.ID(fmt.Sprintf("blk-%03d", seq))
	})
}

func aiMeta(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: title}
}

func blockRow(t *testing.T, repo *Repo, id string) (parent string, position int, content string, ok bool) {
	t.Helper()
	err := repo.db.QueryRowContext(t.Context(),
		`SELECT COALESCE(parent_id, ''), position, content FROM blocks WHERE id = ?`, id).Scan(&parent, &position, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, "", false
	}
	require.NoError(t, err, "read block %s", id)
	return parent, position, content, true
}

type revRow struct {
	seq      int
	content  string
	parent   string
	position int
	deleted  bool
	author   string
	reviewed bool
	group    string
}

func revisionsOf(t *testing.T, repo *Repo, blockID string) []revRow {
	t.Helper()
	rows, err := repo.db.QueryContext(t.Context(),
		`SELECT seq, content, COALESCE(parent_id, ''), position, deleted, author, reviewed, COALESCE(group_id, '')
		 FROM block_revisions WHERE block_id = ? ORDER BY seq`, blockID)
	require.NoError(t, err, "query revisions of %s", blockID)
	defer rows.Close()
	var out []revRow
	for rows.Next() {
		var r revRow
		require.NoError(t, rows.Scan(&r.seq, &r.content, &r.parent, &r.position,
			&r.deleted, &r.author, &r.reviewed, &r.group), "scan revision of %s", blockID)
		out = append(out, r)
	}
	require.NoError(t, rows.Err(), "iterate revisions of %s", blockID)
	return out
}

func countRows(t *testing.T, repo *Repo, table string) int {
	t.Helper()
	var n int
	require.NoError(t, repo.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n), "count %s", table)
	return n
}

func TestApplyOpsAddWritesRevisionAndProjection(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b-root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "b-child", Parent: "b-root", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(ctx, "grp-1", aiMeta("Add root+child"), ops))

	parent, pos, content, ok := blockRow(t, repo, "b-root")
	assert.True(t, ok, "b-root missing")
	assert.Empty(t, parent, "b-root parent")
	assert.Zero(t, pos, "b-root position")
	assert.Equal(t, "root", content)
	parent, pos, content, ok = blockRow(t, repo, "b-child")
	assert.True(t, ok, "b-child missing")
	assert.Equal(t, "b-root", parent)
	assert.Zero(t, pos, "b-child position")
	assert.Equal(t, "child", content)

	for _, id := range []string{"b-root", "b-child"} {
		revs := revisionsOf(t, repo, id)
		require.Len(t, revs, 1, "revisions of %s", id)
		assert.Equal(t, 1, revs[0].seq, "revision of %s", id)
		assert.False(t, revs[0].reviewed, "revision of %s", id)
		assert.Equal(t, "grp-1", revs[0].group, "revision of %s", id)
	}

	var title string
	require.NoError(t, repo.db.QueryRowContext(ctx,
		`SELECT title FROM revision_groups WHERE id = 'grp-1'`).Scan(&title))
	assert.Equal(t, "Add root+child", title)
}

func TestApplyOpsEditAppendsRevision(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}))
	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}))

	_, _, content, ok := blockRow(t, repo, "b1")
	assert.True(t, ok, "b1 missing")
	assert.Equal(t, "v2", content)

	revs := revisionsOf(t, repo, "b1")
	require.Len(t, revs, 2)
	assert.Equal(t, 1, revs[0].seq)
	assert.Equal(t, 2, revs[1].seq)
	assert.Equal(t, revs[0].parent, revs[1].parent, "seq 2 parent changed")
	assert.Equal(t, revs[0].position, revs[1].position, "seq 2 position changed")
	assert.Equal(t, "v2", revs[1].content)
}

func TestApplyOpsMoveKeepsContent(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root", Content: "Mid"},
		{Kind: knowledge.OpAdd, BlockID: "leaf", Parent: "mid", Content: "Leaf"},
		{Kind: knowledge.OpAdd, BlockID: "other", Content: "Other"},
	}))

	require.NoError(t, repo.ApplyOps(ctx, "grp-2", aiMeta("move"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "other", Parent: "root"},
	}))
	parent, pos, content, ok := blockRow(t, repo, "other")
	assert.True(t, ok, "other missing after move")
	assert.Equal(t, "root", parent)
	assert.Equal(t, positionStride, pos)
	assert.Equal(t, "Other", content)
	revs := revisionsOf(t, repo, "other")
	require.Len(t, revs, 2)
	assert.Equal(t, "Other", revs[1].content)
	assert.Equal(t, "root", revs[1].parent)
	assert.Equal(t, positionStride, revs[1].position)

	err := repo.ApplyOps(ctx, "grp-3", aiMeta("cycle"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "root", Parent: "leaf"},
	})
	require.ErrorIs(t, err, knowledge.ErrCycle, "move into own subtree")
	parent, _, _, _ = blockRow(t, repo, "root")
	assert.Empty(t, parent, "root reparented despite the cycle")
	assert.Len(t, revisionsOf(t, repo, "root"), 1, "root gained a revision despite the rejected cycle move")
	var groups int
	require.NoError(t, repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM revision_groups WHERE id = 'grp-3'`).Scan(&groups))
	assert.Zero(t, groups, "grp-3 revision_groups row survived the rolled-back cycle move")
}

func TestApplyOpsDeleteTombstonesSubtree(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	require.NoError(t, repo.ApplyOps(ctx, "grp-1", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root text"},
		{Kind: knowledge.OpAdd, BlockID: "a", Parent: "root", Content: "A text"},
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "a", Content: "B text"},
	}))

	require.NoError(t, repo.ApplyOps(ctx, "grp-del", aiMeta("delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "root"},
	}))

	for _, id := range []string{"root", "a", "b"} {
		_, _, _, ok := blockRow(t, repo, id)
		assert.False(t, ok, "%s still present in blocks after delete", id)
		revs := revisionsOf(t, repo, id)
		require.NotEmpty(t, revs, "%s has no revisions after delete", id)
		head := revs[len(revs)-1]
		assert.True(t, head.deleted, "%s head revision not deleted", id)
		assert.Equal(t, "grp-del", head.group, "%s head revision", id)
	}
	rootRevs := revisionsOf(t, repo, "root")
	assert.Equal(t, "Root text", rootRevs[len(rootRevs)-1].content, "root tombstone content")

	for _, q := range []string{"Root", "text"} {
		hits, err := repo.Search(ctx, q)
		require.NoError(t, err, "Search(%q)", q)
		assert.Empty(t, hits, "Search(%q) after delete", q)
	}
}

func TestApplyOpsOwnerRevisionsBornReviewed(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	meta := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "owner edit"}
	require.NoError(t, repo.ApplyOps(ctx, "grp-1", meta, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "owner-written"},
	}))

	revs := revisionsOf(t, repo, "b1")
	require.Len(t, revs, 1)
	assert.True(t, revs[0].reviewed, "owner revision not reviewed")
}

func TestApplyOpsMissingTargetIsNotFound(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	err := repo.ApplyOps(ctx, "grp-1", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "nope", Content: "x"},
	})
	require.ErrorIs(t, err, knowledge.ErrNotFound)
}

func TestApplyAddCarriesMode(t *testing.T) {
	repo := testRepo(t)
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "doc-1", Content: "# Go Contexts\n\nBody.", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpAdd, BlockID: "out-1", Content: "Go Idioms"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("two topics"), ops))

	blocks, err := repo.GetBlocks(t.Context(), []knowledge.ID{"doc-1", "out-1"})
	require.NoError(t, err)
	assert.Equal(t, knowledge.ModeDoc, blocks["doc-1"].Mode, "doc-1 mode")
	assert.Equal(t, knowledge.ModeOutline, blocks["out-1"].Mode, "out-1 mode")

	revs, err := repo.ListRevisions(t.Context(), "doc-1")
	require.NoError(t, err)
	assert.Equal(t, knowledge.ModeDoc, revs[0].Mode, "revision mode")
}

func TestApplyEditKeepsModeUnlessAsked(t *testing.T) {
	repo := testRepo(t)
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Doc", Mode: knowledge.ModeDoc}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("add"), add))

	edit := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Doc\n\nMore."}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("edit"), edit))
	blocks, err := repo.GetBlocks(t.Context(), []knowledge.ID{"t1"})
	require.NoError(t, err)
	require.Equal(t, knowledge.ModeDoc, blocks["t1"].Mode, "after a plain edit")

	convert := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Doc", Mode: knowledge.ModeOutline}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g3", aiMeta("convert"), convert))
	blocks, err = repo.GetBlocks(t.Context(), []knowledge.ID{"t1"})
	require.NoError(t, err)
	assert.Equal(t, knowledge.ModeOutline, blocks["t1"].Mode, "after an edit asking for outline")
}

func TestApplyRefusesAModeOnAnEditOfAChild(t *testing.T) {
	repo := testRepo(t)
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("build"), build))
	err := repo.ApplyOps(t.Context(), "g2", aiMeta("bad"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "c1", Content: "changed", Mode: knowledge.ModeDoc},
	})
	require.ErrorIs(t, err, knowledge.ErrInvalidOps)
	_, _, content, _ := blockRow(t, repo, "c1")
	assert.Equal(t, "child", content, "the refused edit landed anyway")
}

func TestApplyMoveUnderAParentDropsDocMode(t *testing.T) {
	repo := testRepo(t)
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "host", Content: "Host"},
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Doc", Mode: knowledge.ModeDoc},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("add"), ops))
	move := []knowledge.Op{{Kind: knowledge.OpMove, BlockID: "t1", Parent: "host"}}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("move"), move))
	blocks, err := repo.GetBlocks(t.Context(), []knowledge.ID{"t1"})
	require.NoError(t, err)
	assert.Equal(t, knowledge.ModeOutline, blocks["t1"].Mode, "moved under a parent")
}

func TestApplyOpsAtomic(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	err := repo.ApplyOps(ctx, "grp-1", aiMeta("mixed"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "new", Content: "half-applied"},
		{Kind: knowledge.OpEdit, BlockID: "nope", Content: "never lands"},
	})
	require.ErrorIs(t, err, knowledge.ErrNotFound, "batch with a stale op")

	assert.Zero(t, countRows(t, repo, "blocks"), "blocks rows after a rolled-back apply")
	assert.Zero(t, countRows(t, repo, "block_revisions"), "block_revisions rows after a rolled-back apply")
	assert.Zero(t, countRows(t, repo, "revision_groups"), "revision_groups rows after a rolled-back apply")
}

func TestApplyRefusesAChildUnderADocument(t *testing.T) {
	repo := testRepo(t)
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Doc", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	err := repo.ApplyOps(t.Context(), "g1", aiMeta("bad"), ops)
	require.ErrorIs(t, err, knowledge.ErrDocumentHasChildren)
	_, _, _, ok := blockRow(t, repo, "t1")
	assert.False(t, ok, "the refused batch left a block behind")
}

func TestApplyRefusesADocumentOverAnUntouchedChild(t *testing.T) {
	repo := testRepo(t)
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("build"), build))
	err := repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic", Mode: knowledge.ModeDoc},
	})
	require.ErrorIs(t, err, knowledge.ErrDocumentHasChildren)
	assert.Equal(t, knowledge.ModeOutline, modeOf(t, repo, "t1"), "after the refused batch")
}

func TestApplyAllowsConvertingAwayTheChildrenInOneBatch(t *testing.T) {
	repo := testRepo(t)
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("build"), build))
	convert := []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic\n\n- child", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpDelete, BlockID: "c1"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert))
}

func TestApplyAllowsTheConversionWithTheDeleteFirst(t *testing.T) {
	repo := testRepo(t)
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("build"), build))
	convert := []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "c1"},
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic\n\n- child", Mode: knowledge.ModeDoc},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert))
}

func TestApplyChecksOnlyTheBatchForDocumentChildren(t *testing.T) {
	repo := testRepo(t)
	build := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	require.NoError(t, repo.ApplyOps(t.Context(), "g1", aiMeta("build"), build))
	_, err := repo.db.ExecContext(t.Context(), `UPDATE blocks SET mode = 'doc' WHERE id = ?`, "t1")
	require.NoError(t, err, "plant the inconsistency")

	require.NoError(t, repo.ApplyOps(t.Context(), "g2", aiMeta("elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t2", Content: "Other"},
	}))
	err = repo.ApplyOps(t.Context(), "g3", aiMeta("touch the child"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "c1", Content: "changed"},
	})
	require.ErrorIs(t, err, knowledge.ErrDocumentHasChildren, "write touching the child")
}

// convertedTopic builds an outline topic with one child through the service and
// converts it to a document, returning the topic's id and the conversion's
// group. Everything before the conversion is settled, so whatever is awaiting
// review afterwards is the conversion alone.
func convertedTopic(t *testing.T, svc *knowledge.Service) (knowledge.ID, knowledge.ID) {
	t.Helper()
	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", "")
	require.NoError(t, err)
	_, err = svc.ApplyOps(t.Context(), ownerMeta("fill"), []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "a", Parent: string(topicID), Content: "First."},
	})
	require.NoError(t, err, "fill")
	applied, err := svc.SetTopicMode(t.Context(), topicID, knowledge.ModeDoc)
	require.NoError(t, err)
	return topicID, applied.GroupID
}

func TestConversionWaitsForReviewAsOneUnit(t *testing.T) {
	repo := testRepo(t)
	svc := testService(repo)
	topicID, groupID := convertedTopic(t, svc)

	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	require.Len(t, pending.Blocks, 2, "want the root and its child awaiting review")
	for _, b := range pending.Blocks {
		assert.Equal(t, groupID, b.GroupID, "block %s should wait in the conversion's group", b.Head.BlockID)
		assert.True(t, b.Atomic, "block %s is decidable on its own, so the conversion is not one unit", b.Head.BlockID)
		assert.Equal(t, knowledge.AuthorOwner, b.Head.Author, "block %s author", b.Head.BlockID)
	}
	assert.ErrorIs(t, svc.RejectBlock(t.Context(), topicID), knowledge.ErrAtomicGroup, "rejecting the root alone")
}

func TestConversionIsRevertedWholeByItsGroup(t *testing.T) {
	repo := testRepo(t)
	svc := testService(repo)
	topicID, groupID := convertedTopic(t, svc)

	// UndoGroup is what the Changes screen's "Reject batch" calls.
	require.NoError(t, svc.UndoGroup(t.Context(), groupID))
	sub, err := svc.GetSubtree(t.Context(), topicID)
	require.NoError(t, err)
	require.Len(t, sub, 2, "want the root and its child")
	assert.Equal(t, knowledge.ModeOutline, sub[0].Mode, "root mode")
	assert.Equal(t, "First.", sub[1].Content, "child content")
	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pending.Blocks, "blocks still awaiting review after the revert")
}

func TestAnEditOverAConversionClosesItsWholeBatchRevert(t *testing.T) {
	repo := testRepo(t)
	svc := testService(repo)
	topicID, groupID := convertedTopic(t, svc)

	require.NoError(t, svc.EditBlockContent(t.Context(), topicID, "Go Contexts\n\nTyped."))

	require.ErrorIs(t, svc.UndoGroup(t.Context(), groupID), knowledge.ErrGroupWrittenOver)
	require.ErrorIs(t, svc.RejectPage(t.Context(), topicID), knowledge.ErrInvalidRollback)

	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	var seen bool
	for _, b := range pending.Blocks {
		if b.Head.BlockID != topicID {
			continue
		}
		seen = true
		assert.NotEqual(t, b.GroupID, b.Head.GroupID,
			"head group equals the pending group, so nothing tells the GUI the batch was written over")
	}
	assert.True(t, seen, "the root left the review queue, so the conversion is no longer pending at all")
}

func TestOwnerEditsStayOutOfTheReviewQueue(t *testing.T) {
	repo := testRepo(t)
	svc := testService(repo)
	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", "")
	require.NoError(t, err)
	require.NoError(t, svc.EditBlockContent(t.Context(), topicID, "Go Contexts, revisited"))
	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pending.Blocks, "ordinary typing is not reviewed")
}

func TestConversionRoundTripThroughStorage(t *testing.T) {
	repo := testRepo(t)
	svc := testService(repo)

	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", "")
	require.NoError(t, err)
	_, err = svc.ApplyOps(t.Context(), aiMeta("fill"), []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "a", Parent: string(topicID), Content: "First.\nStill first."},
		{Kind: knowledge.OpAdd, TempID: "b", Parent: "a", Content: "Nested."},
	})
	require.NoError(t, err, "fill")
	before, err := svc.GetSubtree(t.Context(), topicID)
	require.NoError(t, err)

	_, err = svc.SetTopicMode(t.Context(), topicID, knowledge.ModeDoc)
	require.NoError(t, err, "to doc")
	_, err = svc.SetTopicMode(t.Context(), topicID, knowledge.ModeOutline)
	require.NoError(t, err, "back to outline")
	after, err := svc.GetSubtree(t.Context(), topicID)
	require.NoError(t, err)
	require.Len(t, after, len(before), "blocks after the round trip")
	for i := range before {
		assert.Equal(t, before[i].Content, after[i].Content, "block %d content", i)
		assert.Equal(t, parentIndex(before, i), parentIndex(after, i), "block %d parent index", i)
	}
}

// parentIndex reports where sub[i]'s parent sits in the same pre-ordered walk,
// or -1 for the root.
func parentIndex(sub []knowledge.Block, i int) int {
	return slices.IndexFunc(sub, func(b knowledge.Block) bool { return b.ID == sub[i].ParentID })
}
