package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// testRepo opens a scratch repo whose clock and revision ids are
// deterministic, so assertions can pin exact values instead of just shapes.
func testRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	seq := 0
	return NewRepo(db, func() time.Time { return time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC) },
		func() knowledge.ID { seq++; return knowledge.ID(fmt.Sprintf("rev-%03d", seq)) })
}

// aiMeta is the write-batch provenance an AI-authored ApplyOps call carries.
func aiMeta(title string) knowledge.WriteMeta {
	return knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: title}
}

// blockRow reads one blocks row for assertions; ok is false if it is gone.
func blockRow(t *testing.T, repo *Repo, id string) (parent string, position int, content string, ok bool) {
	t.Helper()
	err := repo.db.QueryRowContext(t.Context(),
		`SELECT COALESCE(parent_id, ''), position, content FROM blocks WHERE id = ?`, id).Scan(&parent, &position, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, "", false
	}
	if err != nil {
		t.Fatalf("read block %s: %v", id, err)
	}
	return parent, position, content, true
}

// revRow is one block_revisions row, flattened for assertions.
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

// revisionsOf reads every revision of a block, oldest (seq 1) first.
func revisionsOf(t *testing.T, repo *Repo, blockID string) []revRow {
	t.Helper()
	rows, err := repo.db.QueryContext(t.Context(),
		`SELECT seq, content, COALESCE(parent_id, ''), position, deleted, author, reviewed, COALESCE(group_id, '')
		 FROM block_revisions WHERE block_id = ? ORDER BY seq`, blockID)
	if err != nil {
		t.Fatalf("query revisions of %s: %v", blockID, err)
	}
	defer rows.Close()
	var out []revRow
	for rows.Next() {
		var r revRow
		if err := rows.Scan(&r.seq, &r.content, &r.parent, &r.position,
			&r.deleted, &r.author, &r.reviewed, &r.group); err != nil {
			t.Fatalf("scan revision of %s: %v", blockID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate revisions of %s: %v", blockID, err)
	}
	return out
}

// countRows returns the number of rows in table, for atomicity assertions.
func countRows(t *testing.T, repo *Repo, table string) int {
	t.Helper()
	var n int
	if err := repo.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestApplyOpsAddWritesRevisionAndProjection(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	// Already-resolved ids, parent-first ordered, as prepareOps emits.
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b-root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "b-child", Parent: "b-root", Content: "child"},
	}
	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("Add root+child"), ops); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}

	if parent, pos, content, ok := blockRow(t, repo, "b-root"); !ok || parent != "" || pos != 0 || content != "root" {
		t.Errorf("b-root = parent=%q pos=%d content=%q ok=%v, want ''/0/root/true", parent, pos, content, ok)
	}
	parent, pos, content, ok := blockRow(t, repo, "b-child")
	if !ok || parent != "b-root" || pos != 0 || content != "child" {
		t.Errorf("b-child = parent=%q pos=%d content=%q ok=%v, want b-root/0/child/true", parent, pos, content, ok)
	}

	for _, id := range []string{"b-root", "b-child"} {
		revs := revisionsOf(t, repo, id)
		if len(revs) != 1 {
			t.Fatalf("revisions of %s = %+v, want exactly one", id, revs)
		}
		if revs[0].seq != 1 || revs[0].reviewed || revs[0].group != "grp-1" {
			t.Errorf("revision of %s = %+v, want seq=1 reviewed=false group=grp-1", id, revs[0])
		}
	}

	var title string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT title FROM revision_groups WHERE id = 'grp-1'`).Scan(&title); err != nil {
		t.Fatalf("read revision_groups: %v", err)
	}
	if title != "Add root+child" {
		t.Errorf("revision_groups title = %q, want %q", title, "Add root+child")
	}
}

func TestApplyOpsEditAppendsRevision(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "v1"},
	}); err != nil {
		t.Fatalf("ApplyOps add: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "b1", Content: "v2"},
	}); err != nil {
		t.Fatalf("ApplyOps edit: %v", err)
	}

	if _, _, content, ok := blockRow(t, repo, "b1"); !ok || content != "v2" {
		t.Errorf("b1 content = %q (ok=%v), want v2", content, ok)
	}

	revs := revisionsOf(t, repo, "b1")
	if len(revs) != 2 {
		t.Fatalf("revisions of b1 = %+v, want two", revs)
	}
	if revs[0].seq != 1 || revs[1].seq != 2 {
		t.Errorf("revision seqs = %d, %d, want 1, 2", revs[0].seq, revs[1].seq)
	}
	// seq 2 carries the parent/position that were already there — a revision
	// is a full snapshot, not a diff.
	if revs[1].parent != revs[0].parent || revs[1].position != revs[0].position {
		t.Errorf("seq 2 parent/position = %q/%d, want unchanged from seq 1 (%q/%d)",
			revs[1].parent, revs[1].position, revs[0].parent, revs[0].position)
	}
	if revs[1].content != "v2" {
		t.Errorf("seq 2 content = %q, want v2", revs[1].content)
	}
}

func TestApplyOpsMoveKeepsContent(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	// root -> mid -> leaf, plus an unrelated topic "other" to move onto.
	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root", Content: "Mid"},
		{Kind: knowledge.OpAdd, BlockID: "leaf", Parent: "mid", Content: "Leaf"},
		{Kind: knowledge.OpAdd, BlockID: "other", Content: "Other"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Legitimate move: "other" becomes root's second child. Content survives
	// unchanged; position is one stride past root's existing child (mid, pos 0).
	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("move"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "other", Parent: "root"},
	}); err != nil {
		t.Fatalf("legit move: %v", err)
	}
	parent, pos, content, ok := blockRow(t, repo, "other")
	if !ok || parent != "root" || pos != positionStride || content != "Other" {
		t.Errorf("other after move = parent=%q pos=%d content=%q ok=%v, want root/%d/Other/true",
			parent, pos, content, ok, positionStride)
	}
	revs := revisionsOf(t, repo, "other")
	if len(revs) != 2 || revs[1].content != "Other" || revs[1].parent != "root" || revs[1].position != positionStride {
		t.Errorf("other revisions = %+v, want a second entry content=Other parent=root position=%d", revs, positionStride)
	}

	// Cycle: moving root under its own grandchild must fail and write nothing.
	err := repo.ApplyOps(ctx, "grp-3", aiMeta("cycle"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "root", Parent: "leaf"},
	})
	if !errors.Is(err, knowledge.ErrCycle) {
		t.Fatalf("move into own subtree: got %v, want ErrCycle", err)
	}
	if parent, _, _, _ := blockRow(t, repo, "root"); parent != "" {
		t.Errorf("root reparented despite the cycle: parent=%q", parent)
	}
	if revs := revisionsOf(t, repo, "root"); len(revs) != 1 {
		t.Errorf("root gained a revision despite the rejected cycle move: %+v", revs)
	}
	var groups int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM revision_groups WHERE id = 'grp-3'`).Scan(&groups); err != nil {
		t.Fatalf("count grp-3: %v", err)
	}
	if groups != 0 {
		t.Errorf("grp-3 revision_groups row survived the rolled-back cycle move")
	}
}

