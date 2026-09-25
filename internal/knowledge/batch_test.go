package knowledge

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	require.Equal(t, OpAdd, out[0].Kind, "want the root add first (parent-first order)")
	require.Empty(t, out[0].Parent, "want the root add first (parent-first order)")
	for i, op := range out {
		assert.Empty(t, op.TempID, "op %d still carries a temp id", i)
	}
	assert.Equal(t, string(ids["root"]), out[1].Parent)
	assert.Len(t, ids, 2)
}

func TestPrepareOpsRejectsCycle(t *testing.T) {
	ops := []Op{
		{Kind: OpMove, BlockID: "a", Parent: "b"},
		{Kind: OpMove, BlockID: "b", Parent: "a"},
	}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrCycle)
}

func TestPrepareOpsRejectsEmpty(t *testing.T) {
	_, _, err := prepareOps(nil, seqIDs())
	require.ErrorIs(t, err, ErrEmptyOps)
}

func TestPrepareOpsRejectsDuplicateTemp(t *testing.T) {
	ops := []Op{addOp("t1", "", "A"), addOp("t1", "", "B")}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrInvalidOps)
}

func TestPrepareOpsRejectsAddWithEmptyTempID(t *testing.T) {
	ops := []Op{addOp("", "", "no temp id")}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrInvalidOps)
}

func TestPrepareOpsRejectsCycleAmongTempIDs(t *testing.T) {
	ops := []Op{
		addOp("t1", "t2", "A"),
		addOp("t2", "t1", "B"),
	}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrCycle)
}

func TestPrepareOpsRejectsMoveIntoOwnSubtree(t *testing.T) {
	ops := []Op{
		addOp("t1", "X", "new child of X"),
		{Kind: OpMove, BlockID: "X", Parent: "t1"},
	}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrCycle)
}

func TestPrepareOpsRejectsSelfParent(t *testing.T) {
	ops := []Op{addOp("t1", "t1", "self")}
	_, _, err := prepareOps(ops, seqIDs())
	require.ErrorIs(t, err, ErrCycle)
}

func TestPrepareOpsAllowsDiamondUnderExistingParent(t *testing.T) {
	ops := []Op{addOp("t1", "X", "c1"), addOp("t2", "X", "c2")}
	got, _, err := prepareOps(ops, seqIDs())
	require.NoError(t, err, "diamond wrongly rejected")
	require.Len(t, got, 2)
}

func TestPrepareOpsOrdersMoveAfterItsNewParent(t *testing.T) {
	ops := []Op{
		{Kind: OpMove, BlockID: "X", Parent: "t1"},
		addOp("t1", "", "new parent"),
	}
	got, _, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	require.Equal(t, OpAdd, got[0].Kind, "want add before move")
	require.Equal(t, OpMove, got[1].Kind, "want add before move")
	assert.Equal(t, ID("U1"), got[0].BlockID)
	assert.Equal(t, "U1", got[1].Parent, "move parent not resolved to the new id")
}

func TestPrepareOpsOrdersParentsBeforeChildren(t *testing.T) {
	ops := []Op{
		addOp("t3", "t2", "Grandchild"),
		addOp("t2", "t1", "Child"),
		addOp("t1", "", "# Root"),
	}
	got, _, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	order := []string{"# Root", "Child", "Grandchild"}
	for i, want := range order {
		require.Equal(t, want, got[i].Content, "op %d", i)
	}
	assert.Equal(t, ID("U1"), got[0].BlockID)
	assert.Empty(t, got[0].Parent)
	assert.Equal(t, "U1", got[1].Parent, "child chain not linked to resolved parents")
	assert.Equal(t, string(got[1].BlockID), got[2].Parent, "child chain not linked to resolved parents")
}

