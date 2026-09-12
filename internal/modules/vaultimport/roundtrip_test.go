package vaultimport_test

import (
	"context"
	"testing"

	"github.com/cominddev/comind/internal/fsexport"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/export"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/comind"
	"github.com/cominddev/comind/internal/modules/vaultimport/logseq"
	"github.com/cominddev/comind/internal/modules/vaultimport/obsidian"
)

// shape is a topic as content and structure only: what a round trip through
// markdown must keep. Ids and timestamps are new on every import.
type shape struct {
	mode     knowledge.Mode
	content  string
	children []shape
}

func shapeOf(sub []knowledge.Block) shape {
	kids := map[knowledge.ID][]knowledge.Block{}
	for _, b := range sub[1:] {
		kids[b.ParentID] = append(kids[b.ParentID], b)
	}
	var build func(b knowledge.Block) shape
	build = func(b knowledge.Block) shape {
		s := shape{mode: b.Mode, content: b.Content}
		for _, c := range kids[b.ID] {
			s.children = append(s.children, build(c))
		}
		return s
	}
	return build(sub[0])
}

func shapes(t *testing.T, svc *knowledge.Service) map[string]shape {
	t.Helper()
	ctx := context.Background()
	topics, err := svc.TopicMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]shape{}
	for _, b := range topics {
		if !b.IsTopic() {
			continue
		}
		sub, err := svc.GetSubtree(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		out[knowledge.TopicName(b.Content)] = shapeOf(sub)
	}
	return out
}

func TestExportThenImportKeepsEveryTopic(t *testing.T) {
	ctx := context.Background()
	src := openService(t)
	meta := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: "test", Title: "seed"}
	_, err := src.ApplyOps(ctx, meta, []knowledge.Op{
		{
			Kind: knowledge.OpAdd, TempID: "r", Mode: knowledge.ModeOutline,
			Content: "# Go: contexts?\nThe root has a second line.",
		},
		{Kind: knowledge.OpAdd, TempID: "a", Parent: "r", Content: "Contexts"},
		{Kind: knowledge.OpAdd, TempID: "b", Parent: "a", Content: "carry deadlines, see ((a|ctx))\nacross calls"},
		{Kind: knowledge.OpAdd, TempID: "c", Parent: "r", After: "a", Content: "Never store one in a struct"},
		{
			Kind: knowledge.OpAdd, TempID: "d", Mode: knowledge.ModeDoc,
			Content: "Essay\n\nProse about [[Go: contexts?]].\n\n- a list\n- in prose",
		},
		{Kind: knowledge.OpAdd, TempID: "e", Content: "Empty outline", Mode: knowledge.ModeOutline},
		{Kind: knowledge.OpAdd, TempID: "f", Content: "Bare doc", Mode: knowledge.ModeDoc},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := export.New(src, fsexport.Writer{}).Markdown(ctx, dir); err != nil {
		t.Fatal(err)
	}

	dst := openService(t)
	m := vaultimport.New(dst, dst, comind.Adapter{}, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: dir})
	plan, err := m.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Format != "Comind export" || len(plan.Conflicts) != 0 || plan.Pages != 4 {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := m.Apply(ctx, plan.Token); err != nil {
		t.Fatal(err)
	}

	before, after := shapes(t, src), shapes(t, dst)
	if len(after) != len(before) {
		t.Fatalf("topics after = %v, before = %v", after, before)
	}
	for name, want := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("topic %q did not come back", name)
			continue
		}
		if !sameShape(got, want) {
			t.Errorf("topic %q came back as\n%+v\nwant\n%+v", name, got, want)
		}
	}
}

func sameShape(a, b shape) bool {
	if a.mode != b.mode || a.content != b.content || len(a.children) != len(b.children) {
		return false
	}
	for i := range a.children {
		if !sameShape(a.children[i], b.children[i]) {
			return false
		}
	}
	return true
}