func TestApplyOpsDeleteTombstonesSubtree(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root text"},
		{Kind: knowledge.OpAdd, BlockID: "a", Parent: "root", Content: "A text"},
		{Kind: knowledge.OpAdd, BlockID: "b", Parent: "a", Content: "B text"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-del", aiMeta("delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "root"},
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, id := range []string{"root", "a", "b"} {
		if _, _, _, ok := blockRow(t, repo, id); ok {
			t.Errorf("%s still present in blocks after delete", id)
		}
		revs := revisionsOf(t, repo, id)
		if len(revs) == 0 {
			t.Fatalf("%s has no revisions after delete", id)
		}
		head := revs[len(revs)-1]
		if !head.deleted || head.group != "grp-del" {
			t.Errorf("%s head revision = %+v, want deleted=true group=grp-del", id, head)
		}
	}
	// Content is preserved in the tombstone, not blanked.
	rootRevs := revisionsOf(t, repo, "root")
	if got := rootRevs[len(rootRevs)-1].content; got != "Root text" {
		t.Errorf("root tombstone content = %q, want %q", got, "Root text")
	}

	for _, q := range []string{"Root", "text"} {
		hits, err := repo.Search(ctx, q)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(hits) != 0 {
			t.Errorf("Search(%q) after delete = %v, want empty", q, hits)
		}
	}
}

