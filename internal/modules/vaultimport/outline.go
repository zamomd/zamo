package vaultimport

import (
	"regexp"
	"strings"
)

// bullet matches a list item opener: its indentation and its text.
var bullet = regexp.MustCompile(`^([ \t]*)[-*](?: (.*))?$`)

// ParseOutline builds a block tree from a markdown bullet list, the shape
// Logseq writes and Zamo's own export writes. A tab is one level; spaces
// are measured against the first indented bullet's width. A line that is not
// a bullet continues the block above it, with the bullet's indentation
// removed, so a block keeps its extra lines. Text above the first bullet is
// returned separately, trimmed of blank lines.
func ParseOutline(raw string) (preamble string, nodes []Node) {
	var (
		lines  []string
		roots  []Node
		unit   int
		lastWS string
	)
	level := func(ws string) int {
		tabs := strings.Count(ws, "\t")
		spaces := len(ws) - tabs
		if spaces > 0 && unit == 0 {
			unit = spaces
		}
		if unit == 0 {
			return tabs
		}
		return tabs + spaces/unit
	}
	// Nodes live in slices that reallocate as siblings arrive, so the path
	// to the current node is kept as indexes into each parent's children.
	type frame struct {
		parent *[]Node
		index  int
	}
	var frames []frame
	current := func() *Node {
		f := frames[len(frames)-1]
		return &(*f.parent)[f.index]
	}
	for line := range strings.SplitSeq(raw, "\n") {
		m := bullet.FindStringSubmatch(line)
		if m == nil {
			if len(frames) == 0 {
				lines = append(lines, line)
				continue
			}
			text, ok := strings.CutPrefix(line, lastWS+"  ")
			if !ok {
				text = strings.TrimLeft(line, " \t")
			}
			current().Content += "\n" + text
			continue
		}
		ws, text := m[1], m[2]
		// A bullet indented deeper than one level below its predecessor is
		// a child of it all the same.
		depth := min(level(ws), len(frames))
		frames = frames[:depth]
		parent := &roots
		if depth > 0 {
			parent = &current().Children
		}
		*parent = append(*parent, Node{Content: text})
		frames = append(frames, frame{parent: parent, index: len(*parent) - 1})
		lastWS = ws
	}
	trimTrailing(roots)
	return strings.Trim(strings.Join(lines, "\n"), "\n"), roots
}

// trimTrailing drops the blank lines a block's content ends in, which are
// the blank lines between it and the next bullet.
func trimTrailing(nodes []Node) {
	for i := range nodes {
		nodes[i].Content = strings.TrimRight(nodes[i].Content, "\n")
		trimTrailing(nodes[i].Children)
	}
}
