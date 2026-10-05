package export_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/export"
)

type fakeBase struct {
	topics     []knowledge.Block
	topicsErr  error
	subtrees   map[knowledge.ID][]knowledge.Block
	subtreeErr error
}

func (b *fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) {
	return b.topics, b.topicsErr
}

func (b *fakeBase) GetSubtree(_ context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	if b.subtreeErr != nil {
		return nil, b.subtreeErr
	}
	return b.subtrees[root], nil
}

type fakeFiles struct {
	dir      string
	files    []knowledge.ExportFile
	writeErr error
}

func (w *fakeFiles) WriteFiles(_ context.Context, dir string, files []knowledge.ExportFile) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	w.dir = dir
	w.files = files
	return nil
}

type fakePicker struct {
	answer string
	err    error
}

func (p fakePicker) SaveFile(_, _ string) (string, error)   { return p.answer, p.err }
func (p fakePicker) OpenFile(_ string) (string, error)      { return p.answer, p.err }
func (p fakePicker) OpenDirectory(_ string) (string, error) { return p.answer, p.err }

type fakeHistory struct {
	dir     string
	message string
	commit  host.Commit
	err     error
}

func (h *fakeHistory) Commit(_ context.Context, dir, message string) (host.Commit, error) {
	h.dir, h.message = dir, message
	return h.commit, h.err
}

// fakeScheduler keeps records in memory and runs one when the test says so.
type fakeScheduler struct {
	handler host.ScheduleHandler
	records map[string]host.Schedule
}

func (s *fakeScheduler) Handle(h host.ScheduleHandler, _ host.ScheduleOptions) { s.handler = h }

func (s *fakeScheduler) Put(
	_ context.Context, key, spec string, enabled bool, payload jsontext.Value,
) (host.Schedule, error) {
	if spec == "never" {
		return host.Schedule{}, fmt.Errorf("%w: cron expression %q is not valid", host.ErrInvalidSpec, spec)
	}
	if s.records == nil {
		s.records = map[string]host.Schedule{}
	}
	rec := host.Schedule{Key: key, Spec: spec, Enabled: enabled, Payload: payload}
	s.records[key] = rec
	return rec, nil
}

func (s *fakeScheduler) Get(_ context.Context, key string) (host.Schedule, error) {
	rec, ok := s.records[key]
	if !ok {
		return host.Schedule{}, host.ErrScheduleNotFound
	}
	return rec, nil
}

func (s *fakeScheduler) Delete(_ context.Context, key string) error {
	delete(s.records, key)
	return nil
}

func (s *fakeScheduler) List(context.Context) ([]host.Schedule, error) { return nil, nil }
func (s *fakeScheduler) RunNow(context.Context, string) error          { return nil }
func (s *fakeScheduler) Cancel(context.Context, string) bool           { return false }
func (s *fakeScheduler) Preview(string, int) ([]time.Time, error)      { return nil, nil }

// fire runs the module's one record the way the scheduler would.
func (s *fakeScheduler) fire(t *testing.T) host.RunResult {
	t.Helper()
	require.Len(t, s.records, 1)
	for key, rec := range s.records {
		return s.handler(t.Context(), key, rec.Payload)
	}
	return host.RunResult{}
}

var exportTime = time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)

// oneOff builds a module for the tests of the one-off export, which touch
// neither the schedule nor git.
func oneOff(base host.BaseReader, files host.FileWriter) *export.Module {
	return export.New(base, files, &fakeHistory{}, &fakeScheduler{}, time.Now)
}

type fixture struct {
	m       *export.Module
	files   *fakeFiles
	history *fakeHistory
	sched   *fakeScheduler
}

// withDestination builds a module over a one-topic base whose destination is
// already chosen.
func withDestination(t *testing.T, dir string) fixture {
	t.Helper()
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}
	f := fixture{files: &fakeFiles{}, history: &fakeHistory{}, sched: &fakeScheduler{}}
	f.m = export.New(base, f.files, f.history, f.sched, func() time.Time { return exportTime })
	export.SetPicker(f.m, fakePicker{answer: dir})
	_, err := f.m.ChooseDestination(t.Context())
	require.NoError(t, err)
	return f
}

func blk(id, parent knowledge.ID, content string) knowledge.Block {
	return knowledge.Block{ID: id, ParentID: parent, Content: content}
}

