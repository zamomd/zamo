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

var _ = errors.Is