func TestApplyOpsOwnerRevisionsBornReviewed(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	meta := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "owner edit"}
	if err := repo.ApplyOps(ctx, "grp-1", meta, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "b1", Content: "owner-written"},
	}); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}

	revs := revisionsOf(t, repo, "b1")
	if len(revs) != 1 || !revs[0].reviewed {
		t.Errorf("owner revision = %+v, want reviewed=true", revs)
	}
}

func TestApplyOpsMissingTargetIsNotFound(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	err := repo.ApplyOps(ctx, "grp-1", aiMeta("edit"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "nope", Content: "x"},
	})
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("edit of unknown block: got %v, want ErrNotFound", err)
	}
}

func TestApplyOpsLinkUpsertsEdge(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	if err := repo.ApplyOps(ctx, "grp-1", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "from", Content: "theory"},
		{Kind: knowledge.OpAdd, BlockID: "to", Content: "example"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.ApplyOps(ctx, "grp-2", aiMeta("link"), []knowledge.Op{
		{Kind: knowledge.OpLink, From: "from", To: "to", Relation: knowledge.RelationExample},
	}); err != nil {
		t.Fatalf("link: %v", err)
	}

	var reviewed, deleted bool
	var author string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT reviewed, deleted, author FROM edges WHERE from_id = 'from' AND to_id = 'to'`).
		Scan(&reviewed, &deleted, &author); err != nil {
		t.Fatalf("read edge: %v", err)
	}
	if reviewed || deleted || author != "ai" {
		t.Errorf("edge = reviewed=%v deleted=%v author=%q, want false/false/ai", reviewed, deleted, author)
	}

	// Soft-delete the edge (as a review rejection would), then re-link:
	// the upsert must resurrect it.
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE edges SET deleted = 1 WHERE from_id = 'from' AND to_id = 'to'`); err != nil {
		t.Fatalf("soft-delete edge: %v", err)
	}
	if err := repo.ApplyOps(ctx, "grp-3", aiMeta("re-link"), []knowledge.Op{
		{Kind: knowledge.OpLink, From: "from", To: "to", Relation: knowledge.RelationExample},
	}); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if err := repo.db.QueryRowContext(ctx,
		`SELECT deleted FROM edges WHERE from_id = 'from' AND to_id = 'to'`).Scan(&deleted); err != nil {
		t.Fatalf("read edge after re-link: %v", err)
	}
	if deleted {
		t.Errorf("edge still deleted=true after re-linking, want resurrected")
	}
}

func TestApplyOpsAtomic(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()

	err := repo.ApplyOps(ctx, "grp-1", aiMeta("mixed"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "new", Content: "half-applied"},
		{Kind: knowledge.OpEdit, BlockID: "nope", Content: "never lands"},
	})
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("batch with a stale op: got %v, want ErrNotFound", err)
	}

	if n := countRows(t, repo, "blocks"); n != 0 {
		t.Errorf("blocks has %d rows after a rolled-back apply, want 0", n)
	}
	if n := countRows(t, repo, "block_revisions"); n != 0 {
		t.Errorf("block_revisions has %d rows after a rolled-back apply, want 0", n)
	}
	if n := countRows(t, repo, "revision_groups"); n != 0 {
		t.Errorf("revision_groups has %d rows after a rolled-back apply, want 0", n)
	}
}
