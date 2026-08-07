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
	// The same shape written one level up: sections at `#` rather than `##`
	// must split identically, because nesting is relative to the body's own
	// shallowest heading rather than to the absolute `#` count.
	_, children := SplitDocument("Go Contexts\n\nIntro paragraph.\n\n# Cancellation\n\nctx.Done() closes.\n")
	want := []DocChild{
		{Text: "Intro paragraph.", Depth: 0},
		{Text: "# Cancellation", Depth: 0},
		{Text: "ctx.Done() closes.", Depth: 1},
	}
	if !slices.Equal(children, want) {
		t.Errorf("children = %#v, want %#v", children, want)
	}
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
