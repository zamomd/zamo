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

func TestListProposalsChronologicalOrder(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	// Same second, fractional parts chosen to expose a layout that trims trailing
	// zeros: stored as TEXT, ".120" would sort before ".100", and a whole second
	// (no fraction) would sort after any fractional one.
	base := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	byIndex := []time.Time{
		base.Add(0),                      // .000000000
		base.Add(1),                      // .000000001
		base.Add(100 * time.Millisecond), // .100
		base.Add(120 * time.Millisecond), // .120
	}
	// Insert scrambled; the queue must still come back oldest-first.
	for i, idx := range []int{3, 1, 0, 2} {
		p := knowledge.Proposal{
			ID:        knowledge.ID(fmt.Sprintf("01P%d", i)),
			Source:    knowledge.SourceMCP,
			Status:    knowledge.StatusPending,
			CreatedAt: byIndex[idx],
		}
		if err := repo.SaveProposal(ctx, p); err != nil {
			t.Fatalf("SaveProposal: %v", err)
		}
	}

	got, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(got) != len(byIndex) {
		t.Fatalf("got %d proposals, want %d", len(got), len(byIndex))
	}
	for i := 1; i < len(got); i++ {
		if got[i].CreatedAt.Before(got[i-1].CreatedAt) {
			t.Errorf("queue is out of order at %d: %s comes before %s",
				i, got[i-1].CreatedAt.Format(time.RFC3339Nano), got[i].CreatedAt.Format(time.RFC3339Nano))
		}
	}
}

func TestRejectProposalDecides(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	p := knowledge.Proposal{
		ID:        "01P",
		Source:    knowledge.SourceGUIExpand,
		Status:    knowledge.StatusPending,
		Title:     "t",
		Ops:       []knowledge.Op{{Kind: knowledge.OpAdd, TempID: "t1", Content: "x"}},
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveProposal(ctx, p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := repo.RejectProposal(ctx, p.ID); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	pending, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending queue has %d proposals, want 0 after reject", len(pending))
	}

	rejected, err := repo.ListProposals(ctx, knowledge.StatusRejected)
	if err != nil {
		t.Fatalf("list rejected: %v", err)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected has %d proposals, want 1", len(rejected))
	}
	if !rejected[0].DecidedAt.Equal(fixedClock) {
		t.Errorf("DecidedAt = %v, want %v (the injected clock)", rejected[0].DecidedAt, fixedClock)
	}
}

func TestSaveProposalRoundTripsDecidedAt(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	decided := time.Date(2026, 7, 6, 10, 15, 30, 250_000_000, time.UTC)
	p := knowledge.Proposal{
		ID:        "01APPLIED",
		Source:    knowledge.SourceGUIExpand,
		Status:    knowledge.StatusApplied,
		Title:     "done",
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
		DecidedAt: decided,
	}
	if err := repo.SaveProposal(ctx, p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	got, err := repo.ListProposals(ctx, knowledge.StatusApplied)
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d applied proposals, want 1", len(got))
	}
	if !got[0].DecidedAt.Equal(decided) {
		t.Errorf("DecidedAt round-trip = %v, want %v", got[0].DecidedAt, decided)
	}
}

func TestSaveProposalZeroOps(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	// nil Ops must survive the JSON round-trip (marshals to "null", back to nil).
	p := knowledge.Proposal{
		ID:        "01EMPTY",
		Source:    knowledge.SourceMCP,
		Status:    knowledge.StatusPending,
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveProposal(ctx, p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	got, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	if len(got[0].Ops) != 0 {
		t.Errorf("Ops = %#v, want empty", got[0].Ops)
	}
}

func TestSaveProposalDuplicateID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	p := knowledge.Proposal{
		ID:        "01DUP",
		Source:    knowledge.SourceMCP,
		Status:    knowledge.StatusPending,
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveProposal(ctx, p); err != nil {
		t.Fatalf("first SaveProposal: %v", err)
	}
	if err := repo.SaveProposal(ctx, p); err == nil {
		t.Fatal("saving a duplicate id must fail on the primary key, got nil")
	}
}

func TestRejectProposalGuards(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	// A proposal that does not exist cannot be rejected.
	if err := repo.RejectProposal(ctx, "01MISSING"); !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("reject missing proposal: got %v, want ErrNotFound", err)
	}

	p := knowledge.Proposal{
		ID:        "01P",
		Source:    knowledge.SourceMCP,
		Status:    knowledge.StatusPending,
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveProposal(ctx, p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if err := repo.RejectProposal(ctx, p.ID); err != nil {
		t.Fatalf("first RejectProposal: %v", err)
	}

	// Deciding an already-decided proposal is refused.
	if err := repo.RejectProposal(ctx, p.ID); !errors.Is(err, knowledge.ErrNotPending) {
		t.Errorf("re-reject decided proposal: got %v, want ErrNotPending", err)
	}
}
