// Package export is the feature module that writes the base to disk as
// portable markdown: one file per topic, in the shape the topic has.
package export

import (
	"context"

	"github.com/cominddev/comind/internal/host"
	"github.com/cominddev/comind/internal/knowledge"
)

// Module exports the base. Its exported methods are bound into the desktop
// frontend as the export service.
type Module struct {
	base  host.BaseReader
	files host.FileWriter
}

// New assembles the module from the capabilities it needs.
func New(base host.BaseReader, files host.FileWriter) *Module {
	return &Module{base: base, files: files}
}

// Markdown exports the base into dir: one markdown file per topic, block
// nesting rendered as nested lists. All reads complete before any write, so a
// read failure aborts with nothing written; how a mid-write failure is
// handled is the writer's concern.
func (m *Module) Markdown(ctx context.Context, dir string) error {
	topics, err := m.base.TopicMap(ctx)
	if err != nil {
		return err
	}

	var subtrees [][]knowledge.Block
	for _, t := range topics {
		if !t.IsTopic() { // TopicMap also returns first-level children; skip them
			continue
		}
		sub, err := m.base.GetSubtree(ctx, t.ID)
		if err != nil {
			return err
		}
		subtrees = append(subtrees, sub)
	}

	return m.files.WriteFiles(ctx, dir, knowledge.RenderMarkdown(subtrees))
}
