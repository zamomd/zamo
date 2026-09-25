package export_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/export"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBase struct {
	topics     []knowledge.Block
	topicsErr  error
	subtrees   map[knowledge.ID][]knowledge.Block
	subtreeErr error
}

func (b *fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) {
	return b.topics, b.topicsErr
}

func (b *fakeBase) GetSubtree(_ context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	if b.subtreeErr != nil {
		return nil, b.subtreeErr
	}
	return b.subtrees[root], nil
}

type fakeFiles struct {
	dir      string
	files    []knowledge.ExportFile
	writeErr error
}

func (w *fakeFiles) WriteFiles(_ context.Context, dir string, files []knowledge.ExportFile) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	w.dir = dir
	w.files = files
	return nil
}

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

func TestMarkdownWritesOneFilePerTopic(t *testing.T) {
	base := &fakeBase{
		topics: []knowledge.Block{
			blk("t1", "", "# Alpha"),
			blk("c1", "t1", "child of alpha"),
			blk("t2", "", "# Beta"),
		},
		subtrees: map[knowledge.ID][]knowledge.Block{
			"t1": {blk("t1", "", "# Alpha"), blk("c1", "t1", "child of alpha")},
			"t2": {blk("t2", "", "# Beta")},
		},
	}
	files := &fakeFiles{}

	require.NoError(t, export.New(base, files).Markdown(t.Context(), "/export/here"))

	assert.Equal(t, "/export/here", files.dir, "want the dir passed to Markdown")
	want := []knowledge.ExportFile{
		{Name: "Alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		{Name: "Beta.md", Content: "- # Beta\n"},
		knowledge.MarkdownMarker(),
	}
	assert.Equal(t, want, files.files)
}

func TestMarkdownSurfacesWriteError(t *testing.T) {
	boom := errors.New("disk full")
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}

	err := export.New(base, &fakeFiles{writeErr: boom}).Markdown(t.Context(), "/export/here")
	assert.ErrorIs(t, err, boom)
}

func TestMarkdownEmptyBase(t *testing.T) {
	files := &fakeFiles{}

	require.NoError(t, export.New(&fakeBase{}, files).Markdown(t.Context(), "/export/here"))
	assert.Equal(t, []knowledge.ExportFile{knowledge.MarkdownMarker()}, files.files, "want the marker alone")
}

func TestMarkdownWritesNothingOnAReadError(t *testing.T) {
	boom := errors.New("db down")
	base := &fakeBase{
		topics:     []knowledge.Block{blk("t1", "", "# Alpha")},
		subtreeErr: boom,
	}
	files := &fakeFiles{}

	err := export.New(base, files).Markdown(t.Context(), "/export/here")
	require.ErrorIs(t, err, boom)
	assert.Nil(t, files.files, "wrote files after a read error")
}

func TestExportWritesIntoTheChosenDirectory(t *testing.T) {
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}
	files := &fakeFiles{}
	m := export.New(base, files)
	export.SetPicker(m, fakePicker{answer: "/vault"})

	got, err := m.Export(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/vault", got, "reported directory")
	assert.Equal(t, "/vault", files.dir, "written directory")
}

// A cancelled dialog is not a failure: the frontend tells the two apart by
// the empty path.
func TestExportCancelledDialogWritesNothing(t *testing.T) {
	files := &fakeFiles{}
	m := export.New(&fakeBase{topics: []knowledge.Block{blk("t1", "", "# Alpha")}}, files)
	export.SetPicker(m, fakePicker{answer: ""})

	got, err := m.Export(t.Context())
	require.NoError(t, err)
	require.Empty(t, got)
	assert.Nil(t, files.files, "wrote files after the dialog was cancelled")
}

func TestExportReportsAWriteFailure(t *testing.T) {
	boom := errors.New("disk full")
	m := export.New(&fakeBase{topics: []knowledge.Block{blk("t1", "", "# Alpha")}}, &fakeFiles{writeErr: boom})
	export.SetPicker(m, fakePicker{answer: "/vault"})

	_, err := m.Export(t.Context())
	require.ErrorIs(t, err, boom)
}