func TestMarkdownWritesOneFilePerTopic(t *testing.T) {
	base := &fakeBase{
		topics: []knowledge.Block{
			blk("t1", "", "# Alpha"),
			blk("c1", "t1", "child of alpha"),
			blk("t2", "", "# Beta"),
		},
		subtrees: map[knowledge.ID][]knowledge.Block{
			"t1": {blk("t1", "", "# Alpha"), blk("c1", "t1", "child of alpha")},
			"t2": {blk("t2", "", "# Beta")},
		},
	}
	files := &fakeFiles{}

	require.NoError(t, oneOff(base, files).Markdown(t.Context(), "/export/here"))

	assert.Equal(t, "/export/here", files.dir, "want the dir passed to Markdown")
	want := []knowledge.ExportFile{
		{Name: "Alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		{Name: "Beta.md", Content: "- # Beta\n"},
		knowledge.MarkdownMarker(),
	}
	assert.Equal(t, want, files.files)
}

func TestMarkdownSurfacesWriteError(t *testing.T) {
	boom := errors.New("disk full")
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}

	err := oneOff(base, &fakeFiles{writeErr: boom}).Markdown(t.Context(), "/export/here")
	assert.ErrorIs(t, err, boom)
}

func TestMarkdownEmptyBase(t *testing.T) {
	files := &fakeFiles{}

	require.NoError(t, oneOff(&fakeBase{}, files).Markdown(t.Context(), "/export/here"))
	assert.Equal(t, []knowledge.ExportFile{knowledge.MarkdownMarker()}, files.files, "want the marker alone")
}

func TestMarkdownWritesNothingOnAReadError(t *testing.T) {
	boom := errors.New("db down")
	base := &fakeBase{
		topics:     []knowledge.Block{blk("t1", "", "# Alpha")},
		subtreeErr: boom,
	}
	files := &fakeFiles{}

	err := oneOff(base, files).Markdown(t.Context(), "/export/here")
	require.ErrorIs(t, err, boom)
	assert.Nil(t, files.files, "wrote files after a read error")
}

func TestExportWritesIntoTheChosenDirectory(t *testing.T) {
	base := &fakeBase{
		topics:   []knowledge.Block{blk("t1", "", "# Alpha")},
		subtrees: map[knowledge.ID][]knowledge.Block{"t1": {blk("t1", "", "# Alpha")}},
	}
	files := &fakeFiles{}
	m := oneOff(base, files)
	export.SetPicker(m, fakePicker{answer: "/vault"})

	got, err := m.Export(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/vault", got, "reported directory")
	assert.Equal(t, "/vault", files.dir, "written directory")
}

// A cancelled dialog is not a failure: the frontend tells the two apart by
// the empty path.
func TestExportCancelledDialogWritesNothing(t *testing.T) {
	files := &fakeFiles{}
	m := oneOff(&fakeBase{topics: []knowledge.Block{blk("t1", "", "# Alpha")}}, files)
	export.SetPicker(m, fakePicker{answer: ""})

	got, err := m.Export(t.Context())
	require.NoError(t, err)
	require.Empty(t, got)
	assert.Nil(t, files.files, "wrote files after the dialog was cancelled")
}

func TestExportReportsAWriteFailure(t *testing.T) {
	boom := errors.New("disk full")
	m := oneOff(&fakeBase{topics: []knowledge.Block{blk("t1", "", "# Alpha")}}, &fakeFiles{writeErr: boom})
	export.SetPicker(m, fakePicker{answer: "/vault"})

	_, err := m.Export(t.Context())
	require.ErrorIs(t, err, boom)
}

func TestDestinationIsEmptyUntilOneIsChosen(t *testing.T) {
	got, err := oneOff(&fakeBase{}, &fakeFiles{}).Destination(t.Context())

	require.NoError(t, err)
	assert.Equal(t, export.Destination{Spec: export.DefaultSpec, Message: export.DefaultMessage}, got)
}

func TestChooseDestinationKeepsTheDirectoryAndSchedulesNothing(t *testing.T) {
	f := withDestination(t, "/vault")

	got, err := f.m.Destination(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "/vault", got.Dir)
	assert.False(t, got.Scheduled)
	assert.False(t, got.Commit)
	assert.Nil(t, f.files.files, "choosing a destination exports nothing")
}

func TestChooseDestinationCancelledKeepsTheEarlierChoices(t *testing.T) {
	f := withDestination(t, "/vault")
	_, err := f.m.Configure(t.Context(), export.Options{Commit: true, Spec: "@hourly", Scheduled: true})
	require.NoError(t, err)
	export.SetPicker(f.m, fakePicker{answer: ""})

	got, err := f.m.ChooseDestination(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "/vault", got.Dir)
	assert.True(t, got.Commit)
	assert.True(t, got.Scheduled)
	assert.Equal(t, "@hourly", got.Spec)
}

func TestChooseDestinationAgainMovesItAndKeepsTheOptions(t *testing.T) {
	f := withDestination(t, "/vault")
	_, err := f.m.Configure(t.Context(), export.Options{Commit: true, Spec: "@hourly", Scheduled: true})
	require.NoError(t, err)
	export.SetPicker(f.m, fakePicker{answer: "/elsewhere"})

	got, err := f.m.ChooseDestination(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "/elsewhere", got.Dir)
	assert.True(t, got.Commit)
	assert.True(t, got.Scheduled)
}

func TestConfigureRefusals(t *testing.T) {
	tests := []struct {
		name     string
		opts     export.Options
		wantCode string
	}{
		{
			name:     "a template that does not parse",
			opts:     export.Options{Commit: true, Message: "{{.Time", Spec: "@daily"},
			wantCode: "export_bad_message",
		},
		{
			name:     "a template naming a field there is none of",
			opts:     export.Options{Commit: true, Message: "{{.Topic}}", Spec: "@daily"},
			wantCode: "export_bad_message",
		},
		{
			name:     "a schedule that is not one",
			opts:     export.Options{Spec: "never", Scheduled: true},
			wantCode: "schedule_invalid_spec",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := withDestination(t, "/vault")

			_, err := f.m.Configure(t.Context(), tc.opts)

			coded, ok := errors.AsType[host.Coded](err)
			require.True(t, ok, "want a coded error, got %v", err)
			assert.Equal(t, tc.wantCode, coded.Code())
			got, err := f.m.Destination(t.Context())
			require.NoError(t, err)
			assert.False(t, got.Commit, "a refused change must leave the destination as it was")
		})
	}
}

