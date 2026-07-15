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
