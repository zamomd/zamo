package sqlite

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// fixedClock is the decision time the injected clock returns across these
// tests, so timestamps are deterministic.
var fixedClock = time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

// newTestRepo opens a fresh migrated database on disk and wraps it in a Repo
// whose clock is pinned to fixedClock and whose revision ids are
// deterministic and collision-free within one test.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	seq := 0
	return NewRepo(db, func() time.Time { return fixedClock }, func() knowledge.ID {
		seq++
		return knowledge.ID(fmt.Sprintf("rev-%03d", seq))
	})
}

// linkBase builds a small forest to link across: R1(A, B) and R2(C), so edges
// can span branches and topics like real links do.
func linkBase(t *testing.T, repo *Repo) {
	t.Helper()
	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceGUIExpand, Title: "setup"}
	if err := repo.ApplyOps(t.Context(), "01SETUP", meta, []knowledge.Op{
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
	// Both directions must come back: an example linked *to* a theory block
	// is that block's link too.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("link")); err != nil {
		t.Fatalf("AddEdge related: %v", err)
	}
	if err := repo.AddEdge(ctx, "01CCC", "01AAA", knowledge.RelationExample, ownerMeta("link")); err != nil {
		t.Fatalf("AddEdge example: %v", err)
	}

	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	// Deterministic order: relation first, then endpoints. Owner-created
	// edges are born reviewed.
	want := []knowledge.Edge{
		{From: "01CCC", To: "01AAA", Relation: knowledge.RelationExample, Reviewed: true},
		{From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated, Reviewed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEdges(A) = %+v, want %+v", got, want)
	}

	// The other endpoint sees only its own incident edge.
	got, err = repo.GetEdges(ctx, "01BBB")
	if err != nil {
		t.Fatalf("GetEdges(B): %v", err)
	}
	want = []knowledge.Edge{{From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated, Reviewed: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEdges(B) = %+v, want %+v", got, want)
	}
}

func TestGetEdgesUnlinkedAndUnknownAreEmpty(t *testing.T) {
	// No links — and even an unknown id — is an empty answer, not an error.
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
	// Re-adding the same link must not error — the edge being present already
	// is the success case.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	for range 2 {
		if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("link")); err != nil {
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

func TestAddEdgeVanishedEndpointIsMissing(t *testing.T) {
	// Either endpoint gone is a plain not-found, not a raw foreign-key error.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	err := repo.AddEdge(ctx, "01AAA", "01GONE", knowledge.RelationRelated, ownerMeta("link"))
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("AddEdge to vanished target = %v, want ErrNotFound", err)
	}
	err = repo.AddEdge(ctx, "01GONE", "01AAA", knowledge.RelationRelated, ownerMeta("link"))
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("AddEdge from vanished source = %v, want ErrNotFound", err)
	}
}

func TestRemoveEdgeDeletesAndIsIdempotent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("link")); err != nil {
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

func TestRemoveEdgeSoftDeletes(t *testing.T) {
	// RemoveEdge must soft-delete: GetEdges hides it, but re-AddEdge
	// resurrects the same row (deleted=0) rather than erroring on a
	// duplicate primary key.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("link")); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := repo.RemoveEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
		t.Fatalf("RemoveEdge: %v", err)
	}
	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges after remove: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetEdges after remove = %+v, want empty", got)
	}

	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("re-link")); err != nil {
		t.Fatalf("re-AddEdge: %v", err)
	}
	got, err = repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges after re-add: %v", err)
	}
	if len(got) != 1 || got[0].To != "01BBB" {
		t.Errorf("GetEdges after re-add = %+v, want the edge resurrected", got)
	}
}

func TestEdgesWithDeadEndpointHidden(t *testing.T) {
	// A block deleted by an unreviewed AI change must vanish from GetEdges
	// for its still-live neighbour, and reappear once resurrected.
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.AddEdge(ctx, "01AAA", "01CCC", knowledge.RelationRelated, ownerMeta("link")); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	// linkBase's setup is AI-authored (unreviewed); give 01CCC a reviewed
	// baseline first so RejectBlock below has a prior state to roll back to
	// instead of deleting the block outright.
	if err := repo.MarkBlockReviewed(ctx, "01CCC"); err != nil {
		t.Fatalf("MarkBlockReviewed: %v", err)
	}
	if err := repo.ApplyOps(ctx, "01DEL", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01CCC"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}

	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges while endpoint dead: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetEdges(01AAA) with a dead endpoint = %+v, want empty", got)
	}

	// Reject the delete: 01CCC comes back, so does the edge.
	if err := repo.RejectBlock(ctx, "01CCC"); err != nil {
		t.Fatalf("RejectBlock: %v", err)
	}
	got, err = repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges after resurrect: %v", err)
	}
	if len(got) != 1 || got[0].To != "01CCC" {
		t.Errorf("GetEdges(01AAA) after resurrecting endpoint = %+v, want the edge visible again", got)
	}
}

func TestRejectEdgeHardDeletes(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	// An AI link is unreviewed; RejectEdge must remove the row entirely.
	if err := repo.ApplyOps(ctx, "01LINK", aiMeta("ai link"), []knowledge.Op{
		{Kind: knowledge.OpLink, From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated},
	}); err != nil {
		t.Fatalf("ai link: %v", err)
	}
	if err := repo.RejectEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
		t.Fatalf("RejectEdge: %v", err)
	}
	var n int
	err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM edges WHERE from_id = '01AAA' AND to_id = '01BBB' AND relation = 'related'`).Scan(&n)
	if err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if n != 0 {
		t.Errorf("edge row survived RejectEdge, want gone entirely")
	}

	// RejectEdge on a reviewed edge is refused: only unreviewed rows are
	// deletable this way.
	if err := repo.AddEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated, ownerMeta("link")); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := repo.RejectEdge(ctx, "01AAA", "01BBB", knowledge.RelationRelated); !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("RejectEdge(reviewed edge) = %v, want ErrNotFound", err)
	}
}

func TestMarkEdgeReviewed(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	linkBase(t, repo)

	if err := repo.ApplyOps(ctx, "01LINK", aiMeta("ai link"), []knowledge.Op{
		{Kind: knowledge.OpLink, From: "01AAA", To: "01BBB", Relation: knowledge.RelationRelated},
	}); err != nil {
		t.Fatalf("ai link: %v", err)
	}
	if err := repo.MarkEdgeReviewed(ctx, "01AAA", "01BBB", knowledge.RelationRelated); err != nil {
		t.Fatalf("MarkEdgeReviewed: %v", err)
	}
	got, err := repo.GetEdges(ctx, "01AAA")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(got) != 1 || !got[0].Reviewed {
		t.Errorf("GetEdges after MarkEdgeReviewed = %+v, want Reviewed=true", got)
	}

	err = repo.MarkEdgeReviewed(ctx, "01AAA", "01NOPE", knowledge.RelationRelated)
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("MarkEdgeReviewed(absent) = %v, want ErrNotFound", err)
	}
}
