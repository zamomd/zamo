package knowledge

import "strings"

// RenderDocument renders a topic subtree as one markdown document: the root's
// own text verbatim, then its descendants as a nested bullet list. The first
// line is untouched, so the document names the topic exactly as the outline did
// and every [[Topic]] reference to it keeps resolving.
func RenderDocument(sub []Block) string {
	if len(sub) == 0 {
		return ""
	}
	root := strings.TrimRight(sub[0].Content, "\n") + "\n"
	if len(sub) == 1 {
		return root
	}
	return root + "\n" + renderChildren(sub)
}

// DocChild is one block a document splits into: its markdown text and how deep
// it sits below the topic root, 0 being a direct child.
type DocChild struct {
	Text  string
	Depth int
}

// SplitDocument splits a document into the topic root's own first line, kept
// verbatim so the split renames nothing, and the blocks its body becomes.
//
// Headings and list items carry the nesting: a heading opens a level by its
// depth below the body's shallowest heading, a list item by its indent.
// Everything else is a leaf at the level it is written under, and blank lines
// separate leaves. A fenced code block is never split — it travels whole,
// blank lines and all — and a list item's unmarked continuation lines rejoin
// the item, so a multi-line block survives a conversion.
//
// One ambiguity is inherent to the bullet format rather than to this function:
// a block whose own later line begins with a list marker ("- ", "* " or "+ ")
// renders indistinguishably from a nested child and comes back as one. The
// format is the export format, which Obsidian and Logseq read, and resolving
// the ambiguity would mean leaving it.
func SplitDocument(content string) (root string, children []DocChild) {
	lines := strings.Split(content, "\n")
	root = strings.TrimSpace(lines[0])
	base := baseHeadingLevel(lines[1:])

	var out []DocChild
	var buf []string
	depth := 0
	// The list item still open for continuation lines, and the column its text
	// starts at. A block's later lines are written there with no marker of
	// their own, so the column is the only thing that reattaches them.
	item, column := -1, 0
	flush := func() {
		text := strings.TrimRight(strings.Join(buf, "\n"), "\n")
		buf = nil
		if strings.TrimSpace(text) == "" {
			return
		}
		out = append(out, DocChild{Text: text, Depth: depth})
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		// While an item is open, a line indented to its text column belongs to
		// it whatever the line looks like — bullet indents every later line of
		// a block by exactly that much, so the indent, not the content, is the
		// signal. This is checked before any other classification because a
		// block's own blank line, heading or code fence would otherwise be read
		// as document structure and cut the block apart. Only a nested list
		// item outranks it, and that is the one ambiguity the format cannot
		// resolve.
		if item >= 0 && leadingSpaces(line) >= column && !listItem(line) {
			out[item].Text += "\n" + unindent(line, column)
			continue
		}
		if fenced := fenceEnd(lines, i); fenced > i {
			flush()
			item = -1
			out = append(out, DocChild{Text: strings.Join(lines[i:fenced+1], "\n"), Depth: depth})
			i = fenced
			continue
		}
		switch {
		case listItem(line):
			flush()
			indent, text := listParts(line)
			out = append(out, DocChild{Text: text, Depth: depth + indent})
			item, column = len(out)-1, leadingSpaces(line)+2
		case strings.TrimSpace(line) == "":
			flush()
			item = -1
		case headingLevel(line) > 0:
			flush()
			item = -1
			depth = max(headingLevel(line)-base, 0)
			out = append(out, DocChild{Text: strings.TrimSpace(line), Depth: depth})
			depth++
		default:
			item = -1
			buf = append(buf, line)
		}
	}
	flush()
	return root, out
}

// leadingSpaces counts the spaces a line is indented by.
func leadingSpaces(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// unindent removes n leading spaces, or every one the line has if it has fewer.
func unindent(line string, n int) string {
	if leadingSpaces(line) < n {
		return strings.TrimLeft(line, " ")
	}
	return line[n:]
}

// baseHeadingLevel is the shallowest ATX heading level in a document body: the
// level its top-level sections are written at. Nesting is relative to it, so a
// document whose sections are `##` under a `# Title` splits the same way as one
// whose sections are `#`. A body with no heading answers 1, which leaves the
// first heading to appear opening depth 0.
func baseHeadingLevel(body []string) int {
	base := 0
	for i := 0; i < len(body); i++ {
		if end := fenceEnd(body, i); end > i {
			i = end // a `#` inside a fence is code, not a heading
			continue
		}
		if n := headingLevel(body[i]); n > 0 && (base == 0 || n < base) {
			base = n
		}
	}
	if base == 0 {
		return 1
	}
	return base
}

// headingLevel reports the `#` count of an ATX heading, or 0 for a line that is
// not one.
func headingLevel(line string) int {
	trimmed := strings.TrimLeft(line, " ")
	n := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if n == 0 || n > 6 || !strings.HasPrefix(trimmed[n:], " ") {
		return 0
	}
	return n
}

// listItem reports whether the line opens a bullet list item.
func listItem(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	return strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
		strings.HasPrefix(trimmed, "+ ")
}

// listParts splits a bullet line into its nesting level — two spaces per level,
// as RenderDocument writes it — and the text after the marker.
func listParts(line string) (indent int, text string) {
	trimmed := strings.TrimLeft(line, " ")
	return (len(line) - len(trimmed)) / 2, strings.TrimSpace(trimmed[2:])
}

// fenceEnd reports the index of the line closing the code fence opened at
// start, or start itself when the line opens no fence. An unterminated fence
// runs to the end of the document, which is how a markdown reader takes it too.
func fenceEnd(lines []string, start int) int {
	marker := fenceMarker(lines[start])
	if marker == "" {
		return start
	}
	for i := start + 1; i < len(lines); i++ {
		if fenceMarker(lines[i]) == marker {
			return i
		}
	}
	return len(lines) - 1
}

// fenceMarker returns the run of backticks or tildes a line opens or closes a
// fence with, or an empty string for an ordinary line.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	for _, r := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, r) {
			return r
		}
	}
	return ""
}
