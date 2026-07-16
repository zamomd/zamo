package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reviewFixture queues one pending proposal over a base with blocks 01A
// (content "alpha") and 01B, and returns the service and the proposal id.
func reviewFixture(t *testing.T, ops []Op) (*Service, ID) {
	t.Helper()
	repo := &fakeRepo{
		blocks: map[ID]Block{
			"01A": {ID: "01A", Content: "alpha"},
			"01B": {ID: "01B", ParentID: "01A", Content: "beta"},
		},
		pending: []Proposal{{ID: "P1", Status: StatusPending, Title: "t", Ops: ops}},
	}
	return NewService(nil, repo, seqIDs(), clockAt(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))), "P1"
}

func TestReviewProposalAnnotatesEditWithCurrentContent(t *testing.T) {
	svc, id := reviewFixture(t, []Op{{Kind: OpEdit, BlockID: "01A", Content: "new alpha"}})
	got, err := svc.ReviewProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("ReviewProposal: %v", err)
	}
	if len(got.Ops) != 1 || got.Ops[0].Stale || got.Ops[0].CurrentContent != "alpha" {
		t.Errorf("review = %+v, want the edit carrying current content alpha", got.Ops)
	}
	if got.Proposal.ID != id {
		t.Errorf("proposal = %+v, want %s", got.Proposal, id)
	}
}

func TestReviewProposalFlagsStaleRefs(t *testing.T) {
	// Every existing-block reference a kind can carry: a missing target or
	// parent or link end marks the op stale. Temp refs and empty parents never do.
	cases := []struct {
		name string
		op   Op
		want bool
	}{
		{"edit of vanished block", Op{Kind: OpEdit, BlockID: "01GONE", Content: "x"}, true},
		{"delete of vanished block", Op{Kind: OpDelete, BlockID: "01GONE"}, true},
		{"move of vanished block", Op{Kind: OpMove, BlockID: "01GONE", Parent: "01A"}, true},
		{"move under vanished parent", Op{Kind: OpMove, BlockID: "01A", Parent: "01GONE"}, true},
		{"add under vanished parent", Op{Kind: OpAdd, TempID: "t9", Parent: "01GONE", Content: "x"}, true},
		{"link to vanished block", Op{Kind: OpLink, From: "01A", To: "01GONE"}, true},
		{"edit of live block", Op{Kind: OpEdit, BlockID: "01A", Content: "x"}, false},
		{"add under live parent", Op{Kind: OpAdd, TempID: "t9", Parent: "01B", Content: "x"}, false},
		{"add at topic root", Op{Kind: OpAdd, TempID: "t9", Content: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, id := reviewFixture(t, []Op{tc.op})
			got, err := svc.ReviewProposal(context.Background(), id)
			if err != nil {
				t.Fatalf("ReviewProposal: %v", err)
			}
			if got.Ops[0].Stale != tc.want {
				t.Errorf("Stale = %v, want %v", got.Ops[0].Stale, tc.want)
			}
		})
	}
}

func TestReviewProposalTempRefsAreNeverStale(t *testing.T) {
	// Ops referencing blocks created inside the same proposal must not be
	// checked against storage.
	svc, id := reviewFixture(t, []Op{
		{Kind: OpAdd, TempID: "t1", Content: "root"},
		{Kind: OpAdd, TempID: "t2", Parent: "t1", Content: "child"},
		{Kind: OpLink, From: "t1", To: "t2"},
	})
	got, err := svc.ReviewProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("ReviewProposal: %v", err)
	}
	for i, op := range got.Ops {
		if op.Stale {
			t.Errorf("op %d marked stale, want temp refs ignored: %+v", i, op)
		}
	}
}

func TestReviewProposalNotPending(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), clockAt(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)))
	if _, err := svc.ReviewProposal(context.Background(), "NOPE"); !errors.Is(err, ErrNotPending) {
		t.Errorf("error = %v, want ErrNotPending", err)
	}
}
