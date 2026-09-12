package vaultimport

import (
	"strconv"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
)

// Plan is what an import would do, shown to the user before anything is
// written: the counts of what goes in, the pages left out and why, and the
// content the conversion could not keep whole.
type Plan struct {
	Token     string     `json:"token"`
	Dir       string     `json:"dir"`
	Format    string     `json:"format"`
	Pages     int        `json:"pages"`  // pages in the batch
	Blocks    int        `json:"blocks"` // blocks in the batch, roots included
	Links     int        `json:"links"`  // references that resolve
	Conflicts []Conflict `json:"conflicts"`
	Warnings  []Warning  `json:"warnings"`

	ops []knowledge.Op
}

// plan turns a parsed vault into one write batch against a base whose topic
// keys are existing. Pages are taken in vault order, so among pages that
// claim one name the first by path wins.
func plan(v Vault, existing map[string]bool) Plan {
	p := Plan{Format: v.Format}
	claimed := make(map[string]bool)
	imported := make(map[string]bool)
	var pages []int
	for i, pg := range v.Pages {
		key := knowledge.TopicKey(pg.Name)
		conflict := func(kind, detail string) {
			p.Conflicts = append(p.Conflicts, Conflict{Page: pg.Name, Path: pg.Path, Kind: kind, Detail: detail})
		}
		switch {
		case key == "":
			conflict(ConflictUnnamed, "the name cannot be a topic name")
		case existing[key]:
			conflict(ConflictExists, "a topic of this name is already in the base")
		case claimed[key]:
			conflict(ConflictDuplicate, "another page of the vault has this name")
		default:
			claimed[key] = true
			imported[key] = true
			pages = append(pages, i)
		}
	}

	// Block references resolve to the temp id of the node that carries the
	// format's block id; a reference without a label gets the node's first
	// line as one.
	targets := make(map[string]target)
	for _, i := range pages {
		collectTargets(v.Pages[i].Blocks, pageTemp(i), targets)
	}

	for _, i := range pages {
		pg := v.Pages[i]
		p.Warnings = append(p.Warnings, pg.Warnings...)
		ops := pageOps(pg, pageTemp(i))
		seen := make(map[string]bool)
		for j := range ops {
			op := &ops[j]
			op.Content = RewriteBlockRefs(op.Content, func(ref, label string) (string, string, bool) {
				id, ok := strings.CutPrefix(ref, "ref:")
				if !ok {
					return "", "", false
				}
				if t, found := targets[id]; found {
					p.Links++
					if label == "" {
						label = t.label
					}
					return t.temp, label, true
				}
				if !seen[ref] {
					seen[ref] = true
					p.Warnings = append(p.Warnings, Warning{
						Page: pg.Name, Kind: WarnBrokenBlockRef, Detail: "((" + id + "))",
					})
				}
				return id, label, true
			})
			for _, ref := range knowledge.ParseRefs(op.Content) {
				if ref.Kind != knowledge.RefTopic {
					continue
				}
				if imported[ref.TargetKey] || existing[ref.TargetKey] {
					p.Links++
					continue
				}
				if !seen[ref.TargetKey] {
					seen[ref.TargetKey] = true
					p.Warnings = append(p.Warnings, Warning{
						Page: pg.Name, Kind: WarnBrokenLink, Detail: "[[" + firstTopicRef(op.Content, ref.TargetKey) + "]]",
					})
				}
			}
		}
		p.ops = append(p.ops, ops...)
		p.Pages++
		p.Blocks += len(ops)
	}
	return p
}

func pageTemp(i int) string { return "p" + strconv.Itoa(i) }

// target is where a block reference lands: a temp id of the batch and the
// label to show when the reference has none.
type target struct{ temp, label string }

// collectTargets records the target of every node carrying a block id.
func collectTargets(nodes []Node, parent string, targets map[string]target) {
	for i, n := range nodes {
		temp := parent + "." + strconv.Itoa(i)
		if n.RefID != "" {
			targets[n.RefID] = target{temp: temp, label: firstLine(n.Content)}
		}
		collectTargets(n.Children, temp, targets)
	}
}

// pageOps builds the add ops of one page: the root first, then the nodes in
// pre-order, each anchored after its previous sibling so the order is the
// vault's.
func pageOps(pg Page, temp string) []knowledge.Op {
	root := knowledge.Op{Kind: knowledge.OpAdd, TempID: temp, Mode: pg.Mode, Content: pg.Name}
	if pg.Mode == knowledge.ModeDoc {
		root.Content = docContent(pg)
		return []knowledge.Op{root}
	}
	if pg.Root != "" {
		root.Content = pg.Root
	}
	ops := []knowledge.Op{root}
	var walk func(nodes []Node, parent string)
	walk = func(nodes []Node, parent string) {
		for i, n := range nodes {
			op := knowledge.Op{Kind: knowledge.OpAdd, TempID: parent + "." + strconv.Itoa(i), Parent: parent, Content: n.Content}
			if i > 0 {
				op.After = parent + "." + strconv.Itoa(i-1)
			}
			ops = append(ops, op)
			walk(n.Children, op.TempID)
		}
	}
	walk(pg.Blocks, temp)
	return ops
}

// docContent puts the page name on the first line, where a topic's name
// lives, unless the document already opens with it.
func docContent(pg Page) string {
	doc := strings.TrimRight(pg.Doc, "\n")
	switch {
	case doc == "":
		return pg.Name
	case knowledge.TopicKey(firstLine(doc)) == knowledge.TopicKey(pg.Name):
		return doc
	default:
		return pg.Name + "\n\n" + doc
	}
}

// firstTopicRef returns the text of the first [[...]] in content whose key
// is key, for a warning that quotes what the author wrote.
func firstTopicRef(content, key string) string {
	for line := range strings.SplitSeq(content, "\n") {
		for i := 0; i < len(line); i++ {
			open, body, end, ok := spanAt(line, i)
			if !ok {
				continue
			}
			if open == "[[" && knowledge.NormalizeTitle(body) == key {
				return body
			}
			i = end - 1
		}
	}
	return key
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}
