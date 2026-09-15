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
	base   host.BaseReader
	files  host.FileWriter
	picker host.FilePicker
}

// New assembles the module from the capabilities it needs. The file picker
// arrives through SetPicker once the desktop application exists.
func New(base host.BaseReader, files host.FileWriter) *Module {
	return &Module{base: base, files: files}
}

// SetPicker wires the platform file dialogs, which only exist once the
// desktop application does. A package function rather than a method so it
// is not itself exposed as a frontend binding.
func SetPicker(m *Module, p host.FilePicker) { m.picker = p }

// Export asks for a directory and writes the base into it as markdown,
// answering with the directory it wrote. An empty answer means the user
// cancelled. Files named after topics already in the directory are
// overwritten; nothing else in it is touched.
func (m *Module) Export(ctx context.Context) (string, error) {
	dir, err := m.picker.OpenDirectory("Export the Comind base as Markdown")
	if err != nil || dir == "" {
		return "", err
	}
	if err := m.Markdown(ctx, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// Markdown exports the base into dir: one markdown file per topic, block
// nesting rendered as nested lists, and the marker file that lets an import
// recognise the directory. All reads complete before any write, so a read
// failure aborts with nothing written; how a mid-write failure is handled is
// the writer's concern.
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

	files := append(knowledge.RenderMarkdown(subtrees), knowledge.MarkdownMarker())
	return m.files.WriteFiles(ctx, dir, files)
}
