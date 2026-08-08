package knowledge

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
)

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

func TestPrepareOpsResolvesTempIDs(t *testing.T) {
	seq := 0
	newID := func() ID { seq++; return ID(fmt.Sprintf("id-%d", seq)) }
	ops := []Op{
		{Kind: OpAdd, TempID: "child", Parent: "root"},
		{Kind: OpAdd, TempID: "root"},
	}
	out, ids, err := prepareOps(ops, newID)
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	if out[0].Kind != OpAdd || out[0].Parent != "" {
		t.Fatalf("op 0 = %+v, want the root add first (parent-first order)", out[0])
	}
	for i, op := range out {
		if op.TempID != "" {
			t.Errorf("op %d still carries a temp id %q", i, op.TempID)
		}
	}
	if out[1].Parent != string(ids["root"]) {
		t.Errorf("child parent = %+v, want resolved to ids[root]", out[1])
	}
	if len(ids) != 2 {
		t.Errorf("ids = %+v, want exactly 2 entries (root, child)", ids)
	}
}

func TestPrepareOpsRejectsCycle(t *testing.T) {
	ops := []Op{
		{Kind: OpMove, BlockID: "a", Parent: "b"},
		{Kind: OpMove, BlockID: "b", Parent: "a"},
	}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestPrepareOpsRejectsEmpty(t *testing.T) {
	if _, _, err := prepareOps(nil, seqIDs()); !errors.Is(err, ErrEmptyOps) {
		t.Errorf("error = %v, want ErrEmptyOps", err)
	}
}

func TestPrepareOpsRejectsDuplicateTemp(t *testing.T) {
	ops := []Op{addOp("t1", "", "A"), addOp("t1", "", "B")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
}

func TestPrepareOpsRejectsAddWithEmptyTempID(t *testing.T) {
	ops := []Op{addOp("", "", "no temp id")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("empty temp id: got %v, want ErrInvalidOps", err)
	}
}

func TestPrepareOpsRejectsCycleAmongTempIDs(t *testing.T) {
	ops := []Op{
		addOp("t1", "t2", "A"),
		addOp("t2", "t1", "B"),
	}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestPrepareOpsRejectsMoveIntoOwnSubtree(t *testing.T) {
	ops := []Op{
		addOp("t1", "X", "new child of X"),
		{Kind: OpMove, BlockID: "X", Parent: "t1"},
	}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestPrepareOpsRejectsSelfParent(t *testing.T) {
	ops := []Op{addOp("t1", "t1", "self")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("self-parent: got %v, want ErrCycle", err)
	}
}

func TestPrepareOpsAllowsDiamondUnderExistingParent(t *testing.T) {
	ops := []Op{addOp("t1", "X", "c1"), addOp("t2", "X", "c2")}
	got, _, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("diamond wrongly rejected: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d ops, want 2", len(got))
	}
}

func TestPrepareOpsOrdersMoveAfterItsNewParent(t *testing.T) {
	ops := []Op{
		{Kind: OpMove, BlockID: "X", Parent: "t1"},
		addOp("t1", "", "new parent"),
	}
	got, _, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	if got[0].Kind != OpAdd || got[1].Kind != OpMove {
		t.Fatalf("want add before move, got %+v", got)
	}
	if string(got[0].BlockID) != "U1" || got[1].Parent != "U1" {
		t.Errorf("move parent not resolved to the new id: %+v", got)
	}
}

func TestPrepareOpsOrdersParentsBeforeChildren(t *testing.T) {
	ops := []Op{
		addOp("t3", "t2", "Grandchild"),
		addOp("t2", "t1", "Child"),
		addOp("t1", "", "# Root"),
	}
	got, _, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	order := []string{"# Root", "Child", "Grandchild"}
	for i, want := range order {
		if got[i].Content != want {
			t.Fatalf("op %d = %q, want %q (whole: %+v)", i, got[i].Content, want, got)
		}
	}
	if string(got[0].BlockID) != "U1" || got[0].Parent != "" {
		t.Errorf("root = %+v, want BlockID U1, no parent", got[0])
	}
	if got[1].Parent != "U1" || got[2].Parent != string(got[1].BlockID) {
		t.Errorf("child chain not linked to resolved parents: %+v", got)
	}
}

func TestPrepareOpsResolvesAndOrders(t *testing.T) {
	ops := []Op{
		addOp("t1", "", "# Root"),
		addOp("t2", "t1", "Child A"),
		addOp("t3", "t2", "Grandchild"),
	}
	got, ids, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d ops, want 3: %+v", len(got), got)
	}
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
	if len(ids) != 3 {
		t.Errorf("ids = %+v, want 3 entries", ids)
	}
}

func TestPrepareOpsResolvesAnchors(t *testing.T) {
	ops := []Op{
		{Kind: OpAdd, TempID: "t1", Parent: "root", Content: "first"},
		{Kind: OpAdd, TempID: "t2", Parent: "root", After: "t1", Content: "second"},
		{Kind: OpAdd, TempID: "t3", Parent: "root", Before: "t1", Content: "zeroth"},
	}
	seq := 0
	out, ids, err := prepareOps(ops, func() ID { seq++; return ID(fmt.Sprintf("id-%d", seq)) })
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	for _, op := range out {
		switch op.Content {
		case "second":
			if op.After != string(ids["t1"]) {
				t.Errorf("after = %q, want %q", op.After, ids["t1"])
			}
		case "zeroth":
			if op.Before != string(ids["t1"]) {
				t.Errorf("before = %q, want %q", op.Before, ids["t1"])
			}
		}
	}
}

func TestPrepareOpsOrdersAnchorAfterItsAdd(t *testing.T) {
	ops := []Op{
		{Kind: OpAdd, TempID: "t2", Parent: "root", After: "t1", Content: "second"},
		{Kind: OpAdd, TempID: "t1", Parent: "root", Content: "first"},
	}
	seq := 0
	out, _, err := prepareOps(ops, func() ID { seq++; return ID(fmt.Sprintf("id-%d", seq)) })
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	if out[0].Content != "first" {
		t.Errorf("first op = %q, want the anchor's own add (%q)", out[0].Content, "first")
	}
}

func TestOpValidateRejectsBothAnchors(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "t1", Parent: "root", After: "a", Before: "b"}
	err := op.validate()
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("validate error = %v, want ErrInvalidOps", err)
	}
}

func TestOpValidateRejectsAnAnchorOnAKindThatIgnoresIt(t *testing.T) {
	op := Op{Kind: OpEdit, BlockID: "b1", After: "x"}
	err := op.validate()
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("validate error = %v, want ErrInvalidOps", err)
	}
}

func TestOpValidateRefusesModeOnAChild(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "a", Parent: "root", Mode: ModeDoc}
	if err := op.validate(); !errors.Is(err, ErrInvalidOps) {
		t.Fatalf("validate = %v, want ErrInvalidOps", err)
	}
}

func TestOpValidateRefusesAnUnknownMode(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "a", Mode: Mode("prose")}
	if err := op.validate(); !errors.Is(err, ErrInvalidOps) {
		t.Fatalf("validate = %v, want ErrInvalidOps", err)
	}
}

func TestOpValidateRefusesModeOnAMove(t *testing.T) {
	op := Op{Kind: OpMove, BlockID: "b", Mode: ModeDoc}
	if err := op.validate(); !errors.Is(err, ErrInvalidOps) {
		t.Fatalf("validate = %v, want ErrInvalidOps", err)
	}
}
