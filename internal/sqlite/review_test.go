package sqlite

import (
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func ownerMeta(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: title}
}

// modeOf reads a block's stored mode.
func modeOf(t *testing.T, repo *Repo, id string) knowledge.Mode {
	t.Helper()
	var mode knowledge.Mode
	if err := repo.db.QueryRowContext(t.Context(),
		`SELECT mode FROM blocks WHERE id = ?`, id).Scan(&mode); err != nil {
		t.Fatalf("read mode of %s: %v", id, err)
	}
	return mode
}

func TestDeleteHeadRevisionRevertsProjection(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "original"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "edited"},
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	revs := revisionsOf(t, repo, "b1")
	if len(revs) != 2 {
		t.Fatalf("revisions = %+v, want 2", revs)
	}
	editID := "rev-002"

	if err := repo.DeleteRevision(ctx, "b1", knowledge.ID(editID)); err != nil {
		t.Fatalf("DeleteRevision: %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "original" {
		t.Errorf("b1 content after delete = %q (ok=%v), want original", content, ok)
	}
	if revs := revisionsOf(t, repo, "b1"); len(revs) != 1 {
		t.Errorf("revisions after delete = %+v, want exactly 1", revs)
	}
}

func TestDeleteMiddleRevisionKeepsState(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("edit1"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}); err != nil {
		t.Fatalf("edit1: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-3", aiMeta("edit2"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v3"},
	}); err != nil {
		t.Fatalf("edit2: %v", err)
	}

	if err := repo.DeleteRevision(ctx, "b1", knowledge.ID("rev-002")); err != nil {
		t.Fatalf("DeleteRevision: %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "v3" {
		t.Errorf("b1 content after middle delete = %q (ok=%v), want v3", content, ok)
	}
	revs := revisionsOf(t, repo, "b1")
	if len(revs) != 2 {
		t.Fatalf("revisions after middle delete = %+v, want 2", revs)
	}
	if revs[len(revs)-1].content != "v3" {
		t.Errorf("surviving head content = %q, want v3", revs[len(revs)-1].content)
	}
}

func TestDeleteLastRevisionRemovesBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "leaf", Parent: "root", Content: "leaf"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := repo.DeleteRevision(ctx, "leaf", knowledge.ID("rev-002")); err != nil {
		t.Fatalf("DeleteRevision: %v", err)
	}

	if _, _, _, ok := blockRow(t, repo, "leaf"); ok {
		t.Errorf("leaf still present in blocks after deleting its only revision")
	}
	if revs := revisionsOf(t, repo, "leaf"); len(revs) != 0 {
		t.Errorf("leaf revisions = %+v, want none", revs)
	}
}

func TestDeleteHeadTombstoneResurrects(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "alive text"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "b1"},
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, _, ok := blockRow(t, repo, "b1"); ok {
		t.Fatalf("b1 still present after tombstoning")
	}

	if err := repo.DeleteRevision(ctx, "b1", knowledge.ID("rev-002")); err != nil {
		t.Fatalf("DeleteRevision(tombstone): %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "alive text" {
		t.Errorf("b1 after resurrecting delete = content=%q ok=%v, want alive text/true", content, ok)
	}
	hits, err := repo.Search(ctx, "alive")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Block.ID != "b1" {
		t.Errorf("Search(alive) after resurrection = %v, want [b1]", hits)
	}
}

func TestDeleteRevisionOrphanGuard(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "child", Parent: "root", Content: "child"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	err := repo.DeleteRevision(ctx, "root", knowledge.ID("rev-001"))
	if !errors.Is(err, knowledge.ErrInvalidRollback) {
		t.Fatalf("DeleteRevision(root's only rev) = %v, want ErrInvalidRollback", err)
	}
	if _, _, _, ok := blockRow(t, repo, "root"); !ok {
		t.Errorf("root removed from blocks despite the refused rollback")
	}
	if revs := revisionsOf(t, repo, "root"); len(revs) != 1 {
		t.Errorf("root revisions = %+v, want untouched (1)", revs)
	}
}

