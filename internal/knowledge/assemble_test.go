package knowledge

import (
	"errors"
	"strconv"
	"testing"
)

// seqIDs returns a generator handing out deterministic ids U1, U2, … so tests
// can predict the temp_id → ID resolution.
func seqIDs() IDGenerator {
	n := 0
	return func() ID {
		n++
		return ID("U" + strconv.Itoa(n))
	}
}

func addOp(temp, parent, content string) Op {
	return Op{Kind: OpAdd, TempID: temp, Parent: parent, Content: content}
}

func TestAssembleRejectsCycleAmongTempIDs(t *testing.T) {
	// Two adds each claiming the other as parent form a ring with no root — the
	// tree cannot be built, so assemble must refuse it with ErrCycle.
	p := Proposal{
		Ops: []Op{
			addOp("t1", "t2", "A"),
			addOp("t2", "t1", "B"),
		},
	}
	_, err := assemble(p, []int{0, 1}, seqIDs())
	if !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestAssembleRejectsMoveIntoOwnSubtree(t *testing.T) {
	// A new block is parented under an existing block X, and the same proposal
	// moves X under that new block: X would sit inside its own subtree. The ring
	// runs through an existing id, so it is still ErrCycle.
	p := Proposal{
		Ops: []Op{
			addOp("t1", "X", "new child of X"),
			{Kind: OpMove, BlockID: "X", Parent: "t1"},
		},
	}
	_, err := assemble(p, []int{0, 1}, seqIDs())
	if !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestAssembleOrdersParentsBeforeChildren(t *testing.T) {
	// The ops arrive child-first (an MCP client need not pre-order them); assemble
	// must topologically order parents before children and assign ids in that
	// order, so a parent always exists when its child is written.
	p := Proposal{
		Ops: []Op{
			addOp("t3", "t2", "Grandchild"),
			addOp("t2", "t1", "Child"),
			addOp("t1", "", "# Root"),
		},
	}
	got, err := assemble(p, []int{0, 1, 2}, seqIDs())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	order := []string{"# Root", "Child", "Grandchild"}
	for i, want := range order {
		if got[i].Content != want {
			t.Fatalf("op %d = %q, want %q (whole: %+v)", i, got[i].Content, want, got)
		}
	}
	// Ids follow the emitted order, and each child points at its parent's id.
	if string(got[0].BlockID) != "U1" || got[0].Parent != "" {
		t.Errorf("root = %+v, want BlockID U1, no parent", got[0])
	}
	if got[1].Parent != "U1" || got[2].Parent != string(got[1].BlockID) {
		t.Errorf("child chain not linked to resolved parents: %+v", got)
	}
}

func TestAssembleResolvesAndOrders(t *testing.T) {
	// A small add-tree, fully selected. temp ids resolve to fresh ULIDs in add
	// order, parents keep pointing at their (now resolved) parent, and the result
	// stays parents-before-children.
	p := Proposal{
		Ops: []Op{
			addOp("t1", "", "# Root"),
			addOp("t2", "t1", "Child A"),
			addOp("t3", "t2", "Grandchild"),
		},
	}
	got, err := assemble(p, []int{0, 1, 2}, seqIDs())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d ops, want 3: %+v", len(got), got)
	}
	// Resolution: temp ids are gone, BlockID carries the new ULID.
	want := []struct {
		block, parent, content string
	}{
		{"U1", "", "# Root"},
		{"U2", "U1", "Child A"},
		{"U3", "U2", "Grandchild"},
	}
	for i, w := range want {
		if got[i].TempID != "" {
			t.Errorf("op %d still carries a temp id %q", i, got[i].TempID)
		}
		if string(got[i].BlockID) != w.block {
			t.Errorf("op %d BlockID = %q, want %q", i, got[i].BlockID, w.block)
		}
		if got[i].Parent != w.parent {
			t.Errorf("op %d Parent = %q, want %q", i, got[i].Parent, w.parent)
		}
		if got[i].Content != w.content {
			t.Errorf("op %d Content = %q, want %q", i, got[i].Content, w.content)
		}
	}
}
