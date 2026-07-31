package knowledge

import (
	"reflect"
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
	files := RenderMarkdown([][]Block{sub}, nil)
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

func TestRenderMarkdownSlugsFilenameFromTopic(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, "# Go Contexts")}}, nil)
	if files[0].Name != "go-contexts.md" {
		t.Errorf("name = %q, want go-contexts.md", files[0].Name)
	}
}

func TestRenderMarkdownKeepsUnicodeInSlug(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, "Café Résumé")}}, nil)
	if files[0].Name != "café-résumé.md" {
		t.Errorf("name = %q, want café-résumé.md", files[0].Name)
	}
}

func TestRenderMarkdownDedupesCollidingSlugs(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "# Go Contexts")},
		{blk("t2", "", 0, "Go contexts!")},
		{blk("t3", "", 0, "GO CONTEXTS")},
	}, nil)
	got := []string{files[0].Name, files[1].Name, files[2].Name}
	want := []string{"go-contexts.md", "go-contexts-2.md", "go-contexts-3.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestRenderMarkdownDedupeAvoidsSuffixCollision(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "x 2")},
		{blk("t2", "", 0, "x")},
		{blk("t3", "", 0, "x")},
	}, nil)
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

func TestRenderMarkdownFallsBackOnEmptySlug(t *testing.T) {
	files := RenderMarkdown([][]Block{
		{blk("t1", "", 0, "###")},
		{blk("t2", "", 0, "   ")},
	}, nil)
	if files[0].Name != "untitled.md" || files[1].Name != "untitled-2.md" {
		t.Errorf("names = %q,%q, want untitled.md, untitled-2.md", files[0].Name, files[1].Name)
	}
}

func TestRenderMarkdownIndentsMultilineContent(t *testing.T) {
	sub := []Block{
		blk("t1", "", 0, "# Root"),
		blk("t2", "t1", 0, "first line\nsecond line"),
	}
	files := RenderMarkdown([][]Block{sub}, nil)
	want := "- # Root\n" +
		"  - first line\n" +
		"    second line\n"
	if files[0].Content != want {
		t.Errorf("content mismatch:\n got:\n%q\nwant:\n%q", files[0].Content, want)
	}
}

func TestRenderMarkdownRendersOutgoingLinks(t *testing.T) {
	sub := []Block{
		blk("t1", "", 0, "# Root"),
		blk("t2", "t1", 0, "Goroutine leaks"),
	}
	links := map[ID][]ExportLink{
		"t2": {
			{Relation: RelationExample, Target: "goleak in TestMain"},
			{Relation: RelationRelated, Target: "Context cancellation"},
		},
	}
	files := RenderMarkdown([][]Block{sub}, links)
	want := "- # Root\n" +
		"  - Goroutine leaks\n" +
		"    - links:\n" +
		"      - example → goleak in TestMain\n" +
		"      - related → Context cancellation\n"
	if files[0].Content != want {
		t.Errorf("content mismatch:\n got:\n%q\nwant:\n%q", files[0].Content, want)
	}
}

func TestRenderMarkdownEmptyInput(t *testing.T) {
	if files := RenderMarkdown(nil, nil); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestRenderMarkdownSingleTopicNoChildren(t *testing.T) {
	files := RenderMarkdown([][]Block{{blk("t1", "", 0, "# Lonely")}}, nil)
	if len(files) != 1 || strings.TrimSpace(files[0].Content) != "- # Lonely" {
		t.Errorf("content = %q, want a single root bullet", files[0].Content)
	}
}
