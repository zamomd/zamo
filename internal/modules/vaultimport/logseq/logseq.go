// Package logseq parses a Logseq graph: the markdown files under pages/ and
// journals/, each an outline whose bullets become a block tree.
package logseq

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

// Adapter is the Logseq format.
type Adapter struct{}

// Format names the format for reports.
func (Adapter) Format() string { return "Logseq graph" }

// Detect reports whether dir holds a graph: Logseq keeps its config in a
// logseq directory next to pages.
func (Adapter) Detect(dir string) bool {
	for _, sub := range []string{"logseq", "pages"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// Parse reads pages/*.md and journals/*.md. Page names come from the title
// property when a page has one, otherwise from the filename decoded the way
// Logseq encodes it; journals are named by date.
func (a Adapter) Parse(ctx context.Context, dir string) (vaultimport.Vault, error) {
	v := vaultimport.Vault{Format: a.Format()}
	edn, err := os.ReadFile(filepath.Join(dir, "logseq", "config.edn"))
	if err != nil && !os.IsNotExist(err) {
		return vaultimport.Vault{}, err
	}
	cfg := parseConfig(string(edn))
	for _, sub := range []string{"journals", "pages"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			if os.IsNotExist(err) && sub == "journals" {
				continue
			}
			return vaultimport.Vault{}, err
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return vaultimport.Vault{}, err
			}
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, sub, e.Name()))
			if err != nil {
				return vaultimport.Vault{}, err
			}
			v.Pages = append(v.Pages, page(cfg, sub, e.Name(), string(raw)))
		}
	}
	sort.Slice(v.Pages, func(i, j int) bool { return v.Pages[i].Path < v.Pages[j].Path })
	return v, nil
}

// property matches one property line as mldoc reads it: a key of
// anything but whitespace and colons, then ":: " and the value, or a
// bare "key::" with no value. "key::value" is text.
var property = regexp.MustCompile(`^\s*([^\s:]+)::(?: (.*)|\s*)$`)

// page converts one file into an outline page.
func page(cfg graphConfig, sub, file, raw string) vaultimport.Page {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	p := vaultimport.Page{
		Name: pageName(cfg, sub, base),
		Path: sub + "/" + file,
		Mode: knowledge.ModeOutline,
	}
	preamble, nodes := vaultimport.ParseOutline(raw)
	if preamble != "" {
		kind := vaultimport.WarnProseBeforeBullets
		if propertiesOnly(preamble) {
			kind = vaultimport.WarnProperties
		}
		p.Warnings = append(p.Warnings, vaultimport.Warning{Page: p.Name, Kind: kind, Detail: firstLine(preamble)})
		nodes = append([]vaultimport.Node{{Content: preamble}}, nodes...)
	}
	if len(nodes) > 0 && propertiesOnly(nodes[0].Content) {
		if title, rest := takeProperty(nodes[0].Content, "title"); title != "" {
			p.Name = title
			for i := range p.Warnings {
				p.Warnings[i].Page = title
			}
			if strings.TrimSpace(rest) == "" {
				nodes = nodes[1:]
			} else {
				nodes[0].Content = rest
			}
		}
	}
	p.Blocks = convert(nodes, p.Name, &p.Warnings, preamble != "")
	return p
}

// pageName is the name Logseq gives the page of a file: for a journal the
// date the filename holds, rendered in the graph's title pattern; for any
// other page the filename decoded in the graph's name format.
func pageName(cfg graphConfig, sub, base string) string {
	if sub == "journals" {
		if d, ok := journalDate(base, cfg.fileFormat); ok {
			return formatDate(d, cfg.titleFormat)
		}
	}
	return decodeName(base, cfg.nameFormat)
}

// convert finishes every node: the id property moves to RefID, other
// properties stay and are reported, tags become references, and block
// references are marked for the planner. A properties-only preamble was
// reported by the caller, so its node reports nothing again.
func convert(
	nodes []vaultimport.Node, page string, warns *[]vaultimport.Warning, reportedFirst bool,
) []vaultimport.Node {
	for i := range nodes {
		n := &nodes[i]
		var id string
		id, n.Content = takeProperty(n.Content, "id")
		n.RefID = id
		if hasProperty(n.Content) && (!reportedFirst || i > 0) {
			*warns = append(*warns, vaultimport.Warning{
				Page: page, Kind: vaultimport.WarnProperties, Detail: firstLine(n.Content),
			})
		}
		n.Content = vaultimport.RewriteTags(n.Content)
		n.Content = vaultimport.RewriteBlockRefs(n.Content, func(target, label string) (string, string, bool) {
			return "ref:" + target, label, true
		})
		n.Children = convert(n.Children, page, warns, false)
	}
	return nodes
}

// takeProperty removes the named property from content and returns its
// value. Content keeps its other lines.
func takeProperty(content, key string) (value, rest string) {
	var kept []string
	for line := range strings.SplitSeq(content, "\n") {
		if m := property.FindStringSubmatch(line); m != nil && strings.EqualFold(m[1], key) {
			value = strings.TrimSpace(m[2])
			continue
		}
		kept = append(kept, line)
	}
	return value, strings.Join(kept, "\n")
}

func hasProperty(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		if property.MatchString(line) {
			return true
		}
	}
	return false
}

// propertiesOnly reports content made of property lines and nothing else.
func propertiesOnly(content string) bool {
	found := false
	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !property.MatchString(line) {
			return false
		}
		found = true
	}
	return found
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}
