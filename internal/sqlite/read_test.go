package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertBlock(ctx context.Context, t *testing.T, db *sql.DB, id, parent string, pos int, content string) {
	t.Helper()
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC).Format(timeLayout)
	var parentArg any
	if parent != "" {
		parentArg = parent
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?, ?)`,
		id, parentArg, pos, content, now, now)
	if err != nil {
		t.Fatalf("insert block %s: %v", id, err)
	}
}

func ids(blocks []knowledge.Block) []knowledge.ID {
	out := make([]knowledge.ID, len(blocks))
	for i, b := range blocks {
		out[i] = b.ID
	}
	return out
}

func hitIDs(hits []knowledge.SearchHit) []knowledge.ID {
	out := make([]knowledge.ID, len(hits))
	for i, h := range hits {
		out[i] = h.Block.ID
	}
	return out
}

func TestGetSubtree(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "root", "", 0, "Root")
	insertBlock(ctx, t, db, "b", "root", 1, "B")
	insertBlock(ctx, t, db, "a", "root", 0, "A")
	insertBlock(ctx, t, db, "a1", "a", 0, "A1")
	insertBlock(ctx, t, db, "other", "", 0, "Other root")
	insertBlock(ctx, t, db, "other-child", "other", 0, "Other child")

	got, err := r(db).GetSubtree(ctx, "root")
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}

	want := []knowledge.ID{"root", "a", "a1", "b"}
	if g := ids(got); !equalIDs(g, want) {
		t.Fatalf("GetSubtree order = %v, want %v", g, want)
	}
	if got[0].Content != "Root" {
		t.Fatalf("root content = %q, want Root", got[0].Content)
	}
	if got[1].ParentID != "root" || got[1].Position != 0 {
		t.Fatalf("block a parent/pos = %q/%d, want root/0", got[1].ParentID, got[1].Position)
	}
}

func TestGetSubtreeLeaf(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "root", "", 0, "Root")
	insertBlock(ctx, t, db, "leaf", "root", 0, "Leaf")

	got, err := r(db).GetSubtree(ctx, "leaf")
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}
	if len(got) != 1 || got[0].ID != "leaf" {
		t.Fatalf("subtree of a leaf = %v, want [leaf]", ids(got))
	}
}

func TestGetSubtreeMissingRoot(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "root", "", 0, "Root")

	_, err := r(db).GetSubtree(ctx, "nope")
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("GetSubtree(missing) error = %v, want errors.Is ErrNotFound", err)
	}
}

func TestGetTopics(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "t1", "", 0, "Topic 1")
	insertBlock(ctx, t, db, "t2", "", 1, "Topic 2")
	insertBlock(ctx, t, db, "t1b", "t1", 1, "Sub 1b")
	insertBlock(ctx, t, db, "t1a", "t1", 0, "Sub 1a")
	insertBlock(ctx, t, db, "deep", "t1a", 0, "Grandchild")

	got, err := r(db).GetTopics(ctx)
	if err != nil {
		t.Fatalf("GetTopics: %v", err)
	}

	set := map[knowledge.ID]bool{}
	for _, b := range got {
		set[b.ID] = true
	}
	for _, want := range []knowledge.ID{"t1", "t2", "t1a", "t1b"} {
		if !set[want] {
			t.Errorf("GetTopics missing %s; got %v", want, ids(got))
		}
	}
	if set["deep"] {
		t.Errorf("GetTopics leaked grandchild deep; got %v", ids(got))
	}
	if len(got) != 4 {
		t.Fatalf("GetTopics returned %d blocks, want 4", len(got))
	}
}

func TestGetTopicsOrdersByPosition(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "z-root", "", 0, "First")
	insertBlock(ctx, t, db, "a-root", "", 1, "Second")
	insertBlock(ctx, t, db, "z-b", "z-root", 1, "child b")
	insertBlock(ctx, t, db, "z-a", "z-root", 0, "child a")

	got, err := r(db).GetTopics(ctx)
	if err != nil {
		t.Fatalf("GetTopics: %v", err)
	}
	want := []knowledge.ID{"z-root", "z-a", "z-b", "a-root"}
	gotIDs := ids(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("GetTopics = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("GetTopics order = %v, want %v (roots and children by position)", gotIDs, want)
		}
	}
}

func TestGetTopicsEmpty(t *testing.T) {
	got, err := r(openTestDB(t)).GetTopics(t.Context())
	if err != nil {
		t.Fatalf("GetTopics on empty base: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetTopics on empty base = %v, want empty", ids(got))
	}
}

func TestSearchFindsContent(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "b1", "", 0, "pgx connection pooling in Go")
	insertBlock(ctx, t, db, "b2", "", 1, "python asyncio patterns")

	got, err := r(db).Search(ctx, "pgx")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Block.ID != "b1" {
		t.Fatalf("Search(pgx) = %v, want [b1]", hitIDs(got))
	}
}

func TestSearchMultiWordIsAND(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "both", "", 0, "go concurrency patterns")
	insertBlock(ctx, t, db, "one", "", 1, "python concurrency")

	got, err := r(db).Search(ctx, "concurrency go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Block.ID != "both" {
		t.Fatalf("Search(concurrency go) = %v, want [both] (implicit AND)", hitIDs(got))
	}
}

func TestSearchNeutralizesSyntax(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "dot", "", 0, "context.Context cancellation")
	insertBlock(ctx, t, db, "onlygo", "", 1, "go routines")

	cases := []struct {
		name  string
		query string
		want  []knowledge.ID
	}{
		{"dotted term", "context.Context", []knowledge.ID{"dot"}},
		{"operator word is literal AND", "go OR cancellation", nil}, // no block has go+or+cancellation
		{"lone double quote", `"`, nil},
		{"unbalanced quote with term", `context"`, []knowledge.ID{"dot"}},
		{"plus signs", "c++", nil},
		{"trailing star is literal, not a prefix op", "context*", []knowledge.ID{"dot"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r(db).Search(ctx, tc.query)
			if err != nil {
				t.Fatalf("Search(%q) errored: %v", tc.query, err)
			}
			if !equalIDs(hitIDs(got), tc.want) {
				t.Fatalf("Search(%q) = %v, want %v", tc.query, hitIDs(got), tc.want)
			}
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "b1", "", 0, "something")

	for _, q := range []string{"", "   ", "\t\n"} {
		got, err := r(db).Search(ctx, q)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(got) != 0 {
			t.Fatalf("Search(%q) = %v, want empty", q, hitIDs(got))
		}
	}
}

