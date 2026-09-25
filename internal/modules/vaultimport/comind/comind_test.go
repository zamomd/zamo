package comind_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/comind"
)

const fixture = "../testdata/comind"

func TestDetectNeedsTheMarker(t *testing.T) {
	var a comind.Adapter
	assert.True(t, a.Detect(fixture), "Detect(fixture)")
	for _, other := range []string{"../testdata/obsidian", "../testdata/logseq", t.TempDir()} {
		assert.False(t, a.Detect(other), "Detect(%s)", other)
	}
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a comind.Adapter
	v, err := a.Parse(t.Context(), fixture)
	require.NoError(t, err)
	assert.Equal(t, "Comind export", v.Format)
	pages := map[string]vaultimport.Page{}
	for _, p := range v.Pages {
		assert.Empty(t, p.Warnings, "%s: the export is lossless", p.Name)
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
	require.Len(t, pages, 3, "pages = %v, want three: notes.txt and sub/ are not pages", names)
	// "Two Roots.md" is named by its first line, as every page is.
	for _, want := range []string{"Go Contexts", "Essay", "first"} {
		assert.Contains(t, pages, want, "pages = %v", names)
	}
}

func TestParseReadsAnOutlineWithItsRootWhole(t *testing.T) {
	p := parse(t)["Go Contexts"]
	require.Equal(t, knowledge.ModeOutline, p.Mode)
	require.Equal(t, "Go Contexts.md", p.Path)
	assert.Equal(t, "# Go Contexts\nThe root keeps its second line.", p.Root)
	want := []vaultimport.Node{
		node("context.Context carries deadlines",
			node("WithCancel returns a cancel func, see ((01ARZ3NDEKTSV4RRFFQ69G5FAV|cancel))")),
		node("Never store a Context in a struct"),
	}
	assert.Equal(t, want, p.Blocks)
}

func TestParseReadsADocumentWhole(t *testing.T) {
	p := parse(t)["Essay"]
	require.Equal(t, knowledge.ModeDoc, p.Mode)
	want := "Essay\n\nProse about [[Go Contexts]].\n\n- a list inside the essay\n- stays prose"
	assert.Equal(t, want, p.Doc)
}

func TestParseTreatsSeveralTopLevelBulletsAsADocument(t *testing.T) {
	p := parse(t)["first"]
	assert.Equal(t, knowledge.ModeDoc, p.Mode, "the export writes exactly one root bullet")
	assert.Equal(t, "- first\n- second", p.Doc)
	assert.Equal(t, "Two Roots.md", p.Path)
}
