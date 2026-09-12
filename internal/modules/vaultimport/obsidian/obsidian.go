// Package obsidian parses an Obsidian vault: every markdown file is a page
// named by its basename, and each page becomes a document topic holding its
// whole markdown.
package obsidian

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/modules/vaultimport"
)

// Adapter is the Obsidian format.
type Adapter struct{}

// Format names the format for reports.
func (Adapter) Format() string { return "Obsidian vault" }

// Detect reports whether dir holds a vault: Obsidian keeps its settings in a
// .obsidian directory at the root.
func (Adapter) Detect(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".obsidian"))
	return err == nil && info.IsDir()
}

// Parse reads every markdown file outside dot directories, .trash included,
// and skips everything else: attachments are not pages. A file named
// "Go.md" in any folder is the page "Go", as Obsidian resolves wikilinks.
func (a Adapter) Parse(ctx context.Context, dir string) (vaultimport.Vault, error) {
	v := vaultimport.Vault{Format: a.Format()}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		v.Pages = append(v.Pages, page(filepath.ToSlash(rel), string(raw)))
		return nil
	})
	if err != nil {
		return vaultimport.Vault{}, err
	}
	sort.Slice(v.Pages, func(i, j int) bool { return v.Pages[i].Path < v.Pages[j].Path })
	return v, nil
}

// page converts one file into a document page.
func page(rel, raw string) vaultimport.Page {
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	p := vaultimport.Page{Name: name, Path: rel, Mode: knowledge.ModeDoc}
	if hasFrontmatter(raw) {
		p.Warnings = append(p.Warnings, vaultimport.Warning{
			Page: name, Kind: vaultimport.WarnFrontmatter, Detail: "kept as text",
		})
	}
	doc, warns := vaultimport.RewriteWikilinks(raw)
	for _, w := range warns {
		w.Page = name
		p.Warnings = append(p.Warnings, w)
	}
	p.Doc = vaultimport.RewriteLinkTargets(doc, func(target string) (string, bool) {
		short := shortName(target)
		if short == target {
			return target, false
		}
		if strings.Contains(target, "/") {
			p.Warnings = append(p.Warnings, vaultimport.Warning{
				Page: name, Kind: vaultimport.WarnPath, Detail: "[[" + target + "]]",
			})
		}
		return short, true
	})
	return p
}

// shortName reduces a link target to the page name Obsidian resolves it by:
// the basename, without a .md extension. Obsidian writes a folder path into
// a link only to tell two pages of one name apart, and lets a link carry
// the extension; a page is named by its basename either way.
func shortName(target string) string {
	if i := strings.LastIndexByte(target, '/'); i >= 0 {
		target = target[i+1:]
	}
	return strings.TrimSuffix(target, ".md")
}

// hasFrontmatter reports a YAML block opening the file: a first line of
// "---" and a later line closing it.
func hasFrontmatter(raw string) bool {
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, "---\n") || strings.Contains(rest, "\n---\n") || strings.HasSuffix(rest, "\n---")
}