func TestConfigureWithoutADestinationIsRefused(t *testing.T) {
	_, err := oneOff(&fakeBase{}, &fakeFiles{}).Configure(t.Context(), export.Options{Spec: "@daily"})

	assert.ErrorIs(t, err, export.ErrNoDestination)
}

func TestExportNowWithoutADestinationIsRefused(t *testing.T) {
	files := &fakeFiles{}

	_, err := oneOff(&fakeBase{}, files).ExportNow(t.Context())

	require.ErrorIs(t, err, export.ErrNoDestination)
	assert.Nil(t, files.files)
}

func TestExportNowWritesTheDestinationWithoutCommitting(t *testing.T) {
	f := withDestination(t, "/vault")

	got, err := f.m.ExportNow(t.Context())

	require.NoError(t, err)
	assert.Equal(t, export.Result{Dir: "/vault"}, got)
	assert.Equal(t, "/vault", f.files.dir)
	assert.Empty(t, f.history.dir, "committed although nobody asked")
}

func TestExportNowCommitsWithTheRenderedMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "the default message", want: "Zamo export (2026-10-05 09:30)"},
		{name: "the owner's own", message: `notes {{.Time.Format "Jan 2"}}`, want: "notes Oct 5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := withDestination(t, "/vault")
			f.history.commit = host.Commit{Hash: "abcdef1234567890"}
			_, err := f.m.Configure(t.Context(), export.Options{Commit: true, Message: tc.message, Spec: "@daily"})
			require.NoError(t, err)

			got, err := f.m.ExportNow(t.Context())

			require.NoError(t, err)
			assert.Equal(t, export.Result{Dir: "/vault", Commit: "abcdef1234567890"}, got)
			assert.Equal(t, "/vault", f.history.dir)
			assert.Equal(t, tc.want, f.history.message)
		})
	}
}

func TestExportNowDoesNotCommitWhatItFailedToWrite(t *testing.T) {
	f := withDestination(t, "/vault")
	_, err := f.m.Configure(t.Context(), export.Options{Commit: true, Spec: "@daily"})
	require.NoError(t, err)
	f.files.writeErr = errors.New("disk full")

	_, err = f.m.ExportNow(t.Context())

	require.ErrorIs(t, err, f.files.writeErr)
	assert.Empty(t, f.history.dir)
}

func TestScheduledExportReportsHowItWent(t *testing.T) {
	tests := []struct {
		name      string
		commit    bool
		made      host.Commit
		writeErr  error
		commitErr error
		want      host.RunResult
	}{
		{name: "no git", want: host.RunResult{Outcome: host.OutcomeOK}},
		{
			name: "a commit", commit: true, made: host.Commit{Hash: "abcdef1234567890"},
			want: host.RunResult{Outcome: host.OutcomeOK, Detail: "committed abcdef1"},
		},
		{name: "nothing changed", commit: true, want: host.RunResult{Outcome: host.OutcomeNoChange}},
		{
			name: "the write failed", commit: true, writeErr: errors.New("disk full"),
			want: host.RunResult{Outcome: host.OutcomeFailed, Detail: "disk full"},
		},
		{
			name: "git refused", commit: true, commitErr: errors.New("refusing to commit"),
			want: host.RunResult{Outcome: host.OutcomeFailed, Detail: "refusing to commit"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := withDestination(t, "/vault")
			_, err := f.m.Configure(t.Context(), export.Options{Commit: tc.commit, Spec: "@daily", Scheduled: true})
			require.NoError(t, err)
			f.files.writeErr = tc.writeErr
			f.history.commit, f.history.err = tc.made, tc.commitErr

			got := f.sched.fire(t)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestForgetDestinationDropsItAndItsSchedule(t *testing.T) {
	f := withDestination(t, "/vault")

	require.NoError(t, f.m.ForgetDestination(t.Context()))

	got, err := f.m.Destination(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got.Dir)
	assert.Empty(t, f.sched.records)
}