func TestDeleteRevisionParentGuard(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P"},
		{Kind: knowledge.OpAdd, BlockID: "x", Parent: "p", Content: "X"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("delete x"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "x"},
	}); err != nil {
		t.Fatalf("delete x: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-3", aiMeta("delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}); err != nil {
		t.Fatalf("delete p: %v", err)
	}

	err := repo.DeleteRevision(ctx, "x", knowledge.ID("rev-003"))
	if !errors.Is(err, knowledge.ErrInvalidRollback) {
		t.Fatalf("DeleteRevision(x's tombstone) = %v, want ErrInvalidRollback", err)
	}
	if _, _, _, ok := blockRow(t, repo, "x"); ok {
		t.Errorf("x resurrected in blocks despite its parent p being gone")
	}
}

func TestRejectRestoresTheModeTheBlockHad(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"}}
	if err := repo.ApplyOps(t.Context(), "g1", owner, add); err != nil {
		t.Fatalf("apply add: %v", err)
	}
	convert := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	if err := repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert); err != nil {
		t.Fatalf("apply convert: %v", err)
	}
	// Assert the conversion landed before rejecting it. Without this the test
	// passes on a build where the edit never wrote the mode at all: the row
	// would read outline throughout and the final assertion would be vacuous.
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeDoc {
		t.Fatalf("before reject mode = %q, want %q", got, knowledge.ModeDoc)
	}
	if err := repo.RejectBlock(t.Context(), "t1"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeOutline {
		t.Errorf("after reject mode = %q, want %q", got, knowledge.ModeOutline)
	}
}

func TestUndoGroupRestoresTheModeTheBlockHad(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "Topic"}}
	if err := repo.ApplyOps(t.Context(), "g1", owner, add); err != nil {
		t.Fatalf("apply add: %v", err)
	}
	convert := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	if err := repo.ApplyOps(t.Context(), "g2", owner, convert); err != nil {
		t.Fatalf("apply convert: %v", err)
	}
	// Assert the conversion landed before undoing it. Without this the test
	// passes on a build where the edit never wrote the mode at all: the row
	// would read outline throughout and the final assertion would be vacuous.
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeDoc {
		t.Fatalf("before undo mode = %q, want %q", got, knowledge.ModeDoc)
	}
	if err := repo.UndoGroup(t.Context(), "g2"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeOutline {
		t.Errorf("after undo mode = %q, want %q", got, knowledge.ModeOutline)
	}
}

func TestRejectLeavesADocumentADocument(t *testing.T) {
	repo := testRepo(t)
	owner := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "add"}
	add := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc},
	}
	if err := repo.ApplyOps(t.Context(), "g1", owner, add); err != nil {
		t.Fatalf("apply add: %v", err)
	}
	// A content-only AI edit: it names no mode, so the surviving revision is
	// the only place the mode can come back from. A projectHead that writes the
	// column's default instead of the snapshot's value silently turns the
	// document into an outline here, and nothing else in the system complains.
	edit := []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "t1", Content: "# Topic\n\nBody."}}
	if err := repo.ApplyOps(t.Context(), "g2", aiMeta("edit"), edit); err != nil {
		t.Fatalf("apply edit: %v", err)
	}
	if err := repo.RejectBlock(t.Context(), "t1"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeDoc {
		t.Errorf("after reject mode = %q, want %q", got, knowledge.ModeDoc)
	}
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
	if err := repo.ApplyOps(t.Context(), "g1", ownerMeta("build"), build); err != nil {
		t.Fatalf("apply build: %v", err)
	}
	convert := []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic\n\n- child", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpDelete, BlockID: "c1"},
	}
	if err := repo.ApplyOps(t.Context(), "g2", aiMeta("convert"), convert); err != nil {
		t.Fatalf("apply convert: %v", err)
	}
}

func TestRejectRefusesToResurrectAChildUnderADocument(t *testing.T) {
	repo := testRepo(t)
	convertedToDocument(t, repo)

	err := repo.RejectBlock(t.Context(), "c1")
	if !errors.Is(err, knowledge.ErrInvalidRollback) {
		t.Fatalf("RejectBlock = %v, want ErrInvalidRollback", err)
	}
	if _, _, _, ok := blockRow(t, repo, "c1"); ok {
		t.Error("the refused reject brought the child back anyway")
	}
	if err := repo.ApplyOps(t.Context(), "g3", ownerMeta("elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t2", Content: "Other"},
	}); err != nil {
		t.Fatalf("write after the refusal: %v", err)
	}
}

