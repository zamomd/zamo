package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
)

// editOp is the shape ParseEditOps expects from the model: one restructuring
// operation. Existing blocks are referenced by block_id, new blocks by temp_id;
// parent is a block_id or a temp_id (empty means the topic root).
type editOp struct {
	Op      string `json:"op"`
	TempID  string `json:"temp_id"`
	Parent  string `json:"parent"`
	BlockID string `json:"block_id"`
	Content string `json:"content"`
}

// ParseEditOps parses a model's restructuring reply — a JSON array of
// add/edit/move/delete operations — into domain ops. Like ParseOps it reads the
// first JSON array embedded in the reply (ignoring fences and prose), refuses a
// reply that is oversized or degenerately nested before decoding, and drops any
// structurally-invalid operation (unknown kind, missing required field) rather
// than failing the whole reply. The list is capped at maxBlocks. A reply with no
// usable operation yields ErrLLMBadResponse. Deeper invariants — the block
// exists, the move makes no cycle — are the assembler's, not the parser's.
func ParseEditOps(raw []byte) ([]knowledge.Op, error) {
	if len(raw) > maxReplyBytes {
		return nil, fmt.Errorf("%w: reply too large (%d bytes)", knowledge.ErrLLMBadResponse, len(raw))
	}
	if tooDeeplyNested(raw) {
		return nil, fmt.Errorf("%w: reply nested past %d levels", knowledge.ErrLLMBadResponse, maxNestDepth)
	}
	edits, ok := firstJSONArray(raw)
	if !ok {
		return nil, fmt.Errorf("%w: no JSON array in reply", knowledge.ErrLLMBadResponse)
	}

	ops := make([]knowledge.Op, 0, len(edits))
	seq := 0
	for _, e := range edits {
		if len(ops) >= maxBlocks {
			break
		}
		if op, ok := e.toOp(&seq); ok {
			ops = append(ops, op)
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: reply had no usable operations", knowledge.ErrLLMBadResponse)
	}
	return ops, nil
}

// toOp validates one edit op by kind and maps it to a domain op. seq feeds
// generated temp ids for adds that omit one, so later ops (and the assembler)
// can still reference the new block. It returns false for an unknown kind or a
// missing required field, dropping the op.
func (e editOp) toOp(seq *int) (knowledge.Op, bool) {
	content := strings.TrimSpace(e.Content)
	switch knowledge.OpKind(e.Op) {
	case knowledge.OpAdd:
		if content == "" {
			return knowledge.Op{}, false
		}
		tempID := e.TempID
		if tempID == "" {
			*seq++
			tempID = "t" + strconv.Itoa(*seq)
		}
		return knowledge.Op{Kind: knowledge.OpAdd, TempID: tempID, Parent: e.Parent, Content: content}, true
	case knowledge.OpEdit:
		if e.BlockID == "" || content == "" {
			return knowledge.Op{}, false
		}
		return knowledge.Op{Kind: knowledge.OpEdit, BlockID: knowledge.ID(e.BlockID), Content: content}, true
	case knowledge.OpMove:
		if e.BlockID == "" || e.Parent == "" {
			return knowledge.Op{}, false
		}
		return knowledge.Op{Kind: knowledge.OpMove, BlockID: knowledge.ID(e.BlockID), Parent: e.Parent}, true
	case knowledge.OpDelete:
		if e.BlockID == "" {
			return knowledge.Op{}, false
		}
		return knowledge.Op{Kind: knowledge.OpDelete, BlockID: knowledge.ID(e.BlockID)}, true
	default:
		return knowledge.Op{}, false
	}
}

// firstJSONArray scans for the first '[' that decodes into a JSON array of edit
// ops, ignoring markdown fences, prose and trailing text around it. A bracket
// that does not begin a well-formed array (prose like "step [1]") is skipped.
func firstJSONArray(raw []byte) ([]editOp, bool) {
	for i := range raw {
		if raw[i] != '[' {
			continue
		}
		var edits []editOp
		if json.NewDecoder(bytes.NewReader(raw[i:])).Decode(&edits) != nil {
			continue
		}
		return edits, true
	}
	return nil, false
}
