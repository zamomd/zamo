package wailsbind

import "context"

// BaseArchive captures and restores the whole base. backup.Base satisfies it;
// tests pass a fake.
type BaseArchive interface {
	Backup(ctx context.Context, dest string) error
	StageRestore(ctx context.Context, archive string) error
	PendingRestore() string
	CancelRestore() error
}

// FilePicker asks the user for a path through the platform's own dialog.
// The Wails application supplies it; an empty path means the user cancelled.
//
// An interface rather than a direct call into the Wails application package,
// for the same reason as Emitter: these methods stay callable headlessly.
type FilePicker interface {
	SaveFile(title, suggestedName string) (string, error)
	OpenFile(title string) (string, error)
}

// BackupBase asks where to write a snapshot of the base and writes it,
// answering with the path it wrote. An empty answer means the user cancelled.
func (a *App) BackupBase(ctx context.Context) (string, error) {
	dest, err := a.picker.SaveFile("Back up the Comind base", "comind-base.comind-backup")
	if err != nil || dest == "" {
		return "", err
	}
	if err := a.archive.Backup(ctx, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// RestoreBase asks for an archive and accepts it for the next start,
// answering with the path it accepted. An empty answer means the user
// cancelled. The base itself is replaced at the next launch — see
// backup.Stage for why it cannot happen while the app is running.
func (a *App) RestoreBase(ctx context.Context) (string, error) {
	archive, err := a.picker.OpenFile("Restore the Comind base from a backup")
	if err != nil || archive == "" {
		return "", err
	}
	if err := a.archive.StageRestore(ctx, archive); err != nil {
		return "", err
	}
	return archive, nil
}

// PendingRestore reports the archive that will replace the base at the next
// start, or "" when there is none.
func (a *App) PendingRestore(_ context.Context) (string, error) {
	return a.archive.PendingRestore(), nil
}

// CancelRestore drops a pending restore, leaving the live base in place.
func (a *App) CancelRestore(_ context.Context) error {
	return a.archive.CancelRestore()
}
