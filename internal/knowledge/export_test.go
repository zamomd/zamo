package knowledge

import (
	"slices"
	"strings"
	"testing"
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
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	want := "- # Go Contexts\n" +
		"  - context.Context carries deadlines\n" +
		"    - WithCancel returns a cancel func\n" +
		"  - Never store a Context in a struct\n"
	if files[0].Content != want {
		t.Errorf("content mismatch:\n got:\n%q\nwant:\n%q", files[0].Content, want)
	}
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
			if files[0].Name != tc.want {
				t.Errorf("name = %q, want %q", files[0].Name, tc.want)
			}
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
	if !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestRenderMarkdownDedupeAvoidsSuffixCollision(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "x 2")},
		{blk("t2", "", 0, "x")},
		{blk("t3", "", 0, "x")},
	})
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f.Name] {
			t.Errorf("duplicate filename %q — a topic would be overwritten", f.Name)
		}
		seen[f.Name] = true
	}
	if len(seen) != 3 {
		t.Errorf("got %d distinct names from 3 topics: %v", len(seen), seen)
	}
}

func TestRenderMarkdownFallsBackOnAnUnusableName(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "###")},
		{blk("t2", "", 0, "   ")},
		{blk("t3", "", 0, "...")},
	})
	got := []string{files[0].Name, files[1].Name, files[2].Name}
	want := []string{"Untitled.md", "Untitled-2.md", "Untitled-3.md"}
	if !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
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
	if files[0].Content != want {
		t.Errorf("content mismatch:\n got:\n%q\nwant:\n%q", files[0].Content, want)
	}
}

func TestRenderMarkdownEmptyInput(t *testing.T) {
	if files := RenderMarkdown(nil); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestRenderMarkdownSingleTopicNoChildren(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, "# Lonely")}})
	if len(files) != 1 || strings.TrimSpace(files[0].Content) != "- # Lonely" {
		t.Errorf("content = %q, want a single root bullet", files[0].Content)
	}
}

func TestRenderMarkdownWritesADocumentVerbatim(t *testing.T) {
	doc := "# Go Contexts\n\nPassed first. See [[Go Idioms]].\n\n## Cancellation\n\n- ctx.Done()\n"
	files := RenderMarkdown([][]Block{{{ID: "t", Content: doc, Mode: ModeDoc}}})
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	if files[0].Name != "Go Contexts.md" {
		t.Errorf("name = %q, want %q", files[0].Name, "Go Contexts.md")
	}
	if files[0].Content != doc {
		t.Errorf("content =\n%q\nwant\n%q", files[0].Content, doc)
	}
}

func TestRenderMarkdownEndsADocumentWithANewline(t *testing.T) {
	// A topic created as a document and never edited is one line with no
	// terminator, and the outline branch always writes one. An exported file
	// without a final newline is a text file most tools call malformed.
	for _, doc := range []string{"Go Contexts", "Go Contexts\n\n\n"} {
		files := RenderMarkdown([][]Block{{{ID: "t", Content: doc, Mode: ModeDoc}}})
		if got := files[0].Content; got != "Go Contexts\n" {
			t.Errorf("RenderMarkdown of %q wrote %q, want %q", doc, got, "Go Contexts\n")
		}
	}
}

func TestMarkdownMarkerCannotCollideWithATopicFile(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, MarkdownMarkerName)}})
	if files[0].Name == MarkdownMarkerName || strings.HasPrefix(files[0].Name, ".") {
		t.Fatalf("topic file %q could shadow the marker", files[0].Name)
	}
	if m := MarkdownMarker(); m.Name != MarkdownMarkerName || !strings.HasPrefix(m.Content, "comind markdown ") {
		t.Fatalf("marker = %+v", m)
	}
}
