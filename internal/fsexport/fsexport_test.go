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

func TestWriterRemovesWhatAnEarlierExportWroteAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mine.md"), []byte("the owner's own note"), 0o644))
	first := []knowledge.ExportFile{
		{Name: "alpha.md", Content: "- # Alpha\n"},
		{Name: "beta.md", Content: "- # Beta\n"},
	}
	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, first))

	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, first[:1]))

	assert.FileExists(t, filepath.Join(dir, "alpha.md"))
	assert.NoFileExists(t, filepath.Join(dir, "beta.md"))
	assert.FileExists(t, filepath.Join(dir, "mine.md"))
}

func TestWriterKeepsAFileWhoseNameOnlyChangedCase(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "alpha.md", Content: "old"}}))

	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "Alpha.md", Content: "new"}}))

	got, err := os.ReadFile(filepath.Join(dir, "Alpha.md"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

func TestWriterCleansUpAfterAnExportThatFailedPartWay(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "alpha.md", Content: "x"}}))
	// A name no file can take fails the second export after its first write.
	broken := []knowledge.ExportFile{{Name: "beta.md", Content: "x"}, {Name: "no/such/dir.md", Content: "x"}}
	require.Error(t, (Writer{}).WriteFiles(t.Context(), dir, broken))

	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "gamma.md", Content: "x"}}))

	assert.NoFileExists(t, filepath.Join(dir, "alpha.md"))
	assert.NoFileExists(t, filepath.Join(dir, "beta.md"))
	assert.FileExists(t, filepath.Join(dir, "gamma.md"))
}

func TestWriterNeverRemovesOutsideTheDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "export")
	require.NoError(t, os.Mkdir(dir, 0o755))
	outside := filepath.Join(root, "precious.md")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte("../precious.md\n"+outside+"\n"), 0o644))

	require.NoError(t, (Writer{}).WriteFiles(t.Context(), dir, []knowledge.ExportFile{{Name: "alpha.md", Content: "x"}}))

	assert.FileExists(t, outside)
}
