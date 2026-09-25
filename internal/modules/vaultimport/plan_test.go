package vaultimport

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cominddev/comind/internal/knowledge"
)

func docPage(name, doc string) Page {
	return Page{Name: name, Path: name + ".md", Mode: knowledge.ModeDoc, Doc: doc}
}

func outlinePage(name string, blocks ...Node) Page {
	return Page{Name: name, Path: name + ".md", Mode: knowledge.ModeOutline, Blocks: blocks}
}

func n(content string, children ...Node) Node { return Node{Content: content, Children: children} }

func existing(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, s := range names {
		m[knowledge.TopicKey(s)] = true
	}
	return m
}

func conflictKinds(p Plan) map[string]string {
	m := make(map[string]string)
	for _, c := range p.Conflicts {
		m[c.Page] = c.Kind
	}
	return m
}

func warnKinds(p Plan) []string {
	var kinds []string
	for _, w := range p.Warnings {
		kinds = append(kinds, w.Kind)
	}
	return kinds
}

func TestPlanSkipsPagesItCannotName(t *testing.T) {
	v := Vault{Pages: []Page{
		docPage("", "empty name"),
		docPage("Weird [name]", "bracket"),
		docPage("Inbox", "already in the base"),
		docPage("Go", "first"),
		docPage("go", "case collision"),
		docPage("Fine", "ok"),
	}}
	p := plan(v, existing("Inbox"))
	want := map[string]string{
		"":             ConflictUnnamed,
		"Weird [name]": ConflictUnnamed,
		"Inbox":        ConflictExists,
		"go":           ConflictDuplicate,
	}
	assert.Equal(t, want, conflictKinds(p))
	assert.Equal(t, 2, p.Pages, "Go and Fine")
	assert.Len(t, p.ops, 2, "want one root per imported page")
}

func TestPlanBuildsAnOutlineWithChainedAnchors(t *testing.T) {
	v := Vault{Pages: []Page{outlinePage("Go",
		n("first", n("child a"), n("child b")),
		n("second"),
	)}}
	p := plan(v, nil)
	want := []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "p0", Content: "Go", Mode: knowledge.ModeOutline},
		{Kind: knowledge.OpAdd, TempID: "p0.0", Parent: "p0", Content: "first"},
		{Kind: knowledge.OpAdd, TempID: "p0.0.0", Parent: "p0.0", Content: "child a"},
		{Kind: knowledge.OpAdd, TempID: "p0.0.1", Parent: "p0.0", After: "p0.0.0", Content: "child b"},
		{Kind: knowledge.OpAdd, TempID: "p0.1", Parent: "p0", After: "p0.0", Content: "second"},
	}
	assert.Equal(t, want, p.ops)
	assert.Equal(t, 5, p.Blocks)
}

func TestPlanBuildsADocumentBehindItsName(t *testing.T) {
	v := Vault{Pages: []Page{
		docPage("Go", "---\ntitle: x\n---\nGo is a language."),
		docPage("Home", "# Home\n\nWelcome."),
		docPage("Empty", ""),
	}}
	p := plan(v, nil)
	want := []knowledge.Op{
		{
			Kind: knowledge.OpAdd, TempID: "p0", Mode: knowledge.ModeDoc,
			Content: "Go\n\n---\ntitle: x\n---\nGo is a language.",
		},
		{Kind: knowledge.OpAdd, TempID: "p1", Content: "# Home\n\nWelcome.", Mode: knowledge.ModeDoc},
		{Kind: knowledge.OpAdd, TempID: "p2", Content: "Empty", Mode: knowledge.ModeDoc},
	}
	assert.Equal(t, want, p.ops)
}

func TestPlanReportsBrokenLinksAndCountsResolvedOnes(t *testing.T) {
	v := Vault{Pages: []Page{
		docPage("A", "see [[B]], [[Inbox]], [[Nowhere]] and [[nowhere]] again"),
		docPage("B", "see [[A]]"),
		docPage("Inbox", "skipped but its topic exists"),
	}}
	p := plan(v, existing("Inbox"))
	require.Equal(t, []string{WarnBrokenLink}, warnKinds(p), "want one broken-link (deduplicated by key)")
	assert.Equal(t, "A", p.Warnings[0].Page)
	assert.Equal(t, "[[Nowhere]]", p.Warnings[0].Detail)
	assert.Equal(t, 3, p.Links, "A→B, A→Inbox, B→A")
}

func TestPlanResolvesBlockReferencesToTempIDs(t *testing.T) {
	target := n("Contexts\ncarry deadlines")
	target.RefID = "64f1-0001"
	v := Vault{Pages: []Page{
		outlinePage("Go", n("Go is a language", target)),
		outlinePage("Refs", n("see ((ref:64f1-0001)) and ((ref:dead|gone))")),
	}}
	p := plan(v, nil)
	assert.Equal(t, "see ((p0.0.0|Contexts)) and ((dead|gone))", p.ops[len(p.ops)-1].Content)
	assert.Equal(t, []string{WarnBrokenBlockRef}, warnKinds(p))
	assert.Equal(t, 1, p.Links)
}

func TestPlanCarriesAdapterWarnings(t *testing.T) {
	pg := docPage("A", "x")
	pg.Warnings = []Warning{{Page: "A", Kind: WarnFrontmatter}}
	p := plan(Vault{Format: "Obsidian vault", Pages: []Page{pg}}, nil)
	assert.Equal(t, []string{WarnFrontmatter}, warnKinds(p))
	assert.Equal(t, "Obsidian vault", p.Format)
}

func TestPlanOfAnEmptyVaultHasNothingToApply(t *testing.T) {
	p := plan(Vault{}, nil)
	assert.Empty(t, p.ops)
	assert.Zero(t, p.Pages)
	assert.Empty(t, p.Conflicts)
}

func TestPlanWritesTheRootWholeWhenThePageCarriesIt(t *testing.T) {
	pg := outlinePage("Go", n("child"))
	pg.Root = "# Go\nsecond line of the root"
	p := plan(Vault{Pages: []Page{pg}}, nil)
	assert.Equal(t, pg.Root, p.ops[0].Content, "root op")
	assert.Equal(t, knowledge.ModeOutline, p.ops[0].Mode, "root op")
	assert.Equal(t, "p0", p.ops[1].Parent, "child op")
	assert.Equal(t, "child", p.ops[1].Content, "child op")
}
