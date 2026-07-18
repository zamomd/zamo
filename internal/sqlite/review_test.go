package sqlite

import (
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// ownerMeta is the write-batch provenance an owner-authored ApplyOps call
// carries: born-reviewed, unlike aiMeta (apply_test.go).
func ownerMeta(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: title}
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

	// seq 2 is "rev-002"; deleting it must not touch the current head (seq 3).
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

	// The tombstone is the head (seq 2, rev-002); deleting it must resurrect
	// the block with its prior content.
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
	if len(hits) != 1 || hits[0].ID != "b1" {
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

	// root's only revision is rev-001; deleting it would remove root from the
	// projection while child still points at it.
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

	// P and X both exist; X is then tombstoned (deleted) while still
	// unreviewed, then P is tombstoned too. Deleting X's head tombstone would
	// resurrect X, but X's recorded parent P no longer exists.
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

	// IDs are minted in ApplyOps call order: rev-001 (add p), rev-002 (add
	// x), rev-003 (delete x — x's head tombstone), rev-004 (delete p).
	err := repo.DeleteRevision(ctx, "x", knowledge.ID("rev-003"))
	if !errors.Is(err, knowledge.ErrInvalidRollback) {
		t.Fatalf("DeleteRevision(x's tombstone) = %v, want ErrInvalidRollback", err)
	}
	if _, _, _, ok := blockRow(t, repo, "x"); ok {
		t.Errorf("x resurrected in blocks despite its parent p being gone")
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

	// A fully reviewed block is a no-op.
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

	// Block variant.
	if err := repo.ApplyOps(ctx, "grp-b", aiMeta("ai add b"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "topic", Content: "b text"},
	}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := repo.MarkBlockReviewed(ctx, "b"); err != nil {
		t.Fatalf("MarkBlockReviewed: %v", err)
	}
	assertNotInUnreviewed(t, repo, "b")

	// Group variant.
	if err := repo.ApplyOps(ctx, "grp-g", aiMeta("ai add g"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "g", Parent: "topic", Content: "g text"},
	}); err != nil {
		t.Fatalf("add g: %v", err)
	}
	if err := repo.MarkGroupReviewed(ctx, "grp-g"); err != nil {
		t.Fatalf("MarkGroupReviewed: %v", err)
	}
	assertNotInUnreviewed(t, repo, "g")

	// Page variant, covering a ghost: AI-delete a child under topic, then
	// MarkPageReviewed(topic) must mark the tombstone too.
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

// assertInUnreviewed / assertNotInUnreviewed check ListUnreviewed for a
// given block id, for tests that only care about presence.
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
	// buried: an AI edit lands unreviewed, then the owner edits straight on
	// top of it (owner writes are born reviewed and bypass the queue). The
	// head is now the owner's revision, but the AI revision underneath is
	// still unreviewed — GroupID/GroupTitle must come from that AI revision
	// (the "newest unreviewed" one ListUnreviewed's query targets), not from
	// the head's own (reviewed, unrelated) group.
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
	// The newest *unreviewed* revision's group, matching GroupTitle — must
	// differ from Head.GroupID (asserted above).
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

// TestRejectPageResurrectsNestedSubtree pins the fixpoint order projectHead
// needs for a multi-level AI delete: a naive single deepest-first pass
// processes the grandchild before its parent is live again and trips the
// restore-parent guard. RejectPage must retry until every block in the
// tombstoned subtree is back.
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

// TestRejectPageMixedAddAndDelete covers the case a single global order
// cannot satisfy: one page with both an unreviewed AI add (needs its
// children rejected before its root — bottom-up) and an unreviewed AI
// delete of a reviewed subtree (needs its root resurrected before its
// children — top-down). One RejectPage call must resolve both.
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

// TestAcceptDeletedSubtreeCascades pins that accepting a deleted subtree's
// root also resolves each descendant's own tombstone: without the cascade,
// accepting P alone leaves C's ghost with no reachable parent (P's ghost
// just disappeared) — invisible to the frontend's buildTree (which merges
// ghosts in by parent) yet still counted as pending, and unrejectable
// (RejectBlock(C) would refuse: "restore parent first").
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

// TestAcceptDeletedSubtreeCascadesMultiLevel pins that the cascade is not
// just one generation deep: P→C→G all tombstoned by one AI delete of P,
// MarkBlockReviewed(P) must walk the chain and resolve every level, not
// only C.
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

// TestAcceptDeletedSubtreeCascadeDoesNotCrossSiblings pins that the cascade
// is scoped to the accepted block's own chain: two unrelated AI-deleted
// subtrees under the same topic, and accepting one root must not touch the
// other's tombstones.
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

// TestMarkGroupReviewedMarksEdges pins that accepting a whole ApplyOps
// batch does not leave a link op it contained pending: applyLink stamps
// the edge with the batch's group_id, and MarkGroupReviewed must flip its
// reviewed flag along with the batch's revisions.
func TestMarkGroupReviewedMarksEdges(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-setup", ownerMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "from", Content: "theory"},
		{Kind: knowledge.OpAdd, BlockID: "to", Content: "example"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-link", aiMeta("ai link"), []knowledge.Op{
		{Kind: knowledge.OpLink, From: "from", To: "to", Relation: knowledge.RelationExample},
	}); err != nil {
		t.Fatalf("ai link: %v", err)
	}

	var reviewed bool
	if err := repo.db.QueryRowContext(ctx,
		`SELECT reviewed FROM edges WHERE from_id = 'from' AND to_id = 'to'`).Scan(&reviewed); err != nil {
		t.Fatalf("read edge: %v", err)
	}
	if reviewed {
		t.Fatalf("edge born reviewed, want unreviewed before MarkGroupReviewed")
	}

	if err := repo.MarkGroupReviewed(ctx, "grp-link"); err != nil {
		t.Fatalf("MarkGroupReviewed: %v", err)
	}

	if err := repo.db.QueryRowContext(ctx,
		`SELECT reviewed FROM edges WHERE from_id = 'from' AND to_id = 'to'`).Scan(&reviewed); err != nil {
		t.Fatalf("read edge after MarkGroupReviewed: %v", err)
	}
	if !reviewed {
		t.Errorf("edge reviewed = false after MarkGroupReviewed, want true")
	}
}