func TestSearchReturnsASnippetAroundTheMatch(t *testing.T) {
	repo := testRepo(t)
	// The opening word must not survive into a snippet windowed around a
	// match far past the 24-token budget: a document this long proves the
	// snippet is a real excerpt, not the whole block echoed back.
	filler := strings.Repeat("filler ", 30)
	doc := "# Marmalade\n\n" + filler + "\n\nCancellation propagates through the tree.\n"
	if err := repo.ApplyOps(t.Context(), "g1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: doc, Mode: knowledge.ModeDoc},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	hits, err := repo.Search(t.Context(), "propagates")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	snippet := hits[0].Snippet
	if !strings.Contains(snippet, "propagates") {
		t.Errorf("snippet = %q, want it to carry the match", snippet)
	}
	if strings.Contains(snippet, "Marmalade") {
		t.Errorf("snippet = %q, want the far-off opening word excluded — a whole-content snippet would carry it", snippet)
	}
	if !strings.Contains(snippet, "…") {
		t.Errorf("snippet = %q, want the truncation marker: the document is far longer than the snippet window", snippet)
	}
	if hits[0].Block.ID != "t1" {
		t.Errorf("block = %q, want t1", hits[0].Block.ID)
	}
}

func TestGetBlocks(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "01A", "", 0, "alpha")
	insertBlock(ctx, t, db, "01B", "01A", 0, "beta")

	got, err := r(db).GetBlocks(ctx, []knowledge.ID{"01A", "01GONE", "01B"})
	if err != nil {
		t.Fatalf("GetBlocks: %v", err)
	}
	if len(got) != 2 || got["01A"].Content != "alpha" || got["01B"].Content != "beta" {
		t.Errorf("blocks = %+v, want 01A and 01B", got)
	}
	if _, ok := got["01GONE"]; ok {
		t.Error("a missing id must be absent, not present")
	}
}

func TestGetBlocksEmptyInput(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	got, err := r(db).GetBlocks(ctx, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("GetBlocks(nil) = %v, %v, want empty map and no error", got, err)
	}
}

func r(db *sql.DB) *Repo {
	seq := 0
	return NewRepo(db, time.Now, func() knowledge.ID {
		seq++
		return knowledge.ID(fmt.Sprintf("r-%03d", seq))
	})
}

func equalIDs(a, b []knowledge.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
