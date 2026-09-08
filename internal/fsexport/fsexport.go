// Package fsexport is the filesystem adapter for exporting the base: the
// host.FileWriter implementation that writes rendered files to disk.
package fsexport

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cominddev/comind/internal/host"
	"github.com/cominddev/comind/internal/knowledge"
)

// Writer writes an export to the local filesystem. It keeps the domain free of
// os: knowledge renders the files, this writes them.
type Writer struct{}

var _ host.FileWriter = Writer{}

// WriteFiles writes each file into dir with a fixed 0644 mode. dir must already
// exist; a nonexistent directory surfaces as a write error rather than being
// created. Cancellation is honoured between files; a write failure part-way
// (disk full, permissions) can still leave earlier files on disk.
func (Writer) WriteFiles(ctx context.Context, dir string, files []knowledge.ExportFile) error {
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
