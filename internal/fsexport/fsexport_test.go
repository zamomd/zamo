package fsexport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func TestWriterWritesEachFileToDisk(t *testing.T) {
	dir := t.TempDir()
	files := []knowledge.ExportFile{
		{Name: "alpha.md", Content: "- # Alpha\n"},
		{Name: "beta.md", Content: "- # Beta\n"},
	}
	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, files))
	for _, f := range files {
		got, err := os.ReadFile(filepath.Join(dir, f.Name))
		require.NoError(t, err)
		assert.Equal(t, f.Content, string(got), f.Name)
	}
}

func TestWriterHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	err := (Writer{}).WriteFiles(ctx, dir, []knowledge.ExportFile{{Name: "a.md", Content: "x"}})
	require.Error(t, err)
	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries, "wrote files under a cancelled context")
}

func TestWriterSurfacesWriteError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	err := (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "a.md", Content: "x"}})
	require.Error(t, err)
}
