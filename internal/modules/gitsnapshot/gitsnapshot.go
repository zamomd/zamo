// Package gitsnapshot is the feature module that exports the base as
// markdown into a folder the user chose and commits the change there,
// binding the host.GitSnapshot capability to the desktop frontend.
package gitsnapshot

import (
	"context"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// Module drives an export-and-commit cycle through the platform's file
// dialogs. Its exported methods are bound into the desktop frontend as the
// git snapshot service.
type Module struct {
	base     host.BaseReader
	files    host.FileWriter
	snapshot host.GitSnapshot
	picker   host.FilePicker
}

// New assembles the module from the capabilities it needs. The file picker
// arrives through SetPicker once the desktop application exists.
func New(base host.BaseReader, files host.FileWriter, snapshot host.GitSnapshot) *Module {
	return &Module{base: base, files: files, snapshot: snapshot}
}

// SetPicker wires the platform file dialogs, which only exist once the
// desktop application does. A package function rather than a method so it
// is not itself exposed as a frontend binding.
func SetPicker(m *Module, p host.FilePicker) { m.picker = p }

// Status reports the current preferences and the latest commit under the
// managed directory, if one has been chosen.
func (m *Module) Status(_ context.Context) (host.GitSnapshotStatus, error) {
	return m.snapshot.Status()
}

// PickDirectory asks through the platform's own folder picker and persists
// the choice, answering with the path picked. An empty answer means the
// user cancelled.
func (m *Module) PickDirectory(_ context.Context) (string, error) {
	dir, err := m.picker.OpenDirectory("Choose the Git Snapshot folder")
	if err != nil || dir == "" {
		return "", err
	}
	if err := m.snapshot.SetDirectory(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// SetCommitTemplate validates and persists the commit message template.
func (m *Module) SetCommitTemplate(_ context.Context, tmpl string) error {
	return m.snapshot.SetCommitTemplate(tmpl)
}

// ExportAndCommit renders the base into the managed directory and commits
// it if the render actually changed something there.
func (m *Module) ExportAndCommit(ctx context.Context) (host.GitSnapshotResult, error) {
	return m.snapshot.ExportAndCommit(ctx, m.renderMarkdown)
}

// renderMarkdown writes the base into dir as markdown. Deliberately mirrors
// export.Module.Markdown rather than importing it: depguard's
// modules-host-only rule only lets this package see host, knowledge and
// other modules' source paths, not another module's concrete code, and
// importing a sibling module would make both modules unable to be deleted
// independently.
func (m *Module) renderMarkdown(ctx context.Context, dir string) error {
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
