package comind_test

import (
	"reflect"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/comind"
)

const fixture = "../testdata/comind"

func TestDetectNeedsTheMarker(t *testing.T) {
	var a comind.Adapter
	if !a.Detect(fixture) {
		t.Error("Detect(fixture) = false")
	}
	for _, other := range []string{"../testdata/obsidian", "../testdata/logseq", t.TempDir()} {
		if a.Detect(other) {
			t.Errorf("Detect(%s) = true", other)
		}
	}
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a comind.Adapter
	v, err := a.Parse(t.Context(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if v.Format != "Comind export" {
		t.Errorf("Format = %q", v.Format)
	}
	pages := map[string]vaultimport.Page{}
	for _, p := range v.Pages {
		if len(p.Warnings) != 0 {
			t.Errorf("%s: warnings %v, want none: the export is lossless", p.Name, p.Warnings)
		}
		pages[p.Name] = p
	}
	return pages
}

func node(content string, children ...vaultimport.Node) vaultimport.Node {
	return vaultimport.Node{Content: content, Children: children}
}

func TestParseTakesNamesFromContentAndSkipsWhatIsNotAPage(t *testing.T) {
	pages := parse(t)
	var names []string
	for name := range pages {
		names = append(names, name)
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %v, want three: notes.txt and sub/ are not pages", names)
	}
	// "Two Roots.md" is named by its first line, as every page is.
	for _, want := range []string{"Go Contexts", "Essay", "first"} {
		if _, ok := pages[want]; !ok {
			t.Errorf("no page %q in %v", want, names)
		}
	}
}

func TestParseReadsAnOutlineWithItsRootWhole(t *testing.T) {
	p := parse(t)["Go Contexts"]
	if p.Mode != knowledge.ModeOutline || p.Path != "Go Contexts.md" {
		t.Fatalf("page = %+v", p)
	}
	if p.Root != "# Go Contexts\nThe root keeps its second line." {
		t.Errorf("Root = %q", p.Root)
	}
	want := []vaultimport.Node{
		node("context.Context carries deadlines",
			node("WithCancel returns a cancel func, see ((01ARZ3NDEKTSV4RRFFQ69G5FAV|cancel))")),
		node("Never store a Context in a struct"),
	}
	if !reflect.DeepEqual(p.Blocks, want) {
		t.Errorf("Blocks = %#v\nwant %#v", p.Blocks, want)
	}
}

func TestParseReadsADocumentWhole(t *testing.T) {
	p := parse(t)["Essay"]
	if p.Mode != knowledge.ModeDoc {
		t.Fatalf("Mode = %q", p.Mode)
	}
	want := "Essay\n\nProse about [[Go Contexts]].\n\n- a list inside the essay\n- stays prose"
	if p.Doc != want {
		t.Errorf("Doc = %q\nwant %q", p.Doc, want)
	}
}

func TestParseTreatsSeveralTopLevelBulletsAsADocument(t *testing.T) {
	p := parse(t)["first"]
	if p.Mode != knowledge.ModeDoc || p.Doc != "- first\n- second" || p.Path != "Two Roots.md" {
		t.Errorf("page = %+v, want a document: the export writes exactly one root bullet", p)
	}
}
