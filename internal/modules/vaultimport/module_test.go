package vaultimport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
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
	if err != nil || p.Token != "" {
		t.Fatalf("Preview = %+v, %v; want an empty plan and no error", p, err)
	}
}

func TestPreviewRefusesADirectoryNoAdapterKnows(t *testing.T) {
	m := newModule(t, "/vault", &fakeWriter{}, fakeAdapter{detect: false})
	if _, err := m.Preview(t.Context()); !errors.Is(err, vaultimport.ErrUnknownFormat) {
		t.Fatalf("Preview error = %v, want ErrUnknownFormat", err)
	}
}

func TestPreviewThenApplyWritesOneOwnerBatch(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, err := m.Preview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if p.Token == "" || p.Dir != "/vault" || p.Pages != 2 || p.Links != 1 {
		t.Fatalf("plan = %+v", p)
	}
	res, err := m.Apply(t.Context(), p.Token)
	if err != nil {
		t.Fatal(err)
	}
	if res.GroupID != "G1" || res.Pages != 2 || res.Blocks != 2 {
		t.Errorf("result = %+v", res)
	}
	if w.calls != 1 || len(w.ops) != 2 {
		t.Fatalf("writer got %d calls with %d ops, want one call with two ops", w.calls, len(w.ops))
	}
	want := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: "import", Title: "Import Obsidian vault: 2 pages"}
	if w.meta != want {
		t.Errorf("meta = %+v, want %+v", w.meta, want)
	}
	if !w.meta.BornReviewed() {
		t.Error("an import must be born reviewed")
	}
	if _, err := m.Apply(t.Context(), p.Token); !errors.Is(err, vaultimport.ErrStalePreview) {
		t.Errorf("second Apply error = %v, want ErrStalePreview: the slot is spent", err)
	}
}

func TestApplyRefusesAStaleToken(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	if _, err := m.Apply(t.Context(), "nope"); !errors.Is(err, vaultimport.ErrStalePreview) {
		t.Fatalf("Apply error = %v, want ErrStalePreview", err)
	}
	if w.calls != 0 {
		t.Fatal("a stale token must write nothing")
	}
	if _, err := m.Apply(t.Context(), p.Token); err != nil {
		t.Fatalf("the slot must survive a stale apply: %v", err)
	}
}

func TestDiscardDropsThePlan(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	if err := m.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.Context(), p.Token); !errors.Is(err, vaultimport.ErrStalePreview) {
		t.Fatalf("Apply after Discard = %v, want ErrStalePreview", err)
	}
	if w.calls != 0 {
		t.Fatal("nothing may be written after a discard")
	}
}

func TestApplyKeepsThePlanWhenTheBaseChangedMeanwhile(t *testing.T) {
	w := &fakeWriter{err: knowledge.ErrDuplicateTopic}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	p, _ := m.Preview(t.Context())
	if _, err := m.Apply(t.Context(), p.Token); !errors.Is(err, knowledge.ErrDuplicateTopic) {
		t.Fatalf("Apply error = %v, want ErrDuplicateTopic wrapped", err)
	}
	w.err = nil
	if _, err := m.Apply(t.Context(), p.Token); err != nil {
		t.Fatalf("the plan must survive a refused write: %v", err)
	}
}

func TestApplyRefusesAnEmptyPlan(t *testing.T) {
	w := &fakeWriter{}
	m := newModule(t, "/vault", w, fakeAdapter{format: "Obsidian vault", detect: true})
	p, err := m.Preview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.Context(), p.Token); !errors.Is(err, vaultimport.ErrNothingToImport) {
		t.Fatalf("Apply error = %v, want ErrNothingToImport", err)
	}
	if w.calls != 0 {
		t.Fatal("an empty plan must write nothing")
	}
}

func TestPreviewSkipsPagesAlreadyInTheBase(t *testing.T) {
	m := vaultimport.New(&fakeBase{topics: []knowledge.Block{{ID: "T1", Content: "go"}}}, &fakeWriter{},
		fakeAdapter{format: "Obsidian vault", detect: true, vault: twoPages()})
	vaultimport.SetPicker(m, fakePicker{answer: "/vault"})
	p, err := m.Preview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if p.Pages != 1 || len(p.Conflicts) != 1 || p.Conflicts[0].Kind != vaultimport.ConflictExists {
		t.Fatalf("plan = %+v, want Go skipped as existing", p)
	}
}

func TestPreviewReportsAdapterAndBaseFailures(t *testing.T) {
	boom := errors.New("boom")
	m := newModule(t, "/vault", &fakeWriter{}, fakeAdapter{detect: true, err: boom})
	if _, err := m.Preview(t.Context()); !errors.Is(err, boom) {
		t.Errorf("adapter failure: Preview error = %v", err)
	}
	m = vaultimport.New(&fakeBase{err: boom}, &fakeWriter{}, fakeAdapter{detect: true})
	vaultimport.SetPicker(m, fakePicker{answer: "/vault"})
	if _, err := m.Preview(t.Context()); !errors.Is(err, boom) {
		t.Errorf("base failure: Preview error = %v", err)
	}
}