func TestRejectPageUndoesADocumentConversionWhole(t *testing.T) {
	repo := testRepo(t)
	convertedToDocument(t, repo)

	// The page holds both halves — the root's edit and the child's ghost — so
	// the projection order the child needs (its parent back to an outline
	// first) exists and projectFixpoint has to find it.
	if err := repo.RejectPage(t.Context(), "t1"); err != nil {
		t.Fatalf("RejectPage: %v", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeOutline {
		t.Errorf("after reject page mode = %q, want %q", got, knowledge.ModeOutline)
	}
	if _, _, _, ok := blockRow(t, repo, "c1"); !ok {
		t.Error("the child was not restored")
	}
}

// expandedFromDocument builds a document topic t1, then applies one legal batch
// taking it back to an outline and giving it a child — the reverse conversion,
// as an unreviewed AI batch.
func expandedFromDocument(t *testing.T, repo *Repo) {
	t.Helper()
	add := []knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "t1", Content: "# Topic", Mode: knowledge.ModeDoc}}
	if err := repo.ApplyOps(t.Context(), "g1", ownerMeta("add"), add); err != nil {
		t.Fatalf("apply add: %v", err)
	}
	expand := []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "t1", Content: "Topic", Mode: knowledge.ModeOutline},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "t1", Content: "child"},
	}
	if err := repo.ApplyOps(t.Context(), "g2", aiMeta("expand"), expand); err != nil {
		t.Fatalf("apply expand: %v", err)
	}
}

func TestRejectRefusesToRestoreADocumentOverLiveChildren(t *testing.T) {
	repo := testRepo(t)
	expandedFromDocument(t, repo)

	err := repo.RejectBlock(t.Context(), "t1")
	if !errors.Is(err, knowledge.ErrInvalidRollback) {
		t.Fatalf("RejectBlock = %v, want ErrInvalidRollback", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeOutline {
		t.Errorf("after the refused reject mode = %q, want %q", got, knowledge.ModeOutline)
	}
	if err := repo.ApplyOps(t.Context(), "g3", ownerMeta("elsewhere"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t2", Content: "Other"},
	}); err != nil {
		t.Fatalf("write after the refusal: %v", err)
	}
}

func TestRejectPageUndoesAnOutlineConversionWhole(t *testing.T) {
	repo := testRepo(t)
	expandedFromDocument(t, repo)

	if err := repo.RejectPage(t.Context(), "t1"); err != nil {
		t.Fatalf("RejectPage: %v", err)
	}
	if got := modeOf(t, repo, "t1"); got != knowledge.ModeDoc {
		t.Errorf("after reject page mode = %q, want %q", got, knowledge.ModeDoc)
	}
	if _, _, _, ok := blockRow(t, repo, "c1"); ok {
		t.Error("the child added by the rejected batch is still live")
	}
}

func TestRejectBlockDropsUnreviewedChain(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "owner text"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("edit1"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "ai edit 1"},
	}); err != nil {
		t.Fatalf("edit1: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-3", aiMeta("edit2"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "ai edit 2"},
	}); err != nil {
		t.Fatalf("edit2: %v", err)
	}

	if err := repo.RejectBlock(ctx, "b1"); err != nil {
		t.Fatalf("RejectBlock: %v", err)
	}
	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "owner text" {
		t.Errorf("b1 after RejectBlock = content=%q ok=%v, want owner text/true", content, ok)
	}
	if revs := revisionsOf(t, repo, "b1"); len(revs) != 1 {
		t.Errorf("b1 revisions after RejectBlock = %+v, want 1", revs)
	}

	if err := repo.RejectBlock(ctx, "b1"); err != nil {
		t.Fatalf("RejectBlock (no-op case): %v", err)
	}
	if revs := revisionsOf(t, repo, "b1"); len(revs) != 1 {
		t.Errorf("b1 revisions after no-op RejectBlock = %+v, want still 1", revs)
	}
}

func TestRejectBlockOnAIAddRemovesBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "leaf", Content: "ai text"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := repo.RejectBlock(ctx, "leaf"); err != nil {
		t.Fatalf("RejectBlock: %v", err)
	}
	if _, _, _, ok := blockRow(t, repo, "leaf"); ok {
		t.Errorf("leaf still present after rejecting its only (AI) revision")
	}
	if revs := revisionsOf(t, repo, "leaf"); len(revs) != 0 {
		t.Errorf("leaf revisions after RejectBlock = %+v, want none", revs)
	}
}

