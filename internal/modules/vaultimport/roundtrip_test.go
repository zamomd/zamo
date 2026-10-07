package vaultimport_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/fsexport"
	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/export"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
	"github.com/zamomd/zamo/internal/modules/vaultimport/logseq"
	"github.com/zamomd/zamo/internal/modules/vaultimport/obsidian"
	"github.com/zamomd/zamo/internal/modules/vaultimport/zamo"
)

// unscheduled is the scheduler of an export that only ever runs by hand.
type unscheduled struct{ host.Scheduler }

func (unscheduled) Handle(host.ScheduleHandler, host.ScheduleOptions) {}

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
	require.NoError(t, err)
	out := map[string]shape{}
	for _, b := range topics {
		if !b.IsTopic() {
			continue
		}
		sub, err := svc.GetSubtree(ctx, b.ID)
		require.NoError(t, err)
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
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, export.New(src, fsexport.Writer{}, nil, unscheduled{}, time.Now).Markdown(ctx, dir))

	dst := openService(t)
	m := vaultimport.New(dst, dst, zamo.Adapter{}, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: dir})
	plan, err := m.Preview(ctx)
	require.NoError(t, err)
	require.Equal(t, "Zamo export", plan.Format)
	require.Empty(t, plan.Conflicts)
	require.Equal(t, 4, plan.Pages)
	_, err = m.Apply(ctx, plan.Token)
	require.NoError(t, err)

	before, after := shapes(t, src), shapes(t, dst)
	require.Len(t, after, len(before), "topics after = %v, before = %v", after, before)
	for name, want := range before {
		if !assert.Contains(t, after, name, "topic did not come back") {
			continue
		}
		assert.Equal(t, want, after[name], "topic %q", name)
	}
}
