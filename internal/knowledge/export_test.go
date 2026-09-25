package knowledge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blk(id, parent string, pos int, content string) Block {
	return Block{ID: ID(id), ParentID: ID(parent), Position: pos, Content: content}
}

func TestRenderMarkdownNestsByDepth(t *testing.T) {
	sub := []Block{
		blk("t1", "", 0, "# Go Contexts"),
		blk("t2", "t1", 0, "context.Context carries deadlines"),
		blk("t3", "t2", 0, "WithCancel returns a cancel func"),
		blk("t4", "t1", 1, "Never store a Context in a struct"),
	}
	files := RenderMarkdown([][]Block{sub})
	require.Len(t, files, 1)
	want := "- # Go Contexts\n" +
		"  - context.Context carries deadlines\n" +
		"    - WithCancel returns a cancel func\n" +
		"  - Never store a Context in a struct\n"
	assert.Equal(t, want, files[0].Content)
}

// Obsidian resolves a wikilink by exact basename, so the filename has to be
// the topic's name for an exported [[Topic]] to work at all.
func TestRenderMarkdownNamesFileAfterTopic(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"# Go Contexts", "Go Contexts.md"},
		{"Café Résumé", "Café Résumé.md"},
		{"Local-first architecture", "Local-first architecture.md"},
		{"C++ vs Rust", "C++ vs Rust.md"},
		// Only what a filesystem refuses is replaced.
		{"TCP/IP", "TCP-IP.md"},
		{`Who? What: "Why"`, "Who- What- -Why-.md"},
		// A leading dot would hide the page; a trailing one Windows drops.
		{".hidden", "hidden.md"},
		{"Etc.", "Etc.md"},
	}
	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			files := RenderMarkdown([][]Block{{blk("t1", "", 0, tc.content)}})
			assert.Equal(t, tc.want, files[0].Name)
		})
	}
}

func TestRenderMarkdownDedupesCollidingNames(t *testing.T) {
	// Two topic names differing only in case cannot both exist — the unique
	// index on title_key refuses the second — but on a case-insensitive
	// filesystem they would be one file, so the export refuses to rely on that.
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "# Go Contexts")},
		{blk("t2", "", 0, "Go contexts!")},
		{blk("t3", "", 0, "GO CONTEXTS")},
	})
	got := []string{files[0].Name, files[1].Name, files[2].Name}
	want := []string{"Go Contexts.md", "Go contexts!.md", "GO CONTEXTS-2.md"}
	assert.Equal(t, want, got)
}

func TestRenderMarkdownDedupeAvoidsSuffixCollision(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "x 2")},
		{blk("t2", "", 0, "x")},
		{blk("t3", "", 0, "x")},
	})
	seen := map[string]bool{}
	for _, f := range files {
		assert.False(t, seen[f.Name], "duplicate filename %q — a topic would be overwritten", f.Name)
		seen[f.Name] = true
	}
	assert.Len(t, seen, 3)
}

func TestRenderMarkdownFallsBackOnAnUnusableName(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "###")},
		{blk("t2", "", 0, "   ")},
		{blk("t3", "", 0, "...")},
	})
	got := []string{files[0].Name, files[1].Name, files[2].Name}
	want := []string{"Untitled.md", "Untitled-2.md", "Untitled-3.md"}
	assert.Equal(t, want, got)
}

func TestRenderMarkdownIndentsMultilineContent(t *testing.T) {
	sub := []Block{
		blk("t1", "", 0, "# Root"),
		blk("t2", "t1", 0, "first line\nsecond line"),
	}
	files := RenderMarkdown([][]Block{sub})
	want := "- # Root\n" +
		"  - first line\n" +
		"    second line\n"
	assert.Equal(t, want, files[0].Content)
}

func TestRenderMarkdownEmptyInput(t *testing.T) {
	assert.Empty(t, RenderMarkdown(nil))
}

func TestRenderMarkdownSingleTopicNoChildren(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, "# Lonely")}})
	require.Len(t, files, 1)
	assert.Equal(t, "- # Lonely", strings.TrimSpace(files[0].Content), "want a single root bullet")
}

func TestRenderMarkdownWritesADocumentVerbatim(t *testing.T) {
	doc := "# Go Contexts\n\nPassed first. See [[Go Idioms]].\n\n## Cancellation\n\n- ctx.Done()\n"
	files := RenderMarkdown([][]Block{{{ID: "t", Content: doc, Mode: ModeDoc}}})
	require.Len(t, files, 1)
	assert.Equal(t, "Go Contexts.md", files[0].Name)
	assert.Equal(t, doc, files[0].Content)
}

func TestRenderMarkdownEndsADocumentWithANewline(t *testing.T) {
	// A topic created as a document and never edited is one line with no
	// terminator, and the outline branch always writes one. An exported file
	// without a final newline is a text file most tools call malformed.
	for _, doc := range []string{"Go Contexts", "Go Contexts\n\n\n"} {
		files := RenderMarkdown([][]Block{{{ID: "t", Content: doc, Mode: ModeDoc}}})
		assert.Equal(t, "Go Contexts\n", files[0].Content, "RenderMarkdown of %q", doc)
	}
}

func TestMarkdownMarkerCannotCollideWithATopicFile(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, MarkdownMarkerName)}})
	require.NotEqual(t, MarkdownMarkerName, files[0].Name, "topic file could shadow the marker")
	require.False(t, strings.HasPrefix(files[0].Name, "."), "topic file %q could shadow the marker", files[0].Name)
	m := MarkdownMarker()
	require.Equal(t, MarkdownMarkerName, m.Name)
	require.True(t, strings.HasPrefix(m.Content, "comind markdown "), "marker = %+v", m)
}
