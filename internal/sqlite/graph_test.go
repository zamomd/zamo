package sqlite

import (
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestGetTopicGraphCountsBlocks(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "a", "", 0, "Topic A")
	insertBlock(ctx, t, db, "a1", "a", 0, "A1")
	insertBlock(ctx, t, db, "a2", "a", 1, "A2")
	insertBlock(ctx, t, db, "a1x", "a1", 0, "A1X") // a grandchild still counts
	insertBlock(ctx, t, db, "b", "", 1, "Topic B")

	got, err := r(db).GetTopicGraph(ctx)
	require.NoError(t, err)
	want := []knowledge.TopicNode{
		{ID: "a", Title: "Topic A", Blocks: 4},
		{ID: "b", Title: "Topic B", Blocks: 1},
	}
	if diff := cmp.Diff(want, got.Topics); diff != "" {
		t.Errorf("topics mismatch (-want +got):\n%s", diff)
	}
}

func TestTopicEdgesFromTopicReferences(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "Unlike [[Rust]]."},
	))
	g, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	want := []knowledge.TopicEdge{{From: "01RUST", To: "01ZIG"}}
	if diff := cmp.Diff(want, g.Edges); diff != "" {
		t.Errorf("edges mismatch (-want +got):\n%s", diff)
	}
}

func TestTopicEdgesFromBlockReferences(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01CCCCCCCCCCCCCCCCCCCCCCCC",
			Parent: "01RUST", Content: "A claim.",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "See ((01CCCCCCCCCCCCCCCCCCCCCCCC|A claim)).",
		},
	))
	g, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	want := []knowledge.TopicEdge{{From: "01RUST", To: "01ZIG"}}
	if diff := cmp.Diff(want, g.Edges); diff != "" {
		t.Errorf("edges mismatch (-want +got):\n%s", diff)
	}
}

func TestTopicEdgesDropSelfLoops(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01AAAAAAAAAAAAAAAAAAAAAAAA",
			Parent: "01ZIG", Content: "One.",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01B", Parent: "01ZIG",
			Content: "Two, see ((01AAAAAAAAAAAAAAAAAAAAAAAA|One)).",
		},
	))
	g, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	if len(g.Edges) != 0 {
		t.Errorf("edges = %v, want none", g.Edges)
	}
}

func TestTopicEdgesNoneBetweenUnconnectedTopics(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01N1", Parent: "01ZIG", Content: "Zig has no borrow checker."},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01N2", Parent: "01RUST", Content: "Rust has a borrow checker."},
	))
	g, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	if len(g.Edges) != 0 {
		t.Errorf("edges = %v, want none: two populated topics with no references between them", g.Edges)
	}
}

func TestTopicEdgesCollapseMultiplicityAndDirection(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01N1", Parent: "01ZIG", Content: "First, unlike [[Rust]]."},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01N2", Parent: "01ZIG", Content: "Second, unlike [[Rust]]."},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01N3", Parent: "01RUST", Content: "And [[Zig]] again."},
	))
	g, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	want := []knowledge.TopicEdge{{From: "01RUST", To: "01ZIG"}}
	if diff := cmp.Diff(want, g.Edges); diff != "" {
		t.Errorf("edges mismatch (-want +got), references both ways must collapse to one edge:\n%s", diff)
	}
}

func TestGetTopicGraphOrdersDeterministically(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "c", "", 0, "Topic C")
	insertBlock(ctx, t, db, "a", "", 1, "Topic A")
	insertBlock(ctx, t, db, "b", "", 2, "Topic B")

	got, err := r(db).GetTopicGraph(ctx)
	require.NoError(t, err)
	ids := []knowledge.ID{got.Topics[0].ID, got.Topics[1].ID, got.Topics[2].ID}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("topic order = %v, want a b c", ids)
	}
}

func TestPruneDanglingEdges(t *testing.T) {
	topics := []knowledge.TopicNode{{ID: "a"}, {ID: "b"}}

	cases := []struct {
		name  string
		edges []knowledge.TopicEdge
		want  []knowledge.TopicEdge
	}{
		{
			name:  "dangling on From",
			edges: []knowledge.TopicEdge{{From: "gone", To: "b"}},
			want:  nil,
		},
		{
			name:  "dangling on To",
			edges: []knowledge.TopicEdge{{From: "a", To: "gone"}},
			want:  nil,
		},
		{
			name:  "both endpoints present survives untouched",
			edges: []knowledge.TopicEdge{{From: "a", To: "b"}},
			want:  []knowledge.TopicEdge{{From: "a", To: "b"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pruneDanglingEdges(topics, tc.edges)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("pruneDanglingEdges mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetTopicGraphEmptyBase(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	got, err := r(db).GetTopicGraph(ctx)
	require.NoError(t, err)
	if len(got.Topics) != 0 || len(got.Edges) != 0 {
		t.Errorf("graph = %+v, want empty", got)
	}
}
