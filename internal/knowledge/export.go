package knowledge

import (
	"strconv"
	"strings"
)

// ExportFile is one rendered markdown document: a filename (unique within an
// export) and its content. Writing it to disk is the caller's job.
type ExportFile struct {
	Name    string
	Content string
}

// MarkdownMarkerName is the file an export writes beside its topic files, so
// the directory can be told apart from any other folder of markdown when it
// comes back through import. A topic file can never take the name: leading
// dots are trimmed from every topic filename.
const MarkdownMarkerName = ".zamo-export"

// MarkdownMarker is the marker file. Its content names the format and the
// version of its layout, for an import to check.
func MarkdownMarker() ExportFile {
	return ExportFile{Name: MarkdownMarkerName, Content: "zamo markdown 1\n"}
}

// untitledName is the filename base for a topic whose first line yields no
// usable name. Deduplication keeps concurrent fallbacks distinct.
const untitledName = "Untitled"

// illegalInFilename is the union of the characters POSIX, macOS and Windows
// refuse in a filename. Everything else — spaces, case, punctuation, any
// script — survives, because the filename carries the topic's name.
const illegalInFilename = `/\:*?"<>|`

// RenderMarkdown renders each topic subtree into a portable markdown document.
// Every subtree is the pre-order block walk of one topic (root first, parents
// before children), exactly as Repository.GetSubtree returns it. Blocks become
// a Logseq-style nested bullet list — two spaces of indent per level — that
// Obsidian reads with the hierarchy intact. Each file is named after its topic
// and de-duplicated within the returned set so no file is overwritten. Content
// is assumed to be clean markdown with no service markup.
func RenderMarkdown(subtrees [][]Block) []ExportFile {
	files := make([]ExportFile, 0, len(subtrees))
	taken := make(map[string]bool, len(subtrees))
	for _, sub := range subtrees {
		if len(sub) == 0 {
			continue
		}
		name := uniqueName(topicFileBase(sub[0].Content), taken)
		files = append(files, ExportFile{Name: name, Content: renderTopic(sub)})
	}
	return files
}

// renderTopic renders one topic in the shape it has: a document is already the
// markdown its author wrote, an outline becomes a nested bullet list. Either
// way the file ends in exactly one newline, as RenderDocument writes it — a
// document topic is stored as the text the editor holds, which need not end in
// one at all.
func renderTopic(sub []Block) string {
	if sub[0].Mode == ModeDoc {
		return strings.TrimRight(sub[0].Content, "\n") + "\n"
	}
	return renderSubtree(sub)
}

// renderSubtree renders a pre-ordered subtree as an indented bullet list, the
// root included.
func renderSubtree(sub []Block) string {
	return bullet(sub[0].Content, 0) + renderDescendants(sub, 1)
}

// renderChildren renders everything below sub[0] as a bullet list whose top
// level sits at the left margin — the body of a document, which carries the
// root's own text as prose rather than as a bullet.
func renderChildren(sub []Block) string {
	return renderDescendants(sub, 0)
}

// renderDescendants renders sub[1:] as a bullet list whose direct children of
// sub[0] sit at base. Depth is the length of the parent chain up to the root,
// so indentation reflects the tree even though the input is flat.
func renderDescendants(sub []Block, base int) string {
	depth := map[ID]int{sub[0].ID: base - 1}
	var b strings.Builder
	for _, block := range sub[1:] {
		d := depth[block.ParentID] + 1
		depth[block.ID] = d
		b.WriteString(bullet(block.Content, d))
	}
	return b.String()
}

// bullet writes one block as a list item at depth d, keeping the block's extra
// lines aligned under the bullet text so the list stays valid markdown.
func bullet(content string, d int) string {
	indent := strings.Repeat("  ", d)
	lines := strings.Split(content, "\n")
	var b strings.Builder
	b.WriteString(indent + "- " + lines[0] + "\n")
	for _, line := range lines[1:] {
		b.WriteString(indent + "  " + line + "\n")
	}
	return b.String()
}

// topicFileBase turns a topic's content into its filename base: the topic's
// name — the very text a [[Topic]] reference carries — with only the characters
// a filesystem refuses replaced by a hyphen.
//
// In Obsidian and Logseq the filename *is* the page name, and Obsidian resolves
// a wikilink by exact basename, so an exported [[Go Contexts]] finds its page
// only in "Go Contexts.md". A slug would break every wikilink in the export.
// The one case a link still cannot resolve is a name carrying a replaced
// character, and no choice of filename could rescue that.
//
// Leading and trailing dots and spaces go too: Windows strips them silently and
// POSIX hides a leading dot, either of which would turn a page into a file the
// reader never sees. An empty result signals the caller to fall back.
func topicFileBase(content string) string {
	var b strings.Builder
	for _, r := range TopicName(content) {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(illegalInFilename, r) {
			b.WriteByte('-')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), ". ")
}

// uniqueName appends the ".md" extension and disambiguates against names
// already taken with a numeric suffix (Name.md, Name-2.md, Name-3.md …). It
// checks the final filename, not the base, so a suffixed name can never
// collide with another topic's base name and overwrite its file. Matching is
// case-insensitive because the files land on the user's disk, where macOS and
// Windows treat two names differing only in case as one — a pair the unique
// index on title_key already keeps out of the base, so this only has to hold
// for input that never reached storage. An empty base falls back to a fixed
// name first.
func uniqueName(base string, taken map[string]bool) string {
	if base == "" {
		base = untitledName
	}
	name := base + ".md"
	for n := 2; taken[strings.ToLower(name)]; n++ {
		name = base + "-" + strconv.Itoa(n) + ".md"
	}
	taken[strings.ToLower(name)] = true
	return name
}