func TestMarkReviewedVariants(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-root", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-b", aiMeta("ai add b"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "topic", Content: "b text"},
	}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := repo.MarkBlockReviewed(ctx, "b"); err != nil {
		t.Fatalf("MarkBlockReviewed: %v", err)
	}
	assertNotInUnreviewed(t, repo, "b")

	if err := repo.ApplyOps(ctx, "grp-g", aiMeta("ai add g"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "topic", Content: "g text"},
	}); err != nil {
		t.Fatalf("add g: %v", err)
	}
	if err := repo.MarkGroupReviewed(ctx, "grp-g"); err != nil {
		t.Fatalf("MarkGroupReviewed: %v", err)
	}
	assertNotInUnreviewed(t, repo, "g")

	if err := repo.ApplyOps(ctx, "grp-c", aiMeta("ai add c"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "topic", Content: "c text"},
	}); err != nil {
		t.Fatalf("add c: %v", err)
	}
	if err := repo.MarkBlockReviewed(ctx, "c"); err != nil {
		t.Fatalf("MarkBlockReviewed(c): %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-del-c", aiMeta("ai delete c"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "c"},
	}); err != nil {
		t.Fatalf("delete c: %v", err)
	}
	assertInUnreviewed(t, repo, "c")

	if err := repo.MarkPageReviewed(ctx, "topic"); err != nil {
		t.Fatalf("MarkPageReviewed: %v", err)
	}
	assertNotInUnreviewed(t, repo, "c")
}

func assertInUnreviewed(t *testing.T, repo *Repo, id string) {
	t.Helper()
	if !blockIsUnreviewed(t, repo, id) {
		t.Errorf("block %s missing from ListUnreviewed, want present", id)
	}
}

func assertNotInUnreviewed(t *testing.T, repo *Repo, id string) {
	t.Helper()
	if blockIsUnreviewed(t, repo, id) {
		t.Errorf("block %s still in ListUnreviewed, want absent", id)
	}
}

