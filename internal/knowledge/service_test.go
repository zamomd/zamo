package knowledge

import (
	"context"
	"errors"
	"testing"
)

// fakeRepo is a Repository double: it serves a fixed pending queue and records
// what ApplyProposal was handed, so tests can assert the assembled op list.
type fakeRepo struct {
	pending    []Proposal
	listErr    error
	applyErr   error
	appliedID  ID
	appliedOps []Op
	applyCalls int
}

func (f *fakeRepo) GetTopics(context.Context) ([]Block, error)          { return nil, nil }
func (f *fakeRepo) GetSubtree(context.Context, ID) ([]Block, error)     { return nil, nil }
func (f *fakeRepo) EditBlockContent(context.Context, ID, string) error  { return nil }
func (f *fakeRepo) SaveProposal(context.Context, Proposal) error        { return nil }
func (f *fakeRepo) RejectProposal(context.Context, ID) error            { return nil }
func (f *fakeRepo) Search(context.Context, string) ([]Block, error)     { return nil, nil }

func (f *fakeRepo) ListProposals(context.Context, Status) ([]Proposal, error) {
	return f.pending, f.listErr
}

func (f *fakeRepo) ApplyProposal(_ context.Context, id ID, ops []Op) error {
	f.applyCalls++
	f.appliedID, f.appliedOps = id, ops
	return f.applyErr
}

func TestServiceApplyProposalAssemblesSelection(t *testing.T) {
	repo := &fakeRepo{pending: []Proposal{{
		ID:     "P1",
		Status: StatusPending,
		Ops: []Op{
			addOp("t1", "", "# Root"),
			addOp("t2", "t1", "Child"),
			addOp("t3", "t2", "Grandchild"),
		},
	}}}
	svc := NewService(nil, repo, seqIDs(), nil)

	// Accept the root and child but not the grandchild.
	if err := svc.ApplyProposal(context.Background(), "P1", []int{0, 1}); err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if repo.appliedID != "P1" {
		t.Errorf("applied id = %q, want P1", repo.appliedID)
	}
	if len(repo.appliedOps) != 2 {
		t.Fatalf("applied %d ops, want 2 (grandchild deselected): %+v", len(repo.appliedOps), repo.appliedOps)
	}
	// Storage receives resolved ids, never temp ids.
	if got := repo.appliedOps[0]; got.TempID != "" || string(got.BlockID) != "U1" || got.Parent != "" {
		t.Errorf("root op = %+v, want resolved BlockID U1 at the root", got)
	}
	if got := repo.appliedOps[1]; got.Parent != "U1" {
		t.Errorf("child parent = %q, want the resolved root id U1", got.Parent)
	}
}

func TestServiceApplyProposalNotPending(t *testing.T) {
	repo := &fakeRepo{pending: nil} // the id is not in the review queue
	svc := NewService(nil, repo, seqIDs(), nil)

	err := svc.ApplyProposal(context.Background(), "GONE", []int{0})
	if !errors.Is(err, ErrNotPending) {
		t.Errorf("error = %v, want ErrNotPending", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("storage ApplyProposal called %d times, want 0 for a non-pending id", repo.applyCalls)
	}
}
