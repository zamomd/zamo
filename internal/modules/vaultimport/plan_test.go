package vaultimport

import (
	"reflect"
	"testing"

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
	if got := conflictKinds(p); !reflect.DeepEqual(got, want) {
		t.Errorf("conflicts = %v, want %v", got, want)
	}
	if p.Pages != 2 {
		t.Errorf("Pages = %d, want 2 (Go and Fine)", p.Pages)
	}
	if len(p.ops) != 2 {
		t.Errorf("ops = %d, want one root per imported page", len(p.ops))
	}
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
	if !reflect.DeepEqual(p.ops, want) {
		t.Errorf("ops = %#v\nwant %#v", p.ops, want)
	}
	if p.Blocks != 5 {
		t.Errorf("Blocks = %d, want 5", p.Blocks)
	}
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
	if !reflect.DeepEqual(p.ops, want) {
		t.Errorf("ops = %#v\nwant %#v", p.ops, want)
	}
}

func TestPlanReportsBrokenLinksAndCountsResolvedOnes(t *testing.T) {
	v := Vault{Pages: []Page{
		docPage("A", "see [[B]], [[Inbox]], [[Nowhere]] and [[nowhere]] again"),
		docPage("B", "see [[A]]"),
		docPage("Inbox", "skipped but its topic exists"),
	}}
	p := plan(v, existing("Inbox"))
	if got := warnKinds(p); !reflect.DeepEqual(got, []string{WarnBrokenLink}) {
		t.Errorf("warnings = %v, want one broken-link (deduplicated by key)", p.Warnings)
	}
	if p.Warnings[0].Page != "A" || p.Warnings[0].Detail != "[[Nowhere]]" {
		t.Errorf("warning = %+v, want page A about [[Nowhere]]", p.Warnings[0])
	}
	if p.Links != 3 {
		t.Errorf("Links = %d, want 3 (A→B, A→Inbox, B→A)", p.Links)
	}
}

func TestPlanResolvesBlockReferencesToTempIDs(t *testing.T) {
	target := n("Contexts\ncarry deadlines")
	target.RefID = "64f1-0001"
	v := Vault{Pages: []Page{
		outlinePage("Go", n("Go is a language", target)),
		outlinePage("Refs", n("see ((ref:64f1-0001)) and ((ref:dead|gone))")),
	}}
	p := plan(v, nil)
	got := p.ops[len(p.ops)-1].Content
	if want := "see ((p0.0.0|Contexts)) and ((dead|gone))"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if kinds := warnKinds(p); !reflect.DeepEqual(kinds, []string{WarnBrokenBlockRef}) {
		t.Errorf("warnings = %v, want one broken-block-ref", p.Warnings)
	}
	if p.Links != 1 {
		t.Errorf("Links = %d, want 1", p.Links)
	}
}

func TestPlanCarriesAdapterWarnings(t *testing.T) {
	pg := docPage("A", "x")
	pg.Warnings = []Warning{{Page: "A", Kind: WarnFrontmatter}}
	p := plan(Vault{Format: "Obsidian vault", Pages: []Page{pg}}, nil)
	if got := warnKinds(p); !reflect.DeepEqual(got, []string{WarnFrontmatter}) {
		t.Errorf("warnings = %v", got)
	}
	if p.Format != "Obsidian vault" {
		t.Errorf("Format = %q", p.Format)
	}
}

func TestPlanOfAnEmptyVaultHasNothingToApply(t *testing.T) {
	p := plan(Vault{}, nil)
	if len(p.ops) != 0 || p.Pages != 0 || len(p.Conflicts) != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanWritesTheRootWholeWhenThePageCarriesIt(t *testing.T) {
	pg := outlinePage("Go", n("child"))
	pg.Root = "# Go\nsecond line of the root"
	p := plan(Vault{Pages: []Page{pg}}, nil)
	if p.ops[0].Content != pg.Root || p.ops[0].Mode != knowledge.ModeOutline {
		t.Errorf("root op = %+v, want the page's Root as content", p.ops[0])
	}
	if p.ops[1].Parent != "p0" || p.ops[1].Content != "child" {
		t.Errorf("child op = %+v", p.ops[1])
	}
}
