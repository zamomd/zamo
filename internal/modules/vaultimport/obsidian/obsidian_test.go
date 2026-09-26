package obsidian_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
	"github.com/zamomd/zamo/internal/modules/vaultimport/obsidian"
)

const fixture = "../testdata/obsidian"

func TestDetect(t *testing.T) {
	var a obsidian.Adapter
	assert.True(t, a.Detect(fixture), "Detect(fixture)")
	assert.False(t, a.Detect(t.TempDir()), "Detect(empty dir)")
	assert.False(t, a.Detect("../testdata/logseq"), "Detect(logseq graph)")
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a obsidian.Adapter
	v, err := a.Parse(t.Context(), fixture)
	require.NoError(t, err)
	assert.Equal(t, "Obsidian vault", v.Format)
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
		assert.Contains(t, pages, name, "got %v", keys(pages))
	}
	assert.Len(t, pages, len(want), "got %v: dot directories and non-markdown files are skipped", keys(pages))
	assert.Equal(t, "archive/go.md", pages["go"].Path, "want slash-separated relative path")
}

func TestParseKeepsEveryPageAsADocument(t *testing.T) {
	for name, p := range parse(t) {
		assert.Equal(t, knowledge.ModeDoc, p.Mode, name)
		assert.Nil(t, p.Blocks, name)
	}
}

func TestParseRewritesLinksAndKeepsFrontmatter(t *testing.T) {
	home := parse(t)["Home"]
	want := "---\ntags: [home]\n---\n# Home\n\nWelcome. See [[Go]] and [[Notes]].\n\n" +
		"[[diagram.png]]\n\nMissing: [[Nowhere]].\n"
	assert.Equal(t, want, home.Doc)
	var kinds []string
	for _, w := range home.Warnings {
		kinds = append(kinds, w.Kind)
		assert.Equal(t, "Home", w.Page, "warning %+v", w)
	}
	wantKinds := []string{
		vaultimport.WarnFrontmatter, vaultimport.WarnAlias, vaultimport.WarnAnchor, vaultimport.WarnEmbed,
	}
	assert.Equal(t, wantKinds, kinds)
}

func TestParseReducesPathAndExtensionLinksToTheBasename(t *testing.T) {
	notes := parse(t)["Notes"]
	want := "# Notes\n\n## Ideas\n\n- one, see [[Go]] and [[Home]]\n- two\n"
	assert.Equal(t, want, notes.Doc)
	assert.Equal(t, []string{vaultimport.WarnPath}, kindsOf(notes),
		"an extension is dropped silently, a folder path with a warning")
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
	require.NoError(t, err)
	for i := 1; i < len(v.Pages); i++ {
		require.LessOrEqual(t, v.Pages[i-1].Path, v.Pages[i].Path, "pages out of path order")
	}
}

func TestParseFailsOnAMissingDirectory(t *testing.T) {
	var a obsidian.Adapter
	_, err := a.Parse(t.Context(), filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}

func keys(m map[string]vaultimport.Page) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