func TestPrepareOpsResolvesAndOrders(t *testing.T) {
	ops := []Op{
		addOp("t1", "", "# Root"),
		addOp("t2", "t1", "Child A"),
		addOp("t3", "t2", "Grandchild"),
	}
	got, ids, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	require.Len(t, got, 3)
	want := []struct {
		block, parent, content string
	}{
		{"U1", "", "# Root"},
		{"U2", "U1", "Child A"},
		{"U3", "U2", "Grandchild"},
	}
	for i, w := range want {
		assert.Empty(t, got[i].TempID, "op %d still carries a temp id", i)
		assert.Equal(t, ID(w.block), got[i].BlockID, "op %d", i)
		assert.Equal(t, w.parent, got[i].Parent, "op %d", i)
		assert.Equal(t, w.content, got[i].Content, "op %d", i)
	}
	assert.Len(t, ids, 3)
}

func TestPrepareOpsResolvesAnchors(t *testing.T) {
	ops := []Op{
		{Kind: OpAdd, TempID: "t1", Parent: "root", Content: "first"},
		{Kind: OpAdd, TempID: "t2", Parent: "root", After: "t1", Content: "second"},
		{Kind: OpAdd, TempID: "t3", Parent: "root", Before: "t1", Content: "zeroth"},
	}
	seq := 0
	out, ids, err := prepareOps(ops, func() ID { seq++; return ID(fmt.Sprintf("id-%d", seq)) })
	require.NoError(t, err)
	for _, op := range out {
		switch op.Content {
		case "second":
			assert.Equal(t, string(ids["t1"]), op.After)
		case "zeroth":
			assert.Equal(t, string(ids["t1"]), op.Before)
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
	require.NoError(t, err)
	assert.Equal(t, "first", out[0].Content, "want the anchor's own add first")
}

func TestOpValidateRejectsBothAnchors(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "t1", Parent: "root", After: "a", Before: "b"}
	err := op.validate()
	require.ErrorIs(t, err, ErrInvalidOps)
}

func TestOpValidateRejectsAnAnchorOnAKindThatIgnoresIt(t *testing.T) {
	op := Op{Kind: OpEdit, BlockID: "b1", After: "x"}
	err := op.validate()
	require.ErrorIs(t, err, ErrInvalidOps)
}

func TestOpValidateRefusesModeOnAChild(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "a", Parent: "root", Mode: ModeDoc}
	require.ErrorIs(t, op.validate(), ErrInvalidOps)
}

func TestOpValidateRefusesAnUnknownMode(t *testing.T) {
	op := Op{Kind: OpAdd, TempID: "a", Mode: Mode("prose")}
	require.ErrorIs(t, op.validate(), ErrInvalidOps)
}

func TestOpValidateRefusesModeOnAMove(t *testing.T) {
	op := Op{Kind: OpMove, BlockID: "b", Mode: ModeDoc}
	require.ErrorIs(t, op.validate(), ErrInvalidOps)
}

func TestPrepareOpsRewritesBatchLocalBlockRefs(t *testing.T) {
	ops := []Op{
		addOp("a", "", "see ((b|the other one)) and ((b))"),
		addOp("b", "", "target"),
	}
	out, ids, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	var got string
	for _, op := range out {
		if op.BlockID == ids["a"] {
			got = op.Content
		}
	}
	want := "see ((" + string(ids["b"]) + "|the other one)) and ((" + string(ids["b"]) + "))"
	require.Equal(t, want, got)
}

func TestPrepareOpsLeavesForeignBlockRefsAlone(t *testing.T) {
	const content = "((x|not ours)) ((01ARZ3NDEKTSV4RRFFQ69G5FAV|real)) \\((a|escaped))"
	ops := []Op{addOp("a", "", content)}
	out, _, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	require.Equal(t, content, out[0].Content)
}

func TestPrepareOpsRewritesBlockRefsInEdits(t *testing.T) {
	ops := []Op{
		{Kind: OpEdit, BlockID: "existing", Content: "now points at ((n|new))"},
		addOp("n", "", "new block"),
	}
	out, ids, err := prepareOps(ops, seqIDs())
	require.NoError(t, err)
	var got string
	for _, op := range out {
		if op.Kind == OpEdit {
			got = op.Content
		}
	}
	require.Equal(t, "now points at (("+string(ids["n"])+"|new))", got)
}
