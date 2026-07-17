package sqlite

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// linkBase builds a small forest to link across: R1(A, B) and R2(C), so edges
// can span branches and topics like real links do.
func linkBase(t *testing.T, repo *Repo) {
	t.Helper()
	savePending(t, repo, "01SETUP", knowledge.SourceGUIExpand)
	if err := repo.ApplyProposal(t.Context(), "01SETUP", []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Root 1"},
		{Kind: knowledge.OpAdd, BlockID: "01AAA", Parent: "01R1", Content: "A"},
		{Kind: knowledge.OpAdd, BlockID: "01BBB", Parent: "01R1", Content: "B"},
		{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "Root 2"},
		{Kind: knowledge.OpAdd, BlockID: "01CCC", Parent: "01R2", Content: "C"},
	}); err != nil {
		t.Fatalf("setup apply: %v", err)
	}
}

func TestGetEdgesReturnsIncidentEdges(t *testing.T) {
	// A block's links panel needs both directions: an example linked *to* a
	// theory block must show up on the theory block too.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
		t.Fatalf("AddEdge related: %v", err)
	}
	if err := repo.AddEdge(ctx, "01CCC", "01AAA", knowledge.RelationExample); err != nil {
		t.Fatalf("AddEdge example: %v", err)
	}

	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	// Deterministic order: relation first, then endpoints.
	want := []knowledge.Edge{
		{From: "01CCC", To: "01AAA", Relation: knowledge.RelationExample},
		{From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEdges(A) = %+v, want %+v", got, want)
	}

	// The other endpoint sees only its own incident edge.
	got, err = repo.GetEdges(ctx, "01BBB")
	if err != nil {
		t.Fatalf("GetEdges(B): %v", err)
	}
	want = []knowledge.Edge{{From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEdges(B) = %+v, want %+v", got, want)
	}
}

func TestGetEdgesUnlinkedAndUnknownAreEmpty(t *testing.T) {
	// No links — and even an unknown id — is an empty answer, not an error:
	// "nothing to show" is a normal panel state.
	repo := newTestRepo(t)
	linkBase(t, repo)

	for _, id := range []knowledge.ID{"01BBB", "01NOPE"} {
		got, err := repo.GetEdges(t.Context(), id)
		if err != nil {
			t.Fatalf("GetEdges(%s): %v", id, err)
		}
		if len(got) != 0 {
			t.Errorf("GetEdges(%s) = %+v, want empty", id, got)
		}
	}
}

func TestAddEdgeIsIdempotent(t *testing.T) {
	// Re-adding the same link must not error: the caller's goal state — "this
	// edge exists" — is already true.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	for range 2 {
		if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}
	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("edges after double add = %+v, want exactly one", got)
	}
}

func TestAddEdgeVanishedEndpointIsStale(t *testing.T) {
	// Either endpoint gone is the same "base moved" signal applyLink gives,
	// not a raw foreign-key error.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01GONE", knowledge.RelationRelated); !errors.Is(err, knowledge.ErrStale) {
		t.Errorf("AddEdge to vanished target = %v, want ErrStale", err)
	}
	if err := repo.AddEdge(ctx, "01GONE", "01AAA", knowledge.RelationRelated); !errors.Is(err, knowledge.ErrStale) {
		t.Errorf("AddEdge from vanished source = %v, want ErrStale", err)
	}
}

func TestRemoveEdgeDeletesAndIsIdempotent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	for range 2 { // the second remove hits nothing and must still succeed
		if err := repo.RemoveEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
			t.Fatalf("RemoveEdge: %v", err)
		}
	}
	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("edges after remove = %+v, want empty", got)
	}
}
