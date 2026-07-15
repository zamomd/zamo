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
