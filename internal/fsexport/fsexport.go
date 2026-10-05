// Package fsexport is the filesystem adapter for exporting the base: the
// host.FileWriter implementation that writes rendered files to disk.
package fsexport

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// Writer writes an export to the local filesystem. It keeps the domain free of
// os: knowledge renders the files, this writes them.
type Writer struct{}

var _ host.FileWriter = Writer{}

// manifestName is the file listing what the last export wrote into a
// directory, one name per line. It is what lets the next export remove the
// files of topics that are gone without touching anything else there. A
// topic file can never take the name: leading dots are trimmed from every
// topic filename.
const manifestName = ".zamo-export-files"

// WriteFiles writes each file into dir with a fixed 0644 mode and removes
// the files an earlier export wrote there that are no longer among them. dir
// must already exist; a nonexistent directory surfaces as a write error
// rather than being created. Cancellation is honoured between files; a write
// failure part-way (disk full, permissions) can still leave earlier files on
// disk, and the next export then cleans up after both.
func (Writer) WriteFiles(ctx context.Context, dir string, files []knowledge.ExportFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	earlier, err := readManifest(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	// Until the export is whole the manifest owns both generations, so a
	// failure part-way never orphans a file.
	if err := writeManifest(dir, append(slices.Clone(names), earlier...)); err != nil {
		return err
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	// On a filesystem that folds case a renamed topic's old name is its new
	// file, so names are compared the way such a filesystem compares them.
	current := make(map[string]bool, len(names))
	for _, n := range names {
		current[strings.ToLower(n)] = true
	}
	for _, n := range earlier {
		if current[strings.ToLower(n)] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return writeManifest(dir, names)
}

// readManifest returns the names the last export wrote into dir, or none
// when dir holds no manifest. Only plain file names are answered: the
// manifest sits in a directory anyone can edit, and what it lists is removed.
func readManifest(dir string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for line := range strings.Lines(string(raw)) {
		n := strings.TrimRight(line, "\r\n")
		if n == "" || n == manifestName || !filepath.IsLocal(n) || filepath.Base(n) != n {
			continue
		}
		names = append(names, n)
	}
	return names, nil
}

func writeManifest(dir string, names []string) error {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(dir, manifestName), []byte(b.String()), 0o644)
}
