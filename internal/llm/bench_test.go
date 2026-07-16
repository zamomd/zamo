package llm

import (
	"encoding/json"
	"fmt"
	"testing"
)

// benchReply builds a model reply shaped like ParseOps expects: a topic tree of
// {content, children} nodes. width children per node, depth levels deep, which
// keeps the fixture near the parser's caps (maxDepth, maxBlocks) — the realistic
// worst case a real expansion drives through the decoder and the walk.
func benchReply(width, depth int) []byte {
	type node struct {
		Content  string `json:"content"`
		Children []node `json:"children,omitempty"`
	}
	var build func(level, idx int) node
	build = func(level, idx int) node {
		n := node{Content: fmt.Sprintf("block L%d N%d with a sentence of markdown content", level, idx)}
		if level < depth {
			for i := range width {
				n.Children = append(n.Children, build(level+1, i))
			}
		}
		return n
	}
	raw, err := json.Marshal(build(0, 0))
	if err != nil {
		panic(err)
	}
	return raw
}

func BenchmarkParseOps(b *testing.B) {
	reply := benchReply(3, 4)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseOps(reply); err != nil {
			b.Fatalf("ParseOps: %v", err)
		}
	}
}

// benchEditReply builds an edit-command reply: a JSON array of n add ops forming
// a chain (each parents the previous). Called just over maxBlocks, so the parser
// emits the full cap and also runs its truncation break — the realistic worst
// case for the temp-id reservation and dedup passes. It marshals the production
// editOp, so a json-tag change is caught here rather than silently altering the
// measured workload.
func benchEditReply(n int) []byte {
	edits := make([]editOp, n)
	for i := range n {
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf("t%d", i-1)
		}
		edits[i] = editOp{Op: "add", TempID: fmt.Sprintf("t%d", i), Parent: parent, Content: fmt.Sprintf("block %d", i)}
	}
	raw, err := json.Marshal(edits)
	if err != nil {
		panic(err)
	}
	return raw
}

func BenchmarkParseEditOps(b *testing.B) {
	reply := benchEditReply(maxBlocks + 5)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseEditOps(reply); err != nil {
			b.Fatalf("ParseEditOps: %v", err)
		}
	}
}
