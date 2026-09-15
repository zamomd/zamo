// Package comind parses a directory Comind itself exported: one markdown
// file per topic beside the marker file the export writes, each file either
// a whole document or one root bullet with the block tree under it.
package comind

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
)

// Adapter is Comind's own export format.
type Adapter struct{}

// Format names the format for reports.
func (Adapter) Format() string { return "Comind export" }

// Detect reports whether dir holds a Comind export: the marker file the
// export writes beside the topic files.
func (Adapter) Detect(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, knowledge.MarkdownMarkerName))
	return err == nil && info.Mode().IsRegular()
}

// Parse reads every markdown file in dir itself; the export writes a flat
// directory, so nothing below it is a page. A page is named by its content,
// which is lossless, rather than by its filename, which the export had to
// make safe for a filesystem and unique.
func (a Adapter) Parse(ctx context.Context, dir string) (vaultimport.Vault, error) {
	v := vaultimport.Vault{Format: a.Format()}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return vaultimport.Vault{}, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return vaultimport.Vault{}, err
		}
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return vaultimport.Vault{}, err
		}
		v.Pages = append(v.Pages, page(e.Name(), string(raw)))
	}
	sort.Slice(v.Pages, func(i, j int) bool { return v.Pages[i].Path < v.Pages[j].Path })
	return v, nil
}

// outlineLine matches every line an exported outline may hold after its
// first: a bullet at an even indentation, or a continuation line indented
// at least under a bullet.
var outlineLine = regexp.MustCompile(`^(?:(?:  )*- |  )`)

// page reads one file in the shape the export gave it. An outline is one
// root bullet at the left margin with everything else indented under it;
// any other file is a document, whose first line is the topic's name.
func page(file, raw string) vaultimport.Page {
	content := strings.TrimRight(raw, "\n")
	if isOutline(content) {
		_, nodes := vaultimport.ParseOutline(content)
		if len(nodes) == 1 {
			root := nodes[0]
			return vaultimport.Page{
				Name:   knowledge.TopicName(root.Content),
				Path:   file,
				Mode:   knowledge.ModeOutline,
				Root:   root.Content,
				Blocks: root.Children,
			}
		}
	}
	return vaultimport.Page{
		Name: knowledge.TopicName(content),
		Path: file,
		Mode: knowledge.ModeDoc,
		Doc:  content,
	}
}

// isOutline reports the export's outline shape: a root bullet on the first
// line and only bullets or continuation lines after it.
func isOutline(content string) bool {
	first := true
	for line := range strings.SplitSeq(content, "\n") {
		switch {
		case first:
			if !strings.HasPrefix(line, "- ") {
				return false
			}
			first = false
		case line == "":
		case !outlineLine.MatchString(line):
			return false
		}
	}
	return !first
}
