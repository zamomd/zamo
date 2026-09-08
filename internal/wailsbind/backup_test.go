package wailsbind

import (
	"context"
	"errors"
	"testing"
)

type fakeArchive struct {
	backedUpTo string
	staged     string
	pending    string
	cancelled  bool
	err        error
}

func (f *fakeArchive) Backup(_ context.Context, dest string) error {
	if f.err != nil {
		return f.err
	}
	f.backedUpTo = dest
	return nil
}

func (f *fakeArchive) StageRestore(_ context.Context, archive string) error {
	if f.err != nil {
		return f.err
	}
	f.staged = archive
	return nil
}

func (f *fakeArchive) PendingRestore() string { return f.pending }

func (f *fakeArchive) CancelRestore() error {
	f.cancelled = true
	return f.err
}

type fakePicker struct {
	answer string
	err    error
}

func (p fakePicker) SaveFile(_, _ string) (string, error) { return p.answer, p.err }
func (p fakePicker) OpenFile(_ string) (string, error)    { return p.answer, p.err }

func TestBackupBaseWritesWhereTheUserChose(t *testing.T) {
	archive := &fakeArchive{}
	app := New(nil, nil, nil, archive)
	SetPicker(app, fakePicker{answer: "/tmp/chosen.comind-backup"})

	got, err := app.BackupBase(t.Context())
	if err != nil {
		t.Fatalf("BackupBase: %v", err)
	}
	if got != "/tmp/chosen.comind-backup" {
		t.Errorf("reported %q, want the chosen path", got)
	}
	if archive.backedUpTo != "/tmp/chosen.comind-backup" {
		t.Errorf("wrote to %q, want the chosen path", archive.backedUpTo)
	}
}

// A cancelled dialog is not a failure: the frontend distinguishes the two by
// the empty path, and showing an error for "I changed my mind" is noise.
func TestCancellingADialogWritesNothing(t *testing.T) {
	t.Run("backup", func(t *testing.T) {
		archive := &fakeArchive{}
		app := New(nil, nil, nil, archive)
		SetPicker(app, fakePicker{answer: ""})

		got, err := app.BackupBase(t.Context())
		if err != nil || got != "" {
			t.Fatalf("BackupBase = %q, %v; want an empty path and no error", got, err)
		}
		if archive.backedUpTo != "" {
			t.Errorf("wrote to %q after the dialog was cancelled", archive.backedUpTo)
		}
	})

	t.Run("restore", func(t *testing.T) {
		archive := &fakeArchive{}
		app := New(nil, nil, nil, archive)
		SetPicker(app, fakePicker{answer: ""})

		got, err := app.RestoreBase(t.Context())
		if err != nil || got != "" {
			t.Fatalf("RestoreBase = %q, %v; want an empty path and no error", got, err)
		}
		if archive.staged != "" {
			t.Errorf("staged %q after the dialog was cancelled", archive.staged)
		}
	})
}

func TestRestoreBaseStagesTheChosenArchive(t *testing.T) {
	archive := &fakeArchive{}
	app := New(nil, nil, nil, archive)
	SetPicker(app, fakePicker{answer: "/tmp/base.comind-backup"})

	got, err := app.RestoreBase(t.Context())
	if err != nil {
		t.Fatalf("RestoreBase: %v", err)
	}
	if got != "/tmp/base.comind-backup" || archive.staged != got {
		t.Errorf("reported %q and staged %q, want both to be the chosen archive", got, archive.staged)
	}
}

// A refused archive must reach the frontend as an error rather than as a
// quiet empty answer, which the UI would read as "cancelled".
func TestRestoreBaseReportsARefusedArchive(t *testing.T) {
	refused := errors.New("not a Comind backup archive")
	app := New(nil, nil, nil, &fakeArchive{err: refused})
	SetPicker(app, fakePicker{answer: "/tmp/junk"})

	if _, err := app.RestoreBase(t.Context()); !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the refusal passed through", err)
	}
}

func TestPendingAndCancelPassThrough(t *testing.T) {
	archive := &fakeArchive{pending: "/tmp/base.comind-backup"}
	app := New(nil, nil, nil, archive)

	got, err := app.PendingRestore(t.Context())
	if err != nil || got != "/tmp/base.comind-backup" {
		t.Fatalf("PendingRestore = %q, %v", got, err)
	}
	if err := app.CancelRestore(t.Context()); err != nil {
		t.Fatalf("CancelRestore: %v", err)
	}
	if !archive.cancelled {
		t.Error("CancelRestore did not reach the archive")
	}
}
