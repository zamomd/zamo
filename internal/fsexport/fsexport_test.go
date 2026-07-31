package fsexport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func TestWriterWritesEachFileToDisk(t *testing.T) {
	dir := t.TempDir()
	files := []knowledge.ExportFile{
		{Name: "alpha.md", Content: "- # Alpha\n"},
		{Name: "beta.md", Content: "- # Beta\n"},
	}
	if err := (Writer{}).WriteFiles(t.Context(), dir, files); err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	for _, f := range files {
		got, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if string(got) != f.Content {
			t.Errorf("%s = %q, want %q", f.Name, got, f.Content)
		}
	}
}

func TestWriterHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	err := (Writer{}).WriteFiles(ctx, dir, []knowledge.ExportFile{{Name: "a.md", Content: "x"}})
	if err == nil {
		t.Fatal("WriteFiles on a cancelled context returned nil, want the cancellation error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("wrote %d files under a cancelled context, want 0", len(entries))
	}
}

func TestWriterSurfacesWriteError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	err := (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "a.md", Content: "x"}})
	if err == nil {
		t.Fatal("WriteFiles into a missing directory returned nil, want the write error")
	}
}
