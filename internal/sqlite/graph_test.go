package sqlite

import (
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
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
	if err != nil {
		t.Fatalf("GetTopicGraph: %v", err)
	}
	want := []knowledge.TopicNode{
		{ID: "a", Title: "Topic A", Blocks: 4},
		{ID: "b", Title: "Topic B", Blocks: 1},
	}
	if len(got.Topics) != len(want) {
		t.Fatalf("topics = %v, want %v", got.Topics, want)
	}
	for i := range want {
		if got.Topics[i] != want[i] {
			t.Errorf("topic %d = %+v, want %+v", i, got.Topics[i], want[i])
		}
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %v, want none", got.Edges)
	}
}

func TestGetTopicGraphOrdersDeterministically(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "c", "", 0, "Topic C")
	insertBlock(ctx, t, db, "a", "", 1, "Topic A")
	insertBlock(ctx, t, db, "b", "", 2, "Topic B")

	got, err := r(db).GetTopicGraph(ctx)
	if err != nil {
		t.Fatalf("GetTopicGraph: %v", err)
	}
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
			if len(got) != len(tc.want) {
				t.Fatalf("pruneDanglingEdges = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("edge %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestGetTopicGraphEmptyBase(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	got, err := r(db).GetTopicGraph(ctx)
	if err != nil {
		t.Fatalf("GetTopicGraph: %v", err)
	}
	if len(got.Topics) != 0 || len(got.Edges) != 0 {
		t.Errorf("graph = %+v, want empty", got)
	}
}