func blockIsUnreviewed(t *testing.T, repo *Repo, id string) bool {
	t.Helper()
	changes, err := repo.ListUnreviewed(t.Context())
	if err != nil {
		t.Fatalf("ListUnreviewed: %v", err)
	}
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

	if err := repo.ApplyOps(ctx, "grp-root", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "live", Parent: "root", Content: "live v1"},
		{Kind: knowledge.OpAdd, BlockID: "gone", Parent: "root", Content: "gone v1"},
		{Kind: knowledge.OpAdd, BlockID: "buried", Parent: "root", Content: "buried v1"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, id := range []string{"live", "gone", "buried"} {
		if err := repo.MarkBlockReviewed(ctx, knowledge.ID(id)); err != nil {
			t.Fatalf("mark %s reviewed: %v", id, err)
		}
	}

	if err := repo.ApplyOps(ctx, "grp-edit", aiMeta("ai edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "live", Content: "live v2"},
	}); err != nil {
		t.Fatalf("ai edit: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "gone"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-ai-buried", aiMeta("ai edit buried"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "buried", Content: "buried v2 ai"},
	}); err != nil {
		t.Fatalf("ai edit buried: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-owner-buried", ownerMeta("owner edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "buried", Content: "buried v3 owner"},
	}); err != nil {
		t.Fatalf("owner edit buried: %v", err)
	}

	changes, err := repo.ListUnreviewed(ctx)
	if err != nil {
		t.Fatalf("ListUnreviewed: %v", err)
	}
	if len(changes.Blocks) != 3 {
		t.Fatalf("ListUnreviewed.Blocks = %+v, want 3 entries", changes.Blocks)
	}

	byID := make(map[string]knowledge.UnreviewedBlock, 3)
	for _, ub := range changes.Blocks {
		byID[string(ub.Head.BlockID)] = ub
	}

	live, ok := byID["live"]
	if !ok {
		t.Fatalf("ListUnreviewed missing 'live'")
	}
	if live.Head.Deleted || live.Head.Content != "live v2" {
		t.Errorf("live head = %+v, want Deleted=false Content=live v2", live.Head)
	}
	if live.TopicID != "root" {
		t.Errorf("live TopicID = %q, want root", live.TopicID)
	}
	if live.GroupTitle != "ai edit" {
		t.Errorf("live GroupTitle = %q, want %q", live.GroupTitle, "ai edit")
	}
	if live.GroupID != "grp-edit" {
		t.Errorf("live GroupID = %q, want %q", live.GroupID, "grp-edit")
	}

	gone, ok := byID["gone"]
	if !ok {
		t.Fatalf("ListUnreviewed missing 'gone'")
	}
	if !gone.Head.Deleted || gone.Head.Content != "gone v1" {
		t.Errorf("gone head = %+v, want Deleted=true Content=gone v1", gone.Head)
	}
	if gone.GroupTitle != "ai delete" {
		t.Errorf("gone GroupTitle = %q, want %q", gone.GroupTitle, "ai delete")
	}
	if gone.GroupID != "grp-del" {
		t.Errorf("gone GroupID = %q, want %q", gone.GroupID, "grp-del")
	}
	if gone.TopicID != "root" {
		t.Errorf("gone TopicID = %q, want root", gone.TopicID)
	}

	buried, ok := byID["buried"]
	if !ok {
		t.Fatalf("ListUnreviewed missing 'buried'")
	}
	if buried.Head.Deleted || buried.Head.Content != "buried v3 owner" {
		t.Errorf("buried head = %+v, want Deleted=false Content=buried v3 owner", buried.Head)
	}
	if buried.Head.GroupID != "grp-owner-buried" {
		t.Errorf("buried Head.GroupID = %q, want %q (the head's own, reviewed group)",
			buried.Head.GroupID, "grp-owner-buried")
	}
	if buried.GroupTitle != "ai edit buried" {
		t.Errorf("buried GroupTitle = %q, want %q", buried.GroupTitle, "ai edit buried")
	}
	if buried.GroupID != "grp-ai-buried" {
		t.Errorf("buried GroupID = %q, want %q", buried.GroupID, "grp-ai-buried")
	}
}

func TestBlocksCarryUnreviewedFlag(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "root", Content: "v1"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("ai edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b", Content: "v2"},
	}); err != nil {
		t.Fatalf("ai edit: %v", err)
	}

	blocks, err := repo.GetSubtree(ctx, "root")
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}
	var b knowledge.Block
	for _, blk := range blocks {
		if blk.ID == "b" {
			b = blk
		}
	}
	if !b.Unreviewed {
		t.Errorf("b.Unreviewed = false after AI edit, want true")
	}

	if err := repo.MarkBlockReviewed(ctx, "b"); err != nil {
		t.Fatalf("MarkBlockReviewed: %v", err)
	}
	blocks, err = repo.GetSubtree(ctx, "root")
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}
	for _, blk := range blocks {
		if blk.ID == "b" {
			b = blk
		}
	}
	if b.Unreviewed {
		t.Errorf("b.Unreviewed = true after MarkBlockReviewed, want false")
	}
}

func TestRejectPageResurrectsNestedSubtree(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "c", Content: "G text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	for _, id := range []string{"p", "c", "g"} {
		if _, _, _, ok := blockRow(t, repo, id); ok {
			t.Fatalf("%s still present after tombstoning", id)
		}
	}

	if err := repo.RejectPage(ctx, "p"); err != nil {
		t.Fatalf("RejectPage: %v", err)
	}

	want := map[string]string{"p": "P text", "c": "C text", "g": "G text"}
	for id, content := range want {
		if _, _, got, ok := blockRow(t, repo, id); !ok || got != content {
			t.Errorf("%s after RejectPage = content=%q ok=%v, want %q/true", id, got, ok, content)
		}
	}
	hits, err := repo.Search(ctx, "text")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Errorf("Search(text) after RejectPage = %v, want 3 hits (FTS resynced for the whole subtree)", hits)
	}
}

