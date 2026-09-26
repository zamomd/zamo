package logseq_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
	"github.com/zamomd/zamo/internal/modules/vaultimport/logseq"
)

const fixture = "../testdata/logseq"

func TestDetect(t *testing.T) {
	var a logseq.Adapter
	assert.True(t, a.Detect(fixture), "Detect(fixture)")
	assert.False(t, a.Detect(t.TempDir()), "Detect(empty dir)")
	assert.False(t, a.Detect("../testdata/obsidian"), "Detect(obsidian vault)")
}

func parse(t *testing.T) map[string]vaultimport.Page {
	t.Helper()
	var a logseq.Adapter
	v, err := a.Parse(t.Context(), fixture)
	require.NoError(t, err)
	assert.Equal(t, "Logseq graph", v.Format)
	pages := make(map[string]vaultimport.Page, len(v.Pages))
	for _, p := range v.Pages {
		assert.Equal(t, knowledge.ModeOutline, p.Mode, p.Name)
		pages[p.Name] = p
	}
	return pages
}

func TestParseNamesPages(t *testing.T) {
	pages := parse(t)
	want := []string{"Go", "Actual Name", "lang/go", "a/b", "Spaces", "Prose", "Refs", "Jan 15th, 2024"}
	for _, name := range want {
		assert.Contains(t, pages, name)
	}
	assert.Len(t, pages, len(want))
	assert.Equal(t, "journals/2024_01_15.md", pages["Jan 15th, 2024"].Path, "journal Path")
}

func TestParseFollowsTheGraphConfig(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		require.NoError(t, err)
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
	require.NoError(t, err)
	var names []string
	for _, p := range v.Pages {
		names = append(names, p.Name)
	}
	want := []string{"2024-01-15", "lang/go"}
	assert.Equal(t, want, names, "a graph without :file/name-format is legacy")
}

func TestParseRequiresASpaceAfterThePropertySeparator(t *testing.T) {
	// mldoc reads "key:: value" and a bare "key::" line as properties;
	// "key::value" is text.
	dir := t.TempDir()
	for _, sub := range []string{"logseq", "pages"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pages", "P.md"),
		[]byte("- a\n  type::language\n- b\n  Type-Of:: thing\n"), 0o644))
	var a logseq.Adapter
	v, err := a.Parse(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, []string{vaultimport.WarnProperties}, kindsOf(v.Pages[0]),
		"want one properties warning, for block b only")
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
	assert.Equal(t, want, got)
}

func TestParseBuildsTheTreeFromSpaceIndentedBullets(t *testing.T) {
	got := parse(t)["Spaces"].Blocks
	want := []vaultimport.Node{
		node("parent", node("child", node("grandchild"))),
		node("sibling"),
	}
	assert.Equal(t, want, got)
}

func TestParseTakesTheNameFromTheTitleProperty(t *testing.T) {
	p := parse(t)["Actual Name"]
	want := []vaultimport.Node{node("type:: language"), node("body")}
	assert.Equal(t, want, p.Blocks)
	assert.Equal(t, []string{vaultimport.WarnProperties}, kindsOf(p))
}

func TestParseKeepsProseBeforeTheFirstBullet(t *testing.T) {
	p := parse(t)["Prose"]
	want := []vaultimport.Node{node("Some prose first.\n\nSecond paragraph."), node("then a bullet")}
	assert.Equal(t, want, p.Blocks)
	assert.Equal(t, []string{vaultimport.WarnProseBeforeBullets}, kindsOf(p))
}

func TestParseMarksBlockReferencesForThePlanner(t *testing.T) {
	p := parse(t)["Refs"]
	want := "see ((ref:64f1c2a0-0000-4000-8000-000000000001)) and ((ref:64f1c2a0-0000-4000-8000-00000000dead))"
	require.Len(t, p.Blocks, 1)
	assert.Equal(t, want, p.Blocks[0].Content)
}

func TestParseReportsTheIDPropertyOnlyThroughRefID(t *testing.T) {
	p := parse(t)["Go"]
	assert.Empty(t, kindsOf(p), "an id:: property is structure, not text")
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
