package knowledge

import (
	"strconv"
	"strings"
	"unicode"
)

// ExportFile is one rendered markdown document: a filename (unique within an
// export) and its content. Writing it to disk is the caller's job.
type ExportFile struct {
	Name    string
	Content string
}

// untitledSlug is the filename base for a topic whose content yields no
// slug-able characters. Deduplication keeps concurrent fallbacks distinct.
const untitledSlug = "untitled"

// RenderMarkdown renders each topic subtree into a portable markdown document.
// Every subtree is the pre-order block walk of one topic (root first, parents
// before children), exactly as Repository.GetSubtree returns it. Blocks become
// a Logseq-style nested bullet list — two spaces of indent per level — that
// Obsidian reads with the hierarchy intact. Filenames are slugged from the
// topic content and de-duplicated within the returned set so no file is
// overwritten. Content is assumed to be clean markdown with no service markup.
func RenderMarkdown(subtrees [][]Block) []ExportFile {
	files := make([]ExportFile, 0, len(subtrees))
	used := make(map[string]int, len(subtrees))
	for _, sub := range subtrees {
		if len(sub) == 0 {
			continue
		}
		name := uniqueName(topicSlug(sub[0].Content), used)
		files = append(files, ExportFile{Name: name, Content: renderSubtree(sub)})
	}
	return files
}

// renderSubtree renders a pre-ordered subtree as an indented bullet list. Depth
// is the length of the parent chain up to the root, so indentation reflects the
// tree even though the input is flat. Multi-line block content keeps its extra
// lines aligned under the bullet text, so the list stays valid markdown.
func renderSubtree(sub []Block) string {
	depth := make(map[ID]int, len(sub))
	var b strings.Builder
	for i, block := range sub {
		d := 0
		if i > 0 { // the root stays at depth 0; others inherit parent depth + 1
			d = depth[block.ParentID] + 1
		}
		depth[block.ID] = d

		indent := strings.Repeat("  ", d)
		lines := strings.Split(block.Content, "\n")
		b.WriteString(indent + "- " + lines[0] + "\n")
		for _, line := range lines[1:] {
			b.WriteString(indent + "  " + line + "\n")
		}
	}
	return b.String()
}

// topicSlug turns a topic's content into a filename base: the first line,
// lowercased, with every run of non-letter/non-digit runes collapsed to a
// single hyphen. Unicode letters survive, so a base in any language keeps a
// readable name. An empty result signals the caller to fall back.
func topicSlug(content string) string {
	firstLine, _, _ := strings.Cut(content, "\n")
	var b strings.Builder
	pendingHyphen := false
	for _, r := range firstLine {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			pendingHyphen = false
			continue
		}
		pendingHyphen = true
	}
	return b.String()
}

// uniqueName appends the ".md" extension and disambiguates a repeated slug with
// a numeric suffix (name.md, name-2.md, name-3.md …). An empty slug falls back
// to a fixed base first, so the numbering applies to the fallback too.
func uniqueName(slug string, used map[string]int) string {
	if slug == "" {
		slug = untitledSlug
	}
	used[slug]++
	if n := used[slug]; n > 1 {
		return slug + "-" + strconv.Itoa(n) + ".md"
	}
	return slug + ".md"
}
