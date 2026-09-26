package vaultimport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
)

type fakeBase struct {
	topics []knowledge.Block
	err    error
}

func (b *fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) { return b.topics, b.err }
func (b *fakeBase) GetSubtree(context.Context, knowledge.ID) ([]knowledge.Block, error) {
	return nil, nil
}

type fakeWriter struct {
	calls int
	meta  knowledge.WriteMeta
	ops   []knowledge.Op
	err   error
}

func (w *fakeWriter) ApplyOps(
	_ context.Context, meta knowledge.WriteMeta, ops []knowledge.Op,
) (knowledge.Applied, error) {
	w.calls++
	w.meta, w.ops = meta, ops
	if w.err != nil {
		return knowledge.Applied{}, w.err
	}
	return knowledge.Applied{GroupID: "G1"}, nil
}

type fakePicker struct {
	answer string
	err    error
}

func (p fakePicker) SaveFile(_, _ string) (string, error)   { return p.answer, p.err }
func (p fakePicker) OpenFile(_ string) (string, error)      { return p.answer, p.err }
func (p fakePicker) OpenDirectory(_ string) (string, error) { return p.answer, p.err }

type fakeAdapter struct {
	format string
	detect bool
	vault  vaultimport.Vault
	err    error
}

func (a fakeAdapter) Format() string     { return a.format }
func (a fakeAdapter) Detect(string) bool { return a.detect }
func (a fakeAdapter) Parse(context.Context, string) (vaultimport.Vault, error) {
	return a.vault, a.err
}

func twoPages() vaultimport.Vault {
	return vaultimport.Vault{Format: "Obsidian vault", Pages: []vaultimport.Page{
		{Name: "Go", Path: "Go.md", Mode: knowledge.ModeDoc, Doc: "Go is a language."},
		{Name: "Home", Path: "Home.md", Mode: knowledge.ModeDoc, Doc: "See [[Go]]."},
	}}
}

func newModule(t *testing.T, dir string, writer *fakeWriter, adapters ...vaultimport.Adapter) *vaultimport.Module {
	t.Helper()
	m := vaultimport.New(&fakeBase{}, writer, adapters...)
	vaultimport.SetPicker(m, fakePicker{answer: dir})
	return m
}

func TestPreviewCancelledReturnsNoPlan(t *testing.T) {
	m := newModule(t, "", &fakeWriter{}, fakeAdapter{detect: true})
	p, err := m.Preview(t.Context())
	require.NoError(t, err)
	require.Empty(t, p.Token)
}

func TestPreviewRefusesADirectoryNoAdapterKnows(t *testing.T) {
	m := newModule(t, "/vault", &fakeWriter{}, fakeAdapter{detect: false})
	_, err := m.Preview(t.Context())
	require.ErrorIs(t, err, vaultimport.ErrUnknownFormat)
}

func TestPreviewThenApplyWritesOneOwnerBatch(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, err := m.Preview(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, p.Token)
	require.Equal(t, "/vault", p.Dir)
	require.Equal(t, 2, p.Pages)
	require.Equal(t, 1, p.Links)
	res, err := m.Apply(t.Context(), p.Token)
	require.NoError(t, err)
	assert.Equal(t, knowledge.ID("G1"), res.GroupID)
	assert.Equal(t, 2, res.Pages)
	assert.Equal(t, 2, res.Blocks)
	require.Equal(t, 1, w.calls, "writer calls")
	require.Len(t, w.ops, 2)
	want := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: "import", Title: "Import Obsidian vault: 2 pages"}
	assert.Equal(t, want, w.meta)
	assert.True(t, w.meta.BornReviewed(), "an import must be born reviewed")
	_, err = m.Apply(t.Context(), p.Token)
	assert.ErrorIs(t, err, vaultimport.ErrStalePreview, "second Apply: the slot is spent")
}

func TestApplyRefusesAStaleToken(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	_, err := m.Apply(t.Context(), "nope")
	require.ErrorIs(t, err, vaultimport.ErrStalePreview)
	require.Zero(t, w.calls, "a stale token must write nothing")
	_, err = m.Apply(t.Context(), p.Token)
	require.NoError(t, err, "the slot must survive a stale apply")
}

func TestDiscardDropsThePlan(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	require.NoError(t, m.Discard(t.Context()))
	_, err := m.Apply(t.Context(), p.Token)
	require.ErrorIs(t, err, vaultimport.ErrStalePreview, "Apply after Discard")
	require.Zero(t, w.calls, "nothing may be written after a discard")
}

func TestApplyKeepsThePlanWhenTheBaseChangedMeanwhile(t *testing.T) {
	w := &fakeWriter{err: knowledge.ErrDuplicateTopic}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	_, err := m.Apply(t.Context(), p.Token)
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic)
	w.err = nil
	_, err = m.Apply(t.Context(), p.Token)
	require.NoError(t, err, "the plan must survive a refused write")
}

func TestApplyRefusesAnEmptyPlan(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true})
	p, err := m.Preview(t.Context())
	require.NoError(t, err)
	_, err = m.Apply(t.Context(), p.Token)
	require.ErrorIs(t, err, vaultimport.ErrNothingToImport)
	require.Zero(t, w.calls, "an empty plan must write nothing")
}

func TestPreviewSkipsPagesAlreadyInTheBase(t *testing.T) {
	m := vaultimport.New(&fakeBase{topics: []knowledge.Block{{ID: "T1", Content: "go"}}}, &fakeWriter{},
		fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	vaultimport.SetPicker(m, fakePicker{answer: "/vault"})
	p, err := m.Preview(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, p.Pages)
	require.Len(t, p.Conflicts, 1)
	require.Equal(t, vaultimport.ConflictExists, p.Conflicts[0].Kind, "want Go skipped as existing")
}

func TestPreviewReportsAdapterAndBaseFailures(t *testing.T) {
	boom := errors.New("boom")
	m := newModule(t, "/vault", &fakeWriter{}, fakeAdapter{detect: true, err: boom})
	_, err := m.Preview(t.Context())
	require.ErrorIs(t, err, boom, "adapter failure")
	m = vaultimport.New(&fakeBase{err: boom}, &fakeWriter{}, fakeAdapter{detect: true})
	vaultimport.SetPicker(m, fakePicker{answer: "/vault"})
	_, err = m.Preview(t.Context())
	assert.ErrorIs(t, err, boom, "base failure")
}