func TestRejectPageMixedAddAndDelete(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "old", Parent: "topic", Content: "old text"},
		{Kind: knowledge.OpAdd, BlockID: "oldchild", Parent: "old", Content: "oldchild text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-add", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "new", Parent: "topic", Content: "new text"},
		{Kind: knowledge.OpAdd, BlockID: "newchild", Parent: "new", Content: "newchild text"},
	}); err != nil {
		t.Fatalf("ai add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "old"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}

	if err := repo.RejectPage(ctx, "topic"); err != nil {
		t.Fatalf("RejectPage: %v", err)
	}

	for _, id := range []string{"new", "newchild"} {
		if _, _, _, ok := blockRow(t, repo, id); ok {
			t.Errorf("%s still present after RejectPage, want removed (unreviewed AI add rejected)", id)
		}
	}
	restored := map[string]string{"old": "old text", "oldchild": "oldchild text"}
	for id, content := range restored {
		if _, _, got, ok := blockRow(t, repo, id); !ok || got != content {
			t.Errorf("%s after RejectPage = content=%q ok=%v, want %q/true", id, got, ok, content)
		}
	}
}

func TestAcceptDeletedSubtreeCascades(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := repo.MarkBlockReviewed(ctx, "p"); err != nil {
		t.Fatalf("mark p reviewed: %v", err)
	}
	if err := repo.MarkBlockReviewed(ctx, "c"); err != nil {
		t.Fatalf("mark c reviewed: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	assertInUnreviewed(t, repo, "p")
	assertInUnreviewed(t, repo, "c")

	if err := repo.MarkBlockReviewed(ctx, "p"); err != nil {
		t.Fatalf("MarkBlockReviewed(p): %v", err)
	}

	changes, err := repo.ListUnreviewed(ctx)
	if err != nil {
		t.Fatalf("ListUnreviewed: %v", err)
	}
	if len(changes.Blocks) != 0 {
		t.Errorf("ListUnreviewed.Blocks after accepting the deleted subtree's root = %+v, want none", changes.Blocks)
	}

	for _, id := range []string{"p", "c"} {
		for _, rev := range revisionsOf(t, repo, id) {
			if !rev.reviewed {
				t.Errorf("%s revision %+v still unreviewed after MarkBlockReviewed(p)", id, rev)
			}
		}
	}
}

func TestAcceptDeletedSubtreeCascadesMultiLevel(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "p", Content: "P text"},
		{Kind: knowledge.OpAdd, BlockID: "c", Parent: "p", Content: "C text"},
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "c", Content: "G text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, id := range []string{"p", "c", "g"} {
		if err := repo.MarkBlockReviewed(ctx, knowledge.ID(id)); err != nil {
			t.Fatalf("mark %s reviewed: %v", id, err)
		}
	}

	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("ai delete p"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	for _, id := range []string{"p", "c", "g"} {
		assertInUnreviewed(t, repo, id)
	}

	if err := repo.MarkBlockReviewed(ctx, "p"); err != nil {
		t.Fatalf("MarkBlockReviewed(p): %v", err)
	}

	changes, err := repo.ListUnreviewed(ctx)
	if err != nil {
		t.Fatalf("ListUnreviewed: %v", err)
	}
	if len(changes.Blocks) != 0 {
		t.Errorf("ListUnreviewed.Blocks after accepting the deleted subtree's root = %+v, want none", changes.Blocks)
	}
	for _, id := range []string{"p", "c", "g"} {
		for _, rev := range revisionsOf(t, repo, id) {
			if !rev.reviewed {
				t.Errorf("%s revision %+v still unreviewed after MarkBlockReviewed(p)", id, rev)
			}
		}
	}
}

func TestAcceptDeletedSubtreeCascadeDoesNotCrossSiblings(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "topic", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "p1", Parent: "topic", Content: "P1 text"},
		{Kind: knowledge.OpAdd, BlockID: "c1", Parent: "p1", Content: "C1 text"},
		{Kind: knowledge.OpAdd, BlockID: "p2", Parent: "topic", Content: "P2 text"},
		{Kind: knowledge.OpAdd, BlockID: "c2", Parent: "p2", Content: "C2 text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, id := range []string{"topic", "p1", "c1", "p2", "c2"} {
		if err := repo.MarkBlockReviewed(ctx, knowledge.ID(id)); err != nil {
			t.Fatalf("mark %s reviewed: %v", id, err)
		}
	}

	if err := repo.ApplyOps(ctx, "grp-del1", aiMeta("ai delete p1"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p1"},
	}); err != nil {
		t.Fatalf("ai delete p1: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-del2", aiMeta("ai delete p2"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "p2"},
	}); err != nil {
		t.Fatalf("ai delete p2: %v", err)
	}

	if err := repo.MarkBlockReviewed(ctx, "p1"); err != nil {
		t.Fatalf("MarkBlockReviewed(p1): %v", err)
	}

	assertNotInUnreviewed(t, repo, "p1")
	assertNotInUnreviewed(t, repo, "c1")
	assertInUnreviewed(t, repo, "p2")
	assertInUnreviewed(t, repo, "c2")

	changes, err := repo.ListUnreviewed(ctx)
	if err != nil {
		t.Fatalf("ListUnreviewed: %v", err)
	}
	if len(changes.Blocks) != 2 {
		t.Errorf("ListUnreviewed.Blocks after accepting p1 only = %+v, want exactly p2 and c2", changes.Blocks)
	}
}

func TestUndoGroupTakesBackASplit(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "head", Parent: "root", Content: "head tail"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "head", Content: "head"},
		{Kind: knowledge.OpAdd, BlockID: "tail", Parent: "root", After: "head", Content: " tail"},
	}); err != nil {
		t.Fatalf("split: %v", err)
	}

	if err := repo.UndoGroup(ctx, "grp-2"); err != nil {
		t.Fatalf("UndoGroup: %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "head"); !ok || content != "head tail" {
		t.Errorf("head after undo = content=%q ok=%v, want %q/true", content, ok, "head tail")
	}
	if _, _, _, ok := blockRow(t, repo, "tail"); ok {
		t.Errorf("tail still present after undoing the split that created it")
	}
	if revs := revisionsOf(t, repo, "tail"); len(revs) != 0 {
		t.Errorf("tail revisions after undo = %+v, want none", revs)
	}
	if revs := revisionsOf(t, repo, "head"); len(revs) != 1 {
		t.Errorf("head revisions after undo = %+v, want 1", revs)
	}
}

