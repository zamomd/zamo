package logseq_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/logseq"
)

const fixture = "../testdata/logseq"

func TestDetect(t *testing.T) {
	var a logseq.Adapter
	if !a.Detect(fixture) {
		t.Error("Detect(fixture) = false, want true")
	}
	if a.Detect(t.TempDir()) {
		t.Error("Detect(empty dir) = true, want false")
	}
	if a.Detect("../testdata/obsidian") {
		t.Error("Detect(obsidian vault) = true, want false")
	}
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a logseq.Adapter
	v, err := a.Parse(t.Context(), fixture)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v.Format != "Logseq graph" {
		t.Errorf("Format = %q", v.Format)
	}
	pages := make(map[string]vaultimport.Page, len(v.Pages))
	for _, p := range v.Pages {
		if p.Mode != knowledge.ModeOutline {
			t.Errorf("%s: Mode = %q, want outline", p.Name, p.Mode)
		}
		pages[p.Name] = p
	}
	return pages
}

func TestParseNamesPages(t *testing.T) {
	pages := parse(t)
	want := []string{"Go", "Actual Name", "lang/go", "a/b", "Spaces", "Prose", "Refs", "Jan 15th, 2024"}
	for _, name := range want {
		if _, ok := pages[name]; !ok {
			t.Errorf("no page named %q", name)
		}
	}
	if len(pages) != len(want) {
		t.Errorf("got %d pages, want %d", len(pages), len(want))
	}
	if p := pages["Jan 15th, 2024"]; p.Path != "journals/2024_01_15.md" {
		t.Errorf("journal Path = %q", p.Path)
	}
}

func TestParseFollowsTheGraphConfig(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "logseq"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "pages"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "journals"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "logseq", "config.edn"),
		[]byte(`{:journal/page-title-format "yyyy-MM-dd"}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "pages", "lang.go.md"), []byte("- legacy dot namespace\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "journals", "2024_01_15.md"), []byte("- entry\n"), 0o644))

	var a logseq.Adapter
	v, err := a.Parse(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range v.Pages {
		names = append(names, p.Name)
	}
	want := []string{"2024-01-15", "lang/go"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v: a graph without :file/name-format is legacy", names, want)
	}
}

func TestParseRequiresASpaceAfterThePropertySeparator(t *testing.T) {
	// mldoc reads "key:: value" and a bare "key::" line as properties;
	// "key::value" is text.
	dir := t.TempDir()
	for _, sub := range []string{"logseq", "pages"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "pages", "P.md"),
		[]byte("- a\n  type::language\n- b\n  Type-Of:: thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var a logseq.Adapter
	v, err := a.Parse(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if kinds := kindsOf(v.Pages[0]); !reflect.DeepEqual(kinds, []string{vaultimport.WarnProperties}) {
		t.Errorf("warnings = %v, want one properties warning, for block b only", v.Pages[0].Warnings)
	}
}

func node(content string, children ...vaultimport.Node) vaultimport.Node {
	return vaultimport.Node{Content: content, Children: children}
}

func TestParseBuildsTheTreeFromTabIndentedBullets(t *testing.T) {
	got := parse(t)["Go"].Blocks
	contexts := node("Contexts\ncarry deadlines")
	contexts.RefID = "64f1c2a0-0000-4000-8000-000000000001"
	want := []vaultimport.Node{
		node("Go is a language",
			contexts,
			node("Channels [[tag]] and [[multi word]]"),
		),
		node("Back to [[Home]]"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Blocks = %#v\nwant %#v", got, want)
	}
}

func TestParseBuildsTheTreeFromSpaceIndentedBullets(t *testing.T) {
	got := parse(t)["Spaces"].Blocks
	want := []vaultimport.Node{
		node("parent", node("child", node("grandchild"))),
		node("sibling"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Blocks = %#v\nwant %#v", got, want)
	}
}

func TestParseTakesTheNameFromTheTitleProperty(t *testing.T) {
	p := parse(t)["Actual Name"]
	want := []vaultimport.Node{node("type:: language"), node("body")}
	if !reflect.DeepEqual(p.Blocks, want) {
		t.Errorf("Blocks = %#v\nwant %#v", p.Blocks, want)
	}
	if kinds := kindsOf(p); !reflect.DeepEqual(kinds, []string{vaultimport.WarnProperties}) {
		t.Errorf("warning kinds = %v, want [properties]", kinds)
	}
}

func TestParseKeepsProseBeforeTheFirstBullet(t *testing.T) {
	p := parse(t)["Prose"]
	want := []vaultimport.Node{node("Some prose first.\n\nSecond paragraph."), node("then a bullet")}
	if !reflect.DeepEqual(p.Blocks, want) {
		t.Errorf("Blocks = %#v\nwant %#v", p.Blocks, want)
	}
	if kinds := kindsOf(p); !reflect.DeepEqual(kinds, []string{vaultimport.WarnProseBeforeBullets}) {
		t.Errorf("warning kinds = %v, want [prose-before-bullets]", kinds)
	}
}

func TestParseMarksBlockReferencesForThePlanner(t *testing.T) {
	p := parse(t)["Refs"]
	want := "see ((ref:64f1c2a0-0000-4000-8000-000000000001)) and ((ref:64f1c2a0-0000-4000-8000-00000000dead))"
	if len(p.Blocks) != 1 || p.Blocks[0].Content != want {
		t.Errorf("Blocks = %#v\nwant one block %q", p.Blocks, want)
	}
}

func TestParseReportsTheIDPropertyOnlyThroughRefID(t *testing.T) {
	p := parse(t)["Go"]
	if kinds := kindsOf(p); len(kinds) != 0 {
		t.Errorf("warning kinds = %v, want none: an id:: property is structure, not text", kinds)
	}
}

func kindsOf(p vaultimport.Page) []string {
	var kinds []string
	for _, w := range p.Warnings {
		if w.Page != p.Name {
			panic("warning without its page")
		}
		kinds = append(kinds, w.Kind)
	}
	return kinds
}
