package obsidian_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/obsidian"
)

const fixture = "../testdata/obsidian"

func TestDetect(t *testing.T) {
	var a obsidian.Adapter
	if !a.Detect(fixture) {
		t.Error("Detect(fixture) = false, want true")
	}
	if a.Detect(t.TempDir()) {
		t.Error("Detect(empty dir) = true, want false")
	}
	if a.Detect("../testdata/logseq") {
		t.Error("Detect(logseq graph) = true, want false")
	}
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a obsidian.Adapter
	v, err := a.Parse(t.Context(), fixture)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v.Format != "Obsidian vault" {
		t.Errorf("Format = %q", v.Format)
	}
	pages := make(map[string]vaultimport.Page, len(v.Pages))
	for _, p := range v.Pages {
		pages[p.Name] = p
	}
	return pages
}

func TestParseNamesPagesByBasename(t *testing.T) {
	pages := parse(t)
	want := []string{"Home", "Notes", "Go", "go", "Weird [name]"}
	for _, name := range want {
		if _, ok := pages[name]; !ok {
			t.Errorf("no page named %q; got %v", name, keys(pages))
		}
	}
	if len(pages) != len(want) {
		t.Errorf("got %d pages %v, want %d: dot directories and non-markdown files are skipped",
			len(pages), keys(pages), len(want))
	}
	if p := pages["go"]; p.Path != "archive/go.md" {
		t.Errorf("Path = %q, want slash-separated relative path", p.Path)
	}
}

func TestParseKeepsEveryPageAsADocument(t *testing.T) {
	for name, p := range parse(t) {
		if p.Mode != knowledge.ModeDoc {
			t.Errorf("%s: Mode = %q, want doc", name, p.Mode)
		}
		if p.Blocks != nil {
			t.Errorf("%s: Blocks = %v, want none", name, p.Blocks)
		}
	}
}

func TestParseRewritesLinksAndKeepsFrontmatter(t *testing.T) {
	home := parse(t)["Home"]
	want := "---\ntags: [home]\n---\n# Home\n\nWelcome. See [[Go]] and [[Notes]].\n\n" +
		"[[diagram.png]]\n\nMissing: [[Nowhere]].\n"
	if home.Doc != want {
		t.Errorf("Doc = %q\nwant %q", home.Doc, want)
	}
	var kinds []string
	for _, w := range home.Warnings {
		kinds = append(kinds, w.Kind)
		if w.Page != "Home" {
			t.Errorf("warning %+v carries page %q, want Home", w, w.Page)
		}
	}
	wantKinds := []string{
		vaultimport.WarnFrontmatter, vaultimport.WarnAlias, vaultimport.WarnAnchor, vaultimport.WarnEmbed,
	}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("warning kinds = %v, want %v", kinds, wantKinds)
	}
	for i := range kinds {
		if kinds[i] != wantKinds[i] {
			t.Errorf("warning kinds = %v, want %v", kinds, wantKinds)
		}
	}
}

func TestParseReducesPathAndExtensionLinksToTheBasename(t *testing.T) {
	notes := parse(t)["Notes"]
	want := "# Notes\n\n## Ideas\n\n- one, see [[Go]] and [[Home]]\n- two\n"
	if notes.Doc != want {
		t.Errorf("Doc = %q\nwant %q", notes.Doc, want)
	}
	if kinds := kindsOf(notes); !reflect.DeepEqual(kinds, []string{vaultimport.WarnPath}) {
		t.Errorf("warning kinds = %v, want [path]: an extension is dropped silently, a folder path with a warning", kinds)
	}
}

func kindsOf(p vaultimport.Page) []string {
	var kinds []string
	for _, w := range p.Warnings {
		kinds = append(kinds, w.Kind)
	}
	return kinds
}

func TestParseSortsPagesByPath(t *testing.T) {
	var a obsidian.Adapter
	v, err := a.Parse(t.Context(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(v.Pages); i++ {
		if v.Pages[i-1].Path > v.Pages[i].Path {
			t.Fatalf("pages out of path order: %q before %q", v.Pages[i-1].Path, v.Pages[i].Path)
		}
	}
}

func TestParseFailsOnAMissingDirectory(t *testing.T) {
	var a obsidian.Adapter
	if _, err := a.Parse(t.Context(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Parse(missing dir) = nil error")
	}
}

func keys(m map[string]vaultimport.Page) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
