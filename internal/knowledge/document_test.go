package knowledge

import (
	"slices"
	"testing"
)

func subtree() []Block {
	return []Block{
		{ID: "t", Content: "Go Contexts"},
		{ID: "a", ParentID: "t", Position: 1, Content: "Passed as the first argument.\nAlways first."},
		{ID: "b", ParentID: "t", Position: 2, Content: "## Cancellation"},
		{ID: "c", ParentID: "b", Position: 1, Content: "ctx.Done() closes."},
	}
}

func TestRenderDocumentKeepsTheRootLineAndNestsTheRest(t *testing.T) {
	want := "Go Contexts\n\n- Passed as the first argument.\n  Always first.\n- ## Cancellation\n  - ctx.Done() closes.\n"
	if got := RenderDocument(subtree()); got != want {
		t.Errorf("RenderDocument =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderDocumentOfALoneRootIsItsContent(t *testing.T) {
	if got := RenderDocument([]Block{{ID: "t", Content: "Go Contexts"}}); got != "Go Contexts\n" {
		t.Errorf("RenderDocument = %q, want %q", got, "Go Contexts\n")
	}
}

func TestSplitDocumentNestsByHeadingLevel(t *testing.T) {
	root, children := SplitDocument("# Go Contexts\n\nIntro paragraph.\n\n## Cancellation\n\nctx.Done() closes.\n")
	if root != "# Go Contexts" {
		t.Errorf("root = %q, want %q", root, "# Go Contexts")
	}
	want := []DocChild{
		{Text: "Intro paragraph.", Depth: 0},
		{Text: "## Cancellation", Depth: 0},
		{Text: "ctx.Done() closes.", Depth: 1},
	}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentNestsRelativeToTheShallowestHeading(t *testing.T) {
	// One shape, a section with a subsection, written at two heading levels.
	// Both must nest identically, because a heading opens a level by its depth
	// below the body's own shallowest heading and not by its absolute `#`
	// count. Two levels are needed: at one, every candidate rule agrees.
	const (
		shallow = "Go Contexts\n\n# Cancellation\n\nctx.Done() closes.\n\n## Deadlines\n\nA deadline propagates.\n"
		deep    = "Go Contexts\n\n### Cancellation\n\nctx.Done() closes.\n\n#### Deadlines\n\nA deadline propagates.\n"
	)
	want := []int{0, 1, 1, 2}
	for _, doc := range []string{shallow, deep} {
		_, children := SplitDocument(doc)
		if got := depths(children); !slices.Equal(got, want) {
			t.Errorf("SplitDocument(%q) nests at %v, want %v", doc, got, want)
		}
	}
}

// depths is the nesting of each split block, which is what a comparison across
// two heading levels can share — the text cannot, since it carries the level.
func depths(children []DocChild) []int {
	out := make([]int, len(children))
	for i, c := range children {
		out[i] = c.Depth
	}
	return out
}

func TestSplitDocumentKeepsAFenceWhole(t *testing.T) {
	_, children := SplitDocument("Topic\n\n```go\nfunc main() {}\n\nfmt.Println()\n```\n")
	want := []DocChild{{Text: "```go\nfunc main() {}\n\nfmt.Println()\n```", Depth: 0}}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentReversesRenderDocument(t *testing.T) {
	root, children := SplitDocument(RenderDocument(subtree()))
	if root != "Go Contexts" {
		t.Errorf("root = %q, want %q", root, "Go Contexts")
	}
	want := []DocChild{
		{Text: "Passed as the first argument.\nAlways first.", Depth: 0},
		{Text: "## Cancellation", Depth: 0},
		{Text: "ctx.Done() closes.", Depth: 1},
	}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentKeepsAMultiLineBlockWhole(t *testing.T) {
	// `bullet` writes a block's later lines indented under the bullet text with
	// no marker of their own. Splitting has to put them back, or a multi-line
	// block becomes two blocks on every conversion.
	_, children := SplitDocument("Go Contexts\n\n- line one\n  line two\n- next\n")
	want := []DocChild{
		{Text: "line one\nline two", Depth: 0},
		{Text: "next", Depth: 0},
	}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentKeepsAFenceInsideABlockWithIt(t *testing.T) {
	// A fence written inside a block is the block's own text, not a section of
	// the document. It is indented to the item's column like every other later
	// line, and the indent is what says so.
	_, children := SplitDocument("Go Contexts\n\n- before\n  ```\n  code\n  ```\n  after\n")
	want := []DocChild{{Text: "before\n```\ncode\n```\nafter", Depth: 0}}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentKeepsABlankLineInsideABlock(t *testing.T) {
	// A blank line inside a block renders as the item's text column of spaces,
	// which is what distinguishes it from the blank line that separates two
	// blocks. Losing that reading splits the block and strands the indent.
	_, children := SplitDocument("Go Contexts\n\n- para one\n  \n  para two\n")
	want := []DocChild{{Text: "para one\n\npara two", Depth: 0}}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}

func TestSplitDocumentReversesRenderDocumentOfABlockStartingWithAHeadingLine(t *testing.T) {
	// A block's own "## " line renders indented under the bullet, at the same
	// column headingLevel looks at. Testing for a heading before the
	// continuation rule would misread it as opening a new section.
	sub := []Block{
		{ID: "t", Content: "Go Contexts"},
		{ID: "a", ParentID: "t", Position: 1, Content: "Some text.\n## Not a real heading"},
	}
	_, children := SplitDocument(RenderDocument(sub))
	want := []DocChild{{Text: "Some text.\n## Not a real heading", Depth: 0}}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
}
