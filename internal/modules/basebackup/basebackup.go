// Package basebackup is the feature module that lets the user back up the
// whole base to a file of their choosing and restore it from one.
package basebackup

import (
	"context"

	"github.com/cominddev/comind/internal/host"
)

// Module drives backup and restore through the platform's file dialogs. Its
// exported methods are bound into the desktop frontend as the base-backup
// service.
type Module struct {
	archive host.BaseArchive
	picker  host.FilePicker
}

// New assembles the module around the archive capability. The file picker
// arrives through SetPicker once the desktop application exists.
func New(archive host.BaseArchive) *Module {
	return &Module{archive: archive}
}

// SetPicker wires the platform file dialogs, which only exist once the
// desktop application does. A package function rather than a method so it
// is not itself exposed as a frontend binding.
func SetPicker(m *Module, p host.FilePicker) { m.picker = p }

// Backup asks where to write a snapshot of the base and writes it, answering
// with the path it wrote. An empty answer means the user cancelled.
func (m *Module) Backup(ctx context.Context) (string, error) {
	dest, err := m.picker.SaveFile("Back up the Comind base", "comind-base.comind-backup")
	if err != nil || dest == "" {
		return "", err
	}
	if err := m.archive.Backup(ctx, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Restore asks for an archive and accepts it for the next start, answering
// with the path it accepted. An empty answer means the user cancelled. The
// base itself is replaced at the next launch, when no process holds it.
func (m *Module) Restore(ctx context.Context) (string, error) {
	archive, err := m.picker.OpenFile("Restore the Comind base from a backup")
	if err != nil || archive == "" {
		return "", err
	}
	if err := m.archive.StageRestore(ctx, archive); err != nil {
		return "", err
	}
	return archive, nil
}

// PendingRestore reports the archive that will replace the base at the next
// start, or "" when there is none.
func (m *Module) PendingRestore(_ context.Context) (string, error) {
	return m.archive.PendingRestore(), nil
}

// CancelRestore drops a pending restore, leaving the live base in place.
func (m *Module) CancelRestore(_ context.Context) error {
	return m.archive.CancelRestore()
}
