package export_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/export"
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

	if err := export.New(base, files).Markdown(t.Context(), "/export/here"); err != nil {
		t.Fatalf("Markdown: %v", err)
	}

	if files.dir != "/export/here" {
		t.Errorf("writer got dir %q, want the dir passed to Markdown", files.dir)
	}
	want := []knowledge.ExportFile{
		{Name: "Alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		{Name: "Beta.md", Content: "- # Beta\n"},
	}
	if !slices.Equal(files.files, want) {
		t.Errorf("writer got files %+v, want %+v", files.files, want)
	}
}

func TestMarkdownSurfacesWriteError(t *testing.T) {
	boom := errors.New("disk full")
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}

	err := export.New(base, &fakeFiles{writeErr: boom}).Markdown(t.Context(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the writer error surfaced", err)
	}
}

func TestMarkdownEmptyBase(t *testing.T) {
	files := &fakeFiles{}

	if err := export.New(&fakeBase{}, files).Markdown(t.Context(), "/export/here"); err != nil {
		t.Fatalf("empty base must export cleanly, got %v", err)
	}
	if len(files.files) != 0 {
		t.Errorf("handed %d files for an empty base, want 0", len(files.files))
	}
}

func TestMarkdownWritesNothingOnAReadError(t *testing.T) {
	boom := errors.New("db down")
	base := &fakeBase{
		topics:     []knowledge.Block{blk("t1", "", "# Alpha")},
		subtreeErr: boom,
	}
	files := &fakeFiles{}

	err := export.New(base, files).Markdown(t.Context(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error surfaced", err)
	}
	if files.files != nil {
		t.Errorf("wrote %d files after a read error, want none", len(files.files))
	}
}
