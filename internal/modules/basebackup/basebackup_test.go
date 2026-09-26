package basebackup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/modules/basebackup"
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
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/chosen.zamo-backup"})

	got, err := m.Backup(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/tmp/chosen.zamo-backup", got, "reported path")
	assert.Equal(t, "/tmp/chosen.zamo-backup", archive.backedUpTo, "written path")
}

// A cancelled dialog is not a failure: the frontend distinguishes the two by
// the empty path, and showing an error for "I changed my mind" is noise.
func TestCancellingADialogWritesNothing(t *testing.T) {
	t.Run("backup", func(t *testing.T) {
		archive := &fakeArchive{}
		m := basebackup.New(archive)
		basebackup.SetPicker(m, fakePicker{answer: ""})

		got, err := m.Backup(t.Context())
		require.NoError(t, err)
		require.Empty(t, got)
		assert.Empty(t, archive.backedUpTo, "wrote after the dialog was cancelled")
	})

	t.Run("restore", func(t *testing.T) {
		archive := &fakeArchive{}
		m := basebackup.New(archive)
		basebackup.SetPicker(m, fakePicker{answer: ""})

		got, err := m.Restore(t.Context())
		require.NoError(t, err)
		require.Empty(t, got)
		assert.Empty(t, archive.staged, "staged after the dialog was cancelled")
	})
}

func TestRestoreStagesTheChosenArchive(t *testing.T) {
	archive := &fakeArchive{}
	m := basebackup.New(archive)
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/base.zamo-backup"})

	got, err := m.Restore(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/tmp/base.zamo-backup", got, "reported archive")
	assert.Equal(t, "/tmp/base.zamo-backup", archive.staged, "staged archive")
}

// A refused archive must reach the frontend as an error rather than as a
// quiet empty answer, which the UI would read as "cancelled".
func TestRestoreReportsARefusedArchive(t *testing.T) {
	refused := errors.New("not a Zamo backup archive")
	m := basebackup.New(&fakeArchive{err: refused})
	basebackup.SetPicker(m, fakePicker{answer: "/tmp/junk"})

	_, err := m.Restore(t.Context())
	require.ErrorIs(t, err, refused)
}

func TestPendingAndCancelPassThrough(t *testing.T) {
	archive := &fakeArchive{pending: "/tmp/base.zamo-backup"}
	m := basebackup.New(archive)

	got, err := m.PendingRestore(t.Context())
	require.NoError(t, err)
	require.Equal(t, "/tmp/base.zamo-backup", got)
	require.NoError(t, m.CancelRestore(t.Context()))
	assert.True(t, archive.cancelled, "CancelRestore did not reach the archive")
}
