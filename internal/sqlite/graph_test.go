package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// insertEdge inserts one edge with the storage layer's timestamp convention.
// deleted is the soft-delete flag RemoveEdge sets.
func insertEdge(ctx context.Context, t *testing.T, db *sql.DB, from, to string, rel knowledge.Relation, deleted bool) {
	t.Helper()
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC).Format(timeLayout)
	_, err := db.ExecContext(ctx, `
		INSERT INTO edges (from_id, to_id, relation, author, reviewed, deleted, created_at)
		VALUES (?, ?, ?, 'owner', 1, ?, ?)`,
		from, to, string(rel), deleted, now)
	if err != nil {
		t.Fatalf("insert edge %s->%s: %v", from, to, err)
	}
}

func TestGetTopicGraphCountsBlocks(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	// Topic "a" holds four blocks including its own root; "b" is isolated and
	// holds one. An isolated topic is shown, not hidden.
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

func TestGetTopicGraphAggregatesEdges(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	insertBlock(ctx, t, db, "a", "", 0, "Topic A")
	insertBlock(ctx, t, db, "a1", "a", 0, "A1")
	insertBlock(ctx, t, db, "a2", "a", 1, "A2")
	insertBlock(ctx, t, db, "b", "", 1, "Topic B")
	insertBlock(ctx, t, db, "b1", "b", 0, "B1")
	insertBlock(ctx, t, db, "c", "", 2, "Topic C")

	// Two links between the same pair of topics, from different blocks and
	// with different relations: one edge.
	insertEdge(ctx, t, db, "a1", "b1", knowledge.RelationRelated, false)
	insertEdge(ctx, t, db, "a2", "b1", knowledge.RelationExample, false)
	// The reverse direction of a pair already seen: still one edge.
	insertEdge(ctx, t, db, "b1", "a1", knowledge.RelationExample, false)
	// A link inside one topic is not an edge.
	insertEdge(ctx, t, db, "a1", "a2", knowledge.RelationRelated, false)
	// A soft-deleted edge is invisible, exactly as in GetEdges.
	insertEdge(ctx, t, db, "a1", "c", knowledge.RelationRelated, true)
	// An edge whose endpoint block does not exist is invisible too.
	insertEdge(ctx, t, db, "a1", "gone", knowledge.RelationRelated, false)

	got, err := r(db).GetTopicGraph(ctx)
	if err != nil {
		t.Fatalf("GetTopicGraph: %v", err)
	}
	want := []knowledge.TopicEdge{{From: "a", To: "b"}}
	if len(got.Edges) != len(want) || got.Edges[0] != want[0] {
		t.Fatalf("edges = %v, want %v", got.Edges, want)
	}
	// Topic C stays on the map even though its only edge was deleted.
	if len(got.Topics) != 3 {
		t.Fatalf("topics = %v, want three", got.Topics)
	}
}

func TestGetTopicGraphOrdersDeterministically(t *testing.T) {
	ctx := t.Context()
	db := openTestDB(t)

	// Inserted out of id order; the read must still come back sorted, because
	// the layout seeds positions by index.
	insertBlock(ctx, t, db, "c", "", 0, "Topic C")
	insertBlock(ctx, t, db, "a", "", 1, "Topic A")
	insertBlock(ctx, t, db, "b", "", 2, "Topic B")
	insertEdge(ctx, t, db, "c", "a", knowledge.RelationRelated, false)
	insertEdge(ctx, t, db, "c", "b", knowledge.RelationRelated, false)

	got, err := r(db).GetTopicGraph(ctx)
	if err != nil {
		t.Fatalf("GetTopicGraph: %v", err)
	}
	ids := []knowledge.ID{got.Topics[0].ID, got.Topics[1].ID, got.Topics[2].ID}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("topic order = %v, want a b c", ids)
	}
	want := []knowledge.TopicEdge{{From: "a", To: "c"}, {From: "b", To: "c"}}
	if len(got.Edges) != 2 || got.Edges[0] != want[0] || got.Edges[1] != want[1] {
		t.Errorf("edges = %v, want %v", got.Edges, want)
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
