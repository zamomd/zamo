package llm

import (
	"errors"
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func TestParseEditOpsMapsEveryKind(t *testing.T) {
	raw := []byte(`[
		{"op":"edit","block_id":"01A","content":"revised"},
		{"op":"add","temp_id":"t1","parent":"01A","content":"new child"},
		{"op":"move","block_id":"01B","parent":"01A"},
		{"op":"delete","block_id":"01C"}
	]`)
	ops, err := ParseEditOps(raw)
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 4 {
		t.Fatalf("got %d ops, want 4: %+v", len(ops), ops)
	}
	if ops[0].Kind != knowledge.OpEdit || ops[0].BlockID != "01A" || ops[0].Content != "revised" {
		t.Errorf("edit op = %+v", ops[0])
	}
	if ops[1].Kind != knowledge.OpAdd || ops[1].TempID != "t1" || ops[1].Parent != "01A" || ops[1].Content != "new child" {
		t.Errorf("add op = %+v", ops[1])
	}
	if ops[2].Kind != knowledge.OpMove || ops[2].BlockID != "01B" || ops[2].Parent != "01A" {
		t.Errorf("move op = %+v", ops[2])
	}
	if ops[3].Kind != knowledge.OpDelete || ops[3].BlockID != "01C" {
		t.Errorf("delete op = %+v", ops[3])
	}
}

func TestParseEditOpsReadsArrayAmidProse(t *testing.T) {
	raw := []byte("Sure! Here are the changes:\n```json\n" +
		`[{"op":"delete","block_id":"01C"}]` + "\n```\nLet me know.")
	ops, err := ParseEditOps(raw)
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != knowledge.OpDelete {
		t.Errorf("ops = %+v, want one delete", ops)
	}
}

func TestParseEditOpsGeneratesTempIDForAdd(t *testing.T) {
	// Adds without a temp_id still need one so later ops (and the assembler) can
	// reference the new block. Generated ids count up (t1, t2), stay distinct,
	// and do not clobber an id the model did supply.
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","parent":"01A","content":"first"},
		{"op":"add","temp_id":"keep","parent":"01A","content":"given"},
		{"op":"add","parent":"01A","content":"second"}
	]`))
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 3 {
		t.Fatalf("got %d ops, want 3", len(ops))
	}
	if ops[0].TempID != "t1" || ops[1].TempID != "keep" || ops[2].TempID != "t2" {
		t.Errorf("temp ids = %q,%q,%q, want t1, keep, t2",
			ops[0].TempID, ops[1].TempID, ops[2].TempID)
	}
}

func TestParseEditOpsDropsInvalidOps(t *testing.T) {
	// Each op kind has required fields; an op missing one, or of an unknown
	// kind, is dropped rather than failing the whole reply. One valid op
	// survives to prove filtering, not a blanket reject.
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
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != knowledge.OpDelete || ops[0].BlockID != "01C" {
		t.Errorf("ops = %+v, want only the one valid delete", ops)
	}
}

func TestParseEditOpsMoveToRootKeepsEmptyParent(t *testing.T) {
	// The domain treats an empty parent as the topic root and supports moving a
	// block there; the parser must not be stricter than the pipeline it feeds
	// and silently drop a valid move.
	ops, err := ParseEditOps([]byte(`[{"op":"move","block_id":"01A"}]`))
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != knowledge.OpMove || ops[0].BlockID != "01A" || ops[0].Parent != "" {
		t.Errorf("ops = %+v, want a move of 01A to the root", ops)
	}
}

func TestParseEditOpsGeneratedTempIDAvoidsSupplied(t *testing.T) {
	// A generated temp id must not collide with one the model supplied on
	// another op, or two adds fight for one id and the whole proposal becomes
	// unappliable.
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","temp_id":"t1","parent":"01A","content":"supplied"},
		{"op":"add","parent":"01A","content":"generated"}
	]`))
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2", len(ops))
	}
	if ops[0].TempID != "t1" {
		t.Errorf("supplied temp id = %q, want t1", ops[0].TempID)
	}
	if ops[1].TempID == "" || ops[1].TempID == ops[0].TempID {
		t.Errorf("generated temp id = %q, want a fresh id distinct from t1", ops[1].TempID)
	}
}

func TestParseEditOpsDropsDuplicateSuppliedTempID(t *testing.T) {
	// The later of two adds sharing a temp id is dropped like any other
	// invalid op; the rest of the reply stays usable.
	ops, err := ParseEditOps([]byte(`[
		{"op":"add","temp_id":"t1","parent":"01A","content":"first"},
		{"op":"add","temp_id":"t1","parent":"01A","content":"dup"},
		{"op":"delete","block_id":"01B"}
	]`))
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 2 || ops[0].Content != "first" || ops[1].Kind != knowledge.OpDelete {
		t.Errorf("ops = %+v, want the first t1 add and the delete only", ops)
	}
}

func TestParseEditOpsSkipsEmptyArrayShadow(t *testing.T) {
	// An empty array earlier in the reply must not shadow the real one, the way
	// firstJSONObject skips a bare {}.
	raw := []byte(`Options were [] but here are the changes: [{"op":"delete","block_id":"01C"}]`)
	ops, err := ParseEditOps(raw)
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != knowledge.OpDelete || ops[0].BlockID != "01C" {
		t.Errorf("ops = %+v, want the real delete, not the empty array", ops)
	}
}

func TestParseEditOpsNoUsableOpsIsBadResponse(t *testing.T) {
	cases := map[string]string{
		"empty array":   `[]`,
		"all invalid":   `[{"op":"edit"},{"op":"nope"}]`,
		"no json array": "I cannot help with that.",
		"object only":   `{"content":"not an array"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			ops, err := ParseEditOps([]byte(body))
			if !errors.Is(err, knowledge.ErrLLMBadResponse) {
				t.Errorf("error = %v, want ErrLLMBadResponse", err)
			}
			if ops != nil {
				t.Errorf("ops = %+v, want nil", ops)
			}
		})
	}
}

func TestParseEditOpsRejectsHostileReply(t *testing.T) {
	t.Run("too large", func(t *testing.T) {
		huge := append([]byte(`[{"op":"delete","block_id":"01C"}]`), make([]byte, maxReplyBytes)...)
		if _, err := ParseEditOps(huge); !errors.Is(err, knowledge.ErrLLMBadResponse) {
			t.Errorf("error = %v, want ErrLLMBadResponse", err)
		}
	})
	t.Run("too nested", func(t *testing.T) {
		nested := []byte(strings.Repeat("[", maxNestDepth+2))
		if _, err := ParseEditOps(nested); !errors.Is(err, knowledge.ErrLLMBadResponse) {
			t.Errorf("error = %v, want ErrLLMBadResponse", err)
		}
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
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != maxBlocks {
		t.Errorf("got %d ops, want the cap of %d", len(ops), maxBlocks)
	}
}
