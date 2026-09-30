package gitsnapshot_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/gitsnapshot"
)

type fakeBase struct {
	topics   []knowledge.Block
	subtrees map[knowledge.ID][]knowledge.Block
}

func (b *fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) { return b.topics, nil }

func (b *fakeBase) GetSubtree(_ context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	return b.subtrees[root], nil
}

type fakeFiles struct {
	dir   string
	files []knowledge.ExportFile
}

func (w *fakeFiles) WriteFiles(_ context.Context, dir string, files []knowledge.ExportFile) error {
	w.dir = dir
	w.files = files
	return nil
}

type fakeSnapshot struct {
	status   host.GitSnapshotStatus
	result   host.GitSnapshotResult
	err      error
	dirSet   string
	dirErr   error
	tmplSet  string
	tmplErr  error
	exportFn func(ctx context.Context, dir string) error
}

func (f *fakeSnapshot) Status() (host.GitSnapshotStatus, error) { return f.status, nil }

func (f *fakeSnapshot) ExportAndCommit(ctx context.Context, export func(context.Context, string) error) (host.GitSnapshotResult, error) {
	f.exportFn = export
	if err := export(ctx, "/managed"); err != nil {
		return host.GitSnapshotResult{}, err
	}
	return f.result, f.err
}

func (f *fakeSnapshot) SetDirectory(dir string) error { f.dirSet = dir; return f.dirErr }

func (f *fakeSnapshot) SetCommitTemplate(tmpl string) error { f.tmplSet = tmpl; return f.tmplErr }

type fakePicker struct {
	answer string
	err    error
}

func (p fakePicker) SaveFile(_, _ string) (string, error)   { return p.answer, p.err }
func (p fakePicker) OpenFile(_ string) (string, error)      { return p.answer, p.err }
func (p fakePicker) OpenDirectory(_ string) (string, error) { return p.answer, p.err }

func blk(id, parent knowledge.ID, content string) knowledge.Block {
	return knowledge.Block{ID: id, ParentID: parent, Content: content}
}

func TestStatusPassesThrough(t *testing.T) {
	snapshot := &fakeSnapshot{status: host.GitSnapshotStatus{Dir: "/managed"}}
	m := gitsnapshot.New(&fakeBase{}, &fakeFiles{}, snapshot)

	got, err := m.Status(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "/managed", got.Dir)
}

func TestPickDirectorySetsTheChosenDirectory(t *testing.T) {
	snapshot := &fakeSnapshot{}
	m := gitsnapshot.New(&fakeBase{}, &fakeFiles{}, snapshot)
	gitsnapshot.SetPicker(m, fakePicker{answer: "/vault"})

	got, err := m.PickDirectory(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "/vault", got)
	assert.Equal(t, "/vault", snapshot.dirSet)
}

// A cancelled dialog is not a failure: the frontend tells the two apart by
// the empty path.
func TestCancellingTheDirectoryDialogWritesNothing(t *testing.T) {
	snapshot := &fakeSnapshot{}
	m := gitsnapshot.New(&fakeBase{}, &fakeFiles{}, snapshot)
	gitsnapshot.SetPicker(m, fakePicker{answer: ""})

	got, err := m.PickDirectory(t.Context())

	require.NoError(t, err)
	require.Empty(t, got)
	assert.Empty(t, snapshot.dirSet, "set a directory after the dialog was cancelled")
}

func TestPickDirectoryReportsARefusedDirectory(t *testing.T) {
	refused := errors.New("the git snapshot directory must not be empty")
	snapshot := &fakeSnapshot{dirErr: refused}
	m := gitsnapshot.New(&fakeBase{}, &fakeFiles{}, snapshot)
	gitsnapshot.SetPicker(m, fakePicker{answer: "/vault"})

	_, err := m.PickDirectory(t.Context())

	require.ErrorIs(t, err, refused)
}

func TestSetCommitTemplatePassesThrough(t *testing.T) {
	snapshot := &fakeSnapshot{}
	m := gitsnapshot.New(&fakeBase{}, &fakeFiles{}, snapshot)

	require.NoError(t, m.SetCommitTemplate(t.Context(), "custom {{.Time}}"))

	assert.Equal(t, "custom {{.Time}}", snapshot.tmplSet)
}

func TestExportAndCommitRendersMarkdownIntoTheManagedDirectory(t *testing.T) {
	base := &fakeBase{
		topics: []knowledge.Block{
			blk("t1", "", "# Alpha"),
			blk("c1", "t1", "child of alpha"),
		},
		subtrees: map[knowledge.ID][]knowledge.Block{
			"t1": {blk("t1", "", "# Alpha"), blk("c1", "t1", "child of alpha")},
		},
	}
	files := &fakeFiles{}
	snapshot := &fakeSnapshot{result: host.GitSnapshotResult{Committed: true}}
	m := gitsnapshot.New(base, files, snapshot)

	result, err := m.ExportAndCommit(t.Context())

	require.NoError(t, err)
	assert.True(t, result.Committed)
	assert.Equal(t, "/managed", files.dir)
	want := []knowledge.ExportFile{
		{Name: "Alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		knowledge.MarkdownMarker(),
	}
	assert.Equal(t, want, files.files)
}
