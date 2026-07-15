package knowledge

import (
	"errors"
	"testing"
	"time"
)

func TestNewProposalAcceptsEveryWellFormedKind(t *testing.T) {
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	ops := []Op{
		{Kind: OpAdd, TempID: "t1", Content: "A"},
		{Kind: OpEdit, BlockID: "01A", Content: "B"},
		{Kind: OpMove, BlockID: "01A", Parent: "01B"},
		{Kind: OpDelete, BlockID: "01A"},
		{Kind: OpLink, From: "t1", To: "01A"},
	}
	p, err := NewProposal("01P", SourceMCP, "valid", ops, at)
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	if p.Status != StatusPending || len(p.Ops) != len(ops) {
		t.Errorf("proposal = %+v, want pending with all %d ops", p, len(ops))
	}
}

// A proposal that could never apply is refused before it reaches the queue.
func TestNewProposalRejectsMalformedShape(t *testing.T) {
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		title string
		ops   []Op
	}{
		{"unknown kind", "t", []Op{{Kind: "explode", Content: "boom"}}},
		{"empty kind", "t", []Op{{Content: "no kind"}}},
		{"add without temp id", "t", []Op{{Kind: OpAdd, Content: "A"}}},
		{"duplicate temp ids", "t", []Op{
			{Kind: OpAdd, TempID: "t1", Content: "A"},
			{Kind: OpAdd, TempID: "t1", Content: "B"},
		}},
		{"edit without block id", "t", []Op{{Kind: OpEdit, Content: "new"}}},
		{"move without block id", "t", []Op{{Kind: OpMove, Parent: "01B"}}},
		{"delete without block id", "t", []Op{{Kind: OpDelete}}},
		{"link without from", "t", []Op{{Kind: OpLink, To: "01A"}}},
		{"link without to", "t", []Op{{Kind: OpLink, From: "01A"}}},
		{"empty title", "", []Op{{Kind: OpAdd, TempID: "t1", Content: "A"}}},
		{"blank title", "   ", []Op{{Kind: OpAdd, TempID: "t1", Content: "A"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewProposal("01P", SourceMCP, tc.title, tc.ops, at)
			if !errors.Is(err, ErrInvalidProposal) {
				t.Errorf("NewProposal error = %v, want errors.Is ErrInvalidProposal", err)
			}
		})
	}
}

// The queue entry point refuses a malformed op: nothing is saved.
func TestServiceSubmitProposalRejectsMalformedOp(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	_, err := svc.SubmitProposal(t.Context(), SourceMCP, "t", []Op{{Kind: "explode"}})
	if !errors.Is(err, ErrInvalidProposal) {
		t.Errorf("SubmitProposal error = %v, want errors.Is ErrInvalidProposal", err)
	}
	if repo.saved != nil {
		t.Errorf("a malformed proposal must not be saved, got %+v", repo.saved)
	}
}
