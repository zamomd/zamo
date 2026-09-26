package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func TestParseEditOpsMapsEveryKind(t *testing.T) {
	raw := []byte(`[
		{"op":"edit","block_id":"01A","content":"revised"},
		{"op":"add","temp_id":"t1","parent":"01A","content":"new child"},
		{"op":"move","block_id":"01B","parent":"01A"},
		{"op":"delete","block_id":"01C"}
	]`)
	ops, err := ParseEditOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 4)
	assert.Equal(t, knowledge.OpEdit, ops[0].Kind)
	assert.Equal(t, knowledge.ID("01A"), ops[0].BlockID)
	assert.Equal(t, "revised", ops[0].Content)
	assert.Equal(t, knowledge.OpAdd, ops[1].Kind)
	assert.Equal(t, "t1", ops[1].TempID)
	assert.Equal(t, "01A", ops[1].Parent)
	assert.Equal(t, "new child", ops[1].Content)
	assert.Equal(t, knowledge.OpMove, ops[2].Kind)
	assert.Equal(t, knowledge.ID("01B"), ops[2].BlockID)
	assert.Equal(t, "01A", ops[2].Parent)
	assert.Equal(t, knowledge.OpDelete, ops[3].Kind)
	assert.Equal(t, knowledge.ID("01C"), ops[3].BlockID)
}

func TestParseEditOpsReadsArrayAmidProse(t *testing.T) {
	raw := []byte("Sure! Here are the changes:\n```json\n" +
		`[{"op":"delete","block_id":"01C"}]` + "\n```\nLet me know.")
	ops, err := ParseEditOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, knowledge.OpDelete, ops[0].Kind)
}

func TestParseEditOpsGeneratesTempIDForAdd(t *testing.T) {
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","parent":"01A","content":"first"},
		{"op":"add","temp_id":"keep","parent":"01A","content":"given"},
		{"op":"add","parent":"01A","content":"second"}
	]`))
	require.NoError(t, err)
	require.Len(t, ops, 3)
	assert.Equal(t, "t1", ops[0].TempID)
	assert.Equal(t, "keep", ops[1].TempID)
	assert.Equal(t, "t2", ops[2].TempID)
}

func TestParseEditOpsDropsInvalidOps(t *testing.T) {
	raw := []byte(`[
		{"op":"frobnicate","block_id":"01A"},
		{"op":"edit","block_id":"01A"},
		{"op":"edit","content":"no target"},
		{"op":"move","parent":"01A"},
		{"op":"delete"},
		{"op":"add","content":""},
		{"op":"delete","block_id":"01C"}
	]`)
	ops, err := ParseEditOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1, "want only the one valid delete")
	assert.Equal(t, knowledge.OpDelete, ops[0].Kind)
	assert.Equal(t, knowledge.ID("01C"), ops[0].BlockID)
}

func TestParseEditOpsMoveToRootKeepsEmptyParent(t *testing.T) {
	ops, err := ParseEditOps([]byte(`[{"op":"move","block_id":"01A"}]`))
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, knowledge.OpMove, ops[0].Kind)
	assert.Equal(t, knowledge.ID("01A"), ops[0].BlockID)
	assert.Empty(t, ops[0].Parent, "want a move to the root")
}

func TestParseEditOpsGeneratedTempIDAvoidsSupplied(t *testing.T) {
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","temp_id":"t1","parent":"01A","content":"supplied"},
		{"op":"add","parent":"01A","content":"generated"}
	]`))
	require.NoError(t, err)
	require.Len(t, ops, 2)
	assert.Equal(t, "t1", ops[0].TempID, "supplied temp id")
	assert.NotEmpty(t, ops[1].TempID, "generated temp id")
	assert.NotEqual(t, ops[0].TempID, ops[1].TempID, "generated temp id must be distinct from the supplied one")
}

func TestParseEditOpsDropsDuplicateSuppliedTempID(t *testing.T) {
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","temp_id":"t1","parent":"01A","content":"first"},
		{"op":"add","temp_id":"t1","parent":"01A","content":"dup"},
		{"op":"delete","block_id":"01B"}
	]`))
	require.NoError(t, err)
	require.Len(t, ops, 2, "want the first t1 add and the delete only")
	assert.Equal(t, "first", ops[0].Content)
	assert.Equal(t, knowledge.OpDelete, ops[1].Kind)
}

func TestParseEditOpsSkipsEmptyArrayShadow(t *testing.T) {
	raw := []byte(`Options were [] but here are the changes: [{"op":"delete","block_id":"01C"}]`)
	ops, err := ParseEditOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1, "want the real delete, not the empty array")
	assert.Equal(t, knowledge.OpDelete, ops[0].Kind)
	assert.Equal(t, knowledge.ID("01C"), ops[0].BlockID)
}

func TestParseEditOpsEmptyArrayMeansNoChange(t *testing.T) {
	for name, body := range map[string]string{
		"bare":   `[]`,
		"fenced": "```json\n[]\n```",
	} {
		t.Run(name, func(t *testing.T) {
			ops, err := ParseEditOps([]byte(body))
			require.NoError(t, err)
			assert.Empty(t, ops)
		})
	}
}

func TestParseEditOpsSkipsForeignArrayShadow(t *testing.T) {
	raw := []byte(`Considered [{"id":1}] first. Changes: [{"op":"delete","block_id":"01C"}]`)
	ops, err := ParseEditOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1, "want the real delete, not the foreign array")
	assert.Equal(t, knowledge.OpDelete, ops[0].Kind)
	assert.Equal(t, knowledge.ID("01C"), ops[0].BlockID)
}

func TestParseEditOpsNoUsableOpsIsBadResponse(t *testing.T) {
	cases := map[string]string{
		"all invalid":   `[{"op":"edit"},{"op":"nope"}]`,
		"no json array": "I cannot help with that.",
		"object only":   `{"content":"not an array"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			ops, err := ParseEditOps([]byte(body))
			require.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
			assert.Nil(t, ops)
		})
	}
}

func TestParseEditOpsRejectsHostileReply(t *testing.T) {
	t.Run("too large", func(t *testing.T) {
		huge := append([]byte(`[{"op":"delete","block_id":"01C"}]`), make([]byte, maxReplyBytes)...)
		_, err := ParseEditOps(huge)
		assert.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
	})
	t.Run("too nested", func(t *testing.T) {
		nested := []byte(strings.Repeat("[", maxNestDepth+2))
		_, err := ParseEditOps(nested)
		assert.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
	})
}

func TestParseEditOpsCapsCount(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := range maxBlocks + 20 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"op":"delete","block_id":"01C"}`)
	}
	b.WriteByte(']')
	ops, err := ParseEditOps([]byte(b.String()))
	require.NoError(t, err)
	assert.Len(t, ops, maxBlocks)
}
