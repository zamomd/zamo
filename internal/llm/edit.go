package llm

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
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
// than failing the whole reply. The list is capped at maxBlocks. An empty array
// (the model's "no change needed") yields no ops and no error; a reply with no
// usable array yields ErrLLMBadResponse. Deeper invariants — the block exists,
// the move makes no cycle — are the assembler's, not the parser's.
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
	// The model's "no change needed" answer.
	if len(edits) == 0 {
		return nil, nil
	}

	// Reserve every temp id the model supplied so a generated id for a keyless
	// add cannot collide with one, which would make the whole batch unappliable.
	taken := make(map[string]bool)
	for _, e := range edits {
		if e.TempID != "" {
			taken[e.TempID] = true
		}
	}

	ops := make([]knowledge.Op, 0, len(edits))
	emitted := make(map[string]bool) // temp ids already given to an add
	seq := 0
	for _, e := range edits {
		if len(ops) >= maxBlocks {
			break
		}
		if !e.valid() {
			continue
		}
		op := e.toOp(&seq, taken)
		// A repeated temp id would make the batch unappliable; drop the duplicate.
		if op.Kind == knowledge.OpAdd {
			if emitted[op.TempID] {
				continue
			}
			emitted[op.TempID] = true
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// valid reports whether the op's shape is usable: a known kind carrying the
// fields that kind cannot work without. A move's empty parent means the topic
// root, so it is not required.
func (e editOp) valid() bool {
	content := strings.TrimSpace(e.Content)
	switch knowledge.OpKind(e.Op) {
	case knowledge.OpAdd:
		return content != ""
	case knowledge.OpEdit:
		return e.BlockID != "" && content != ""
	case knowledge.OpMove, knowledge.OpDelete:
		return e.BlockID != ""
	default:
		return false
	}
}

// toOp maps a valid edit op onto a domain op. A keyless add gets a minted temp
// id, skipping any already in taken, so references to the new block cannot
// collide.
func (e editOp) toOp(seq *int, taken map[string]bool) knowledge.Op {
	content := strings.TrimSpace(e.Content)
	switch knowledge.OpKind(e.Op) {
	case knowledge.OpAdd:
		tempID := e.TempID
		if tempID == "" {
			for {
				*seq++
				tempID = "t" + strconv.Itoa(*seq)
				if !taken[tempID] {
					taken[tempID] = true // reserve so the next keyless add differs too
					break
				}
			}
		}
		return knowledge.Op{Kind: knowledge.OpAdd, TempID: tempID, Parent: e.Parent, Content: content}
	case knowledge.OpEdit:
		return knowledge.Op{Kind: knowledge.OpEdit, BlockID: knowledge.ID(e.BlockID), Content: content}
	case knowledge.OpMove:
		return knowledge.Op{Kind: knowledge.OpMove, BlockID: knowledge.ID(e.BlockID), Parent: e.Parent}
	default: // delete — valid admits no other kind
		return knowledge.Op{Kind: knowledge.OpDelete, BlockID: knowledge.ID(e.BlockID)}
	}
}

// firstJSONArray scans for the first '[' that decodes into an array carrying
// at least one usable op, ignoring fences, prose and foreign arrays — an empty
// [] or stray junk must not shadow the real array later in the reply. When the
// only well-formed array is empty (the "no change" reply) it returns (nil, true).
func firstJSONArray(raw []byte) ([]editOp, bool) {
	sawEmpty := false
	for i := range raw {
		if raw[i] != '[' {
			continue
		}
		var edits []editOp
		if json.UnmarshalDecode(jsontext.NewDecoder(bytes.NewReader(raw[i:])), &edits) != nil {
			continue
		}
		if slices.ContainsFunc(edits, editOp.valid) {
			return edits, true
		}
		sawEmpty = sawEmpty || len(edits) == 0
	}
	return nil, sawEmpty
}
