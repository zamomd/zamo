package knowledge

import (
	"errors"
	"fmt"
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

func TestPrepareOpsResolvesTempIDs(t *testing.T) {
	seq := 0
	newID := func() ID { seq++; return ID(fmt.Sprintf("id-%d", seq)) }
	ops := []Op{
		{Kind: OpAdd, TempID: "child", Parent: "root"},
		{Kind: OpAdd, TempID: "root"},
		{Kind: OpLink, From: "child", To: "existing"},
	}
	out, ids, err := prepareOps(ops, newID)
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	// Parent-first: the root add (no Parent) must be emitted before the
	// child add that references it, even though it arrives second.
	if out[0].Kind != OpAdd || out[0].Parent != "" {
		t.Fatalf("op 0 = %+v, want the root add first (parent-first order)", out[0])
	}
	for i, op := range out {
		if op.TempID != "" {
			t.Errorf("op %d still carries a temp id %q", i, op.TempID)
		}
	}
	link := out[2]
	if link.Kind != OpLink || link.From != string(ids["child"]) || link.To != "existing" {
		t.Errorf("link = %+v, want From resolved to ids[child] and To passed through unresolved", link)
	}
	if len(ids) != 2 {
		t.Errorf("ids = %+v, want exactly 2 entries (root, child)", ids)
	}
}

func TestPrepareOpsRejectsCycle(t *testing.T) {
	// A moves under B, B moves under A: a ring through two existing ids.
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
	// Two adds sharing a temp id would collide on resolution — one real id for
	// two blocks. Reject the whole batch rather than resolve ambiguously.
	ops := []Op{addOp("t1", "", "A"), addOp("t1", "", "B")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
}

func TestPrepareOpsRejectsAddWithEmptyTempID(t *testing.T) {
	// An add with no temp id cannot resolve to a real id — it would emit a
	// blank BlockID. Batches reach here from untrusted sources (MCP), so a
	// malformed add is a domain validation error, not silently applied.
	ops := []Op{addOp("", "", "no temp id")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("empty temp id: got %v, want ErrInvalidOps", err)
	}
}

func TestPrepareOpsRejectsCycleAmongTempIDs(t *testing.T) {
	// Two adds each claiming the other as parent form a ring with no root — the
	// tree cannot be built, so prepareOps must refuse it with ErrCycle.
	ops := []Op{
		addOp("t1", "t2", "A"),
		addOp("t2", "t1", "B"),
	}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestPrepareOpsRejectsMoveIntoOwnSubtree(t *testing.T) {
	// A new block is parented under an existing block X, and the same batch
	// moves X under that new block: X would sit inside its own subtree. The
	// ring runs through an existing id, so it is still ErrCycle.
	ops := []Op{
		addOp("t1", "X", "new child of X"),
		{Kind: OpMove, BlockID: "X", Parent: "t1"},
	}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("error = %v, want ErrCycle", err)
	}
}

func TestPrepareOpsRejectsSelfParent(t *testing.T) {
	// A block claiming itself as parent is a one-node ring.
	ops := []Op{addOp("t1", "t1", "self")}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrCycle) {
		t.Errorf("self-parent: got %v, want ErrCycle", err)
	}
}

func TestPrepareOpsAllowsDiamondUnderExistingParent(t *testing.T) {
	// Two new children of the same existing block X share a parent — a diamond
	// in id-space, but no ring, so it must not be flagged as a cycle.
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
	// A move onto a block created in the same batch must be emitted after that
	// add and carry the resolved parent id, even when it comes first in the
	// input.
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

func TestPrepareOpsResolvesLinkEndpoints(t *testing.T) {
	// A link between two new blocks is emitted after both adds, with From/To
	// resolved to their new ids.
	ops := []Op{
		addOp("t1", "", "A"),
		addOp("t2", "", "B"),
		{Kind: OpLink, From: "t1", To: "t2", Relation: RelationRelated},
	}
	got, _, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	link := got[2]
	if link.Kind != OpLink || link.From != "U1" || link.To != "U2" {
		t.Errorf("link endpoints not resolved: %+v", link)
	}
}

func TestPrepareOpsResolvesUnlinkEndpoints(t *testing.T) {
	// Unlink is a first-class op: it validates on from/to and its endpoints
	// resolve through temp ids exactly like a link's, so an unlink can name
	// blocks added earlier in the same batch.
	ops := []Op{
		addOp("t1", "", "A"),
		addOp("t2", "", "B"),
		{Kind: OpUnlink, From: "t1", To: "t2", Relation: RelationRelated},
	}
	got, _, err := prepareOps(ops, seqIDs())
	if err != nil {
		t.Fatalf("prepareOps: %v", err)
	}
	un := got[2]
	if un.Kind != OpUnlink || un.From != "U1" || un.To != "U2" {
		t.Errorf("unlink endpoints not resolved: %+v", un)
	}
}

func TestPrepareOpsRejectsUnlinkWithoutEndpoints(t *testing.T) {
	ops := []Op{{Kind: OpUnlink, From: "", To: "x", Relation: RelationRelated}}
	if _, _, err := prepareOps(ops, seqIDs()); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("unlink with empty from: got %v, want ErrInvalidOps", err)
	}
}

func TestPrepareOpsOrdersParentsBeforeChildren(t *testing.T) {
	// The ops arrive child-first (an MCP client need not pre-order them);
	// prepareOps must topologically order parents before children and assign
	// ids in that order, so a parent always exists when its child is written.
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
	// Ids follow the emitted order, and each child points at its parent's id.
	if string(got[0].BlockID) != "U1" || got[0].Parent != "" {
		t.Errorf("root = %+v, want BlockID U1, no parent", got[0])
	}
	if got[1].Parent != "U1" || got[2].Parent != string(got[1].BlockID) {
		t.Errorf("child chain not linked to resolved parents: %+v", got)
	}
}

func TestPrepareOpsResolvesAndOrders(t *testing.T) {
	// A small add-tree. temp ids resolve to fresh ids (from the injected
	// generator) in add order, parents keep pointing at their (now resolved)
	// parent, and the result stays parents-before-children.
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
