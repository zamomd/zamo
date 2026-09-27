package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
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
	require.NoError(t, err, "insert block %s", id)
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
	require.NoError(t, err)

	want := []knowledge.ID{"root", "a", "a1", "b"}
	require.Equal(t, want, ids(got))
	require.Equal(t, "Root", got[0].Content)
	require.Equal(t, knowledge.ID("root"), got[1].ParentID)
	require.Equal(t, 0, got[1].Position)
}

func TestGetSubtreeLeaf(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "root", "", 0, "Root")
	insertBlock(ctx, t, db, "leaf", "root", 0, "Leaf")

	got, err := r(db).GetSubtree(ctx, "leaf")
	require.NoError(t, err)
	require.Equal(t, []knowledge.ID{"leaf"}, ids(got))
}

func TestGetSubtreeMissingRoot(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "root", "", 0, "Root")

	_, err := r(db).GetSubtree(ctx, "nope")
	require.ErrorIs(t, err, knowledge.ErrNotFound)
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
	require.NoError(t, err)

	set := map[knowledge.ID]bool{}
	for _, b := range got {
		set[b.ID] = true
	}
	for _, want := range []knowledge.ID{"t1", "t2", "t1a", "t1b"} {
		assert.True(t, set[want], "GetTopics missing %s; got %v", want, ids(got))
	}
	assert.False(t, set["deep"], "GetTopics leaked grandchild deep; got %v", ids(got))
	require.Len(t, got, 4)
}

func TestGetTopicsOrdersByPosition(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "z-root", "", 0, "First")
	insertBlock(ctx, t, db, "a-root", "", 1, "Second")
	insertBlock(ctx, t, db, "z-b", "z-root", 1, "child b")
	insertBlock(ctx, t, db, "z-a", "z-root", 0, "child a")

	got, err := r(db).GetTopics(ctx)
	require.NoError(t, err)
	want := []knowledge.ID{"z-root", "z-a", "z-b", "a-root"}
	require.Equal(t, want, ids(got), "roots and children by position")
}

func TestGetTopicsEmpty(t *testing.T) {
	got, err := r(openTestDB(t)).GetTopics(t.Context())
	require.NoError(t, err, "GetTopics on empty base")
	require.Empty(t, got)
}

func TestSearchFindsContent(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "b1", "", 0, "pgx connection pooling in Go")
	insertBlock(ctx, t, db, "b2", "", 1, "python asyncio patterns")

	got, err := r(db).Search(ctx, "pgx")
	require.NoError(t, err)
	require.Equal(t, []knowledge.ID{"b1"}, hitIDs(got))
}

func TestSearchMultiWordIsAND(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "both", "", 0, "go concurrency patterns")
	insertBlock(ctx, t, db, "one", "", 1, "python concurrency")

	got, err := r(db).Search(ctx, "concurrency go")
	require.NoError(t, err)
	require.Equal(t, []knowledge.ID{"both"}, hitIDs(got), "implicit AND")
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
			require.NoError(t, err)
			require.True(t, equalIDs(hitIDs(got), tc.want), "Search(%q) = %v, want %v", tc.query, hitIDs(got), tc.want)
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "b1", "", 0, "something")

	for _, q := range []string{"", "   ", "\t\n"} {
		got, err := r(db).Search(ctx, q)
		require.NoError(t, err, "Search(%q)", q)
		require.Empty(t, got, "Search(%q)", q)
	}
}

func TestSearchReturnsASnippetAroundTheMatch(t *testing.T) {
	repo := testRepo(t)
	// The opening word must not survive into a snippet windowed around a
	// match far past the 24-token budget: a document this long proves the
	// snippet is a real excerpt, not the whole block echoed back.
	filler := strings.Repeat("filler ", 30)
	doc := "# Marmalade\n\n" + filler + "\n\nCancellation propagates through the tree.\n"
	require.NoError(t, applyOps(t.Context(), repo, "g1", aiMeta("add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "t1", Content: doc, Mode: knowledge.ModeDoc},
	}), "apply")
	hits, err := repo.Search(t.Context(), "propagates")
	require.NoError(t, err, "search")
	require.Len(t, hits, 1)
	snippet := hits[0].Snippet
	assert.Contains(t, snippet, "propagates")
	assert.NotContains(t, snippet, "Marmalade", "a whole-content snippet would carry the far-off opening word")
	assert.Contains(t, snippet, "…", "the document is far longer than the snippet window")
	assert.Equal(t, knowledge.ID("t1"), hits[0].Block.ID)
}

func TestGetBlocks(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)
	insertBlock(ctx, t, db, "01A", "", 0, "alpha")
	insertBlock(ctx, t, db, "01B", "01A", 0, "beta")

	got, err := r(db).GetBlocks(ctx, []knowledge.ID{"01A", "01GONE", "01B"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "alpha", got["01A"].Content)
	assert.Equal(t, "beta", got["01B"].Content)
	assert.NotContains(t, got, knowledge.ID("01GONE"), "a missing id must be absent")
}

func TestGetBlocksEmptyInput(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	got, err := r(db).GetBlocks(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
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