func TestUndoGroupResurrectsAMergedBlock(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "topic"},
		{Kind: knowledge.OpAdd, BlockID: "target", Parent: "root", Content: "head"},
		{Kind: knowledge.OpAdd, BlockID: "source", Parent: "root", After: "target", Content: " tail"},
		{Kind: knowledge.OpAdd, BlockID: "kid", Parent: "source", Content: "kid"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "target", Content: "head tail"},
		{Kind: knowledge.OpMove, BlockID: "kid", Parent: "target"},
		{Kind: knowledge.OpDelete, BlockID: "source"},
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if err := repo.UndoGroup(ctx, "grp-2"); err != nil {
		t.Fatalf("UndoGroup: %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "target"); !ok || content != "head" {
		t.Errorf("target after undo = content=%q ok=%v, want head/true", content, ok)
	}
	if _, _, content, ok := blockRow(t, repo, "source"); !ok || content != " tail" {
		t.Errorf("source after undo = content=%q ok=%v, want ' tail'/true", content, ok)
	}
	if parent, _, _, ok := blockRow(t, repo, "kid"); !ok || parent != "source" {
		t.Errorf("kid after undo = parent=%q ok=%v, want source/true", parent, ok)
	}
}

func TestUndoGroupRefusesAGroupWithNewerRevisions(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", ownerMeta("write"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-3", ownerMeta("manual edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v3"},
	}); err != nil {
		t.Fatalf("later edit: %v", err)
	}

	err := repo.UndoGroup(ctx, "grp-2")

	if !errors.Is(err, knowledge.ErrGroupWrittenOver) {
		t.Fatalf("UndoGroup of a buried group = %v, want ErrGroupWrittenOver", err)
	}
	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "v3" {
		t.Errorf("b1 after refused undo = content=%q ok=%v, want v3/true", content, ok)
	}
	if revs := revisionsOf(t, repo, "b1"); len(revs) != 3 {
		t.Errorf("b1 revisions after refused undo = %+v, want all 3 intact", revs)
	}
}

func TestUndoGroupReportsAnUnknownGroup(t *testing.T) {
	repo := testRepo(t)

	err := repo.UndoGroup(t.Context(), "never-written")

	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("UndoGroup of an unknown group = %v, want ErrNotFound", err)
	}
}
