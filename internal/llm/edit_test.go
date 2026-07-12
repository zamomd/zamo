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
	// An add without a temp_id still needs one so later ops (and the assembler)
	// can reference the new block.
	ops, err := ParseEditOps([]byte(`[{"op":"add","parent":"01A","content":"x"}]`))
	if err != nil {
		t.Fatalf("ParseEditOps: %v", err)
	}
	if len(ops) != 1 || ops[0].TempID == "" {
		t.Errorf("ops = %+v, want a generated temp id", ops)
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
		{"op":"move","block_id":"01B"},
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
