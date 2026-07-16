package knowledge

import (
	"fmt"
	"testing"
)

// benchProposal builds a proposal of n adds forming a chain (each block parents
// the next), the shape that stresses assemble's cascade, cycle check, and
// parent-before-child ordering the hardest — a chain is the deepest dependency
// path, so ordering has the most work to do.
func benchProposal(n int) Proposal {
	ops := make([]Op, n)
	for i := range n {
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf("t%d", i-1)
		}
		ops[i] = addOp(fmt.Sprintf("t%d", i), parent, fmt.Sprintf("block %d", i))
	}
	return Proposal{Ops: ops}
}

// benchSubtrees builds topics subtrees of blocksPer blocks each, pre-ordered
// (root first, then children), the shape Repository.GetSubtree returns and
// RenderMarkdown consumes. Half the children are nested a level deeper so the
// depth walk and indentation do real work; one block per topic is multi-line so
// the continuation-alignment path is exercised.
func benchSubtrees(topics, blocksPer int) [][]Block {
	subtrees := make([][]Block, topics)
	for t := range topics {
		sub := make([]Block, blocksPer)
		root := ID(fmt.Sprintf("t%d-root", t))
		sub[0] = Block{ID: root, Content: fmt.Sprintf("Topic %d overview", t)}
		for i := 1; i < blocksPer; i++ {
			parent := root
			if i%2 == 0 {
				parent = sub[i-1].ID // nest under the previous child
			}
			content := fmt.Sprintf("block %d of topic %d", i, t)
			if i == 1 {
				content += "\nsecond line kept aligned under the bullet"
			}
			sub[i] = Block{ID: ID(fmt.Sprintf("t%d-b%d", t, i)), ParentID: parent, Position: i, Content: content}
		}
		subtrees[t] = sub
	}
	return subtrees
}

func BenchmarkRenderMarkdown(b *testing.B) {
	subtrees := benchSubtrees(20, 25) // ~500 blocks, an export-sized base
	b.ReportAllocs()
	for b.Loop() {
		if files := RenderMarkdown(subtrees); len(files) != 20 {
			b.Fatalf("RenderMarkdown: got %d files, want 20", len(files))
		}
	}
}

func BenchmarkAssemble(b *testing.B) {
	p := benchProposal(60)
	selected := make([]int, len(p.Ops))
	for i := range selected {
		selected[i] = i
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := assemble(p, selected, seqIDs()); err != nil {
			b.Fatalf("assemble: %v", err)
		}
	}
}
