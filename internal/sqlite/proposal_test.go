package sqlite

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// fixedClock is the decision time the injected clock returns across these tests,
// so decided_at is deterministic.
var fixedClock = time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

// newTestRepo opens a fresh migrated database on disk and wraps it in a Repo
// whose clock is pinned to fixedClock.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepo(db, func() time.Time { return fixedClock })
}

func TestSaveAndListProposals(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	created := time.Date(2026, 7, 5, 9, 30, 0, 123456789, time.UTC)
	want := knowledge.Proposal{
		ID:     "01PENDING",
		Source: knowledge.SourceGUIExpand,
		Status: knowledge.StatusPending,
		Title:  "Contexts in Go",
		Ops: []knowledge.Op{
			{Kind: knowledge.OpAdd, TempID: "t1", Content: "# Contexts in Go"},
			{Kind: knowledge.OpAdd, TempID: "t2", Parent: "t1", Content: "Why context.Context", Rationale: "core idea"},
		},
		CreatedAt: created,
	}
	// A non-pending proposal must not surface in the pending queue.
	other := knowledge.Proposal{
		ID:        "01REJECTED",
		Source:    knowledge.SourceMCP,
		Status:    knowledge.StatusRejected,
		Title:     "noise",
		Ops:       []knowledge.Op{{Kind: knowledge.OpAdd, TempID: "t1", Content: "x"}},
		CreatedAt: created,
	}
	for _, p := range []knowledge.Proposal{want, other} {
		if err := repo.SaveProposal(ctx, p); err != nil {
			t.Fatalf("SaveProposal(%s): %v", p.ID, err)
		}
	}

	got, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("pending queue has %d proposals, want 1 (rejected must be filtered out)", len(got))
	}
	g := got[0]
	if !g.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", g.CreatedAt, want.CreatedAt)
	}
	if !g.DecidedAt.IsZero() {
		t.Errorf("DecidedAt = %v, want zero for a pending proposal", g.DecidedAt)
	}
	// Times are compared with Equal above; align them before the deep compare of
	// the remaining fields (ops, ids, source, status, title).
	g.CreatedAt, g.DecidedAt = want.CreatedAt, want.DecidedAt
	if !reflect.DeepEqual(g, want) {
		t.Errorf("round-trip mismatch:\n got %#v\nwant %#v", g, want)
	}
}
