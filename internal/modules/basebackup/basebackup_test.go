package basebackup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/modules/basebackup"
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

func (p fakePicker) SaveFile(_, _ string) (string, error)   { return p.answer, p.err }
func (p fakePicker) OpenFile(_ string) (string, error)      { return p.answer, p.err }
func (p fakePicker) OpenDirectory(_ string) (string, error) { return p.answer, p.err }

func TestBackupWritesWhereTheUserChose(t *testing.T) {
	archive := &fakeArchive{}
	m := basebackup.New(archive)
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/chosen.comind-backup"})

	got, err := m.Backup(t.Context())
	if err != nil {
		t.Fatalf("Backup: %v", err)
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
		m := basebackup.New(archive)
		basebackup.SetPicker(m, fakePicker{answer: ""})

		got, err := m.Backup(t.Context())
		if err != nil || got != "" {
			t.Fatalf("Backup = %q, %v; want an empty path and no error", got, err)
		}
		if archive.backedUpTo != "" {
			t.Errorf("wrote to %q after the dialog was cancelled", archive.backedUpTo)
		}
	})

	t.Run("restore", func(t *testing.T) {
		archive := &fakeArchive{}
		m := basebackup.New(archive)
		basebackup.SetPicker(m, fakePicker{answer: ""})

		got, err := m.Restore(t.Context())
		if err != nil || got != "" {
			t.Fatalf("Restore = %q, %v; want an empty path and no error", got, err)
		}
		if archive.staged != "" {
			t.Errorf("staged %q after the dialog was cancelled", archive.staged)
		}
	})
}

func TestRestoreStagesTheChosenArchive(t *testing.T) {
	archive := &fakeArchive{}
	m := basebackup.New(archive)
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/base.comind-backup"})

	got, err := m.Restore(t.Context())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got != "/tmp/base.comind-backup" || archive.staged != got {
		t.Errorf("reported %q and staged %q, want both to be the chosen archive", got, archive.staged)
	}
}

// A refused archive must reach the frontend as an error rather than as a
// quiet empty answer, which the UI would read as "cancelled".
func TestRestoreReportsARefusedArchive(t *testing.T) {
	refused := errors.New("not a Comind backup archive")
	m := basebackup.New(&fakeArchive{err: refused})
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/junk"})

	if _, err := m.Restore(t.Context()); !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the refusal passed through", err)
	}
}

func TestPendingAndCancelPassThrough(t *testing.T) {
	archive := &fakeArchive{pending: "/tmp/base.comind-backup"}
	m := basebackup.New(archive)

	got, err := m.PendingRestore(t.Context())
	if err != nil || got != "/tmp/base.comind-backup" {
		t.Fatalf("PendingRestore = %q, %v", got, err)
	}
	if err := m.CancelRestore(t.Context()); err != nil {
		t.Fatalf("CancelRestore: %v", err)
	}
	if !archive.cancelled {
		t.Error("CancelRestore did not reach the archive")
	}
}
