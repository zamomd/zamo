package wailsbind

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/scheduler"
)

type fakeLister struct{ entries []scheduler.Entry }

func (f fakeLister) ListAll(context.Context) ([]scheduler.Entry, error) { return f.entries, nil }

func TestListSchedulesFlattensEveryModulesEntries(t *testing.T) {
	app := New(nil, nil, nil)
	SetScheduler(app, fakeLister{entries: []scheduler.Entry{{
		Module: "smartcron", Available: true,
		Key: "blk", Spec: "0 9 * * *", Enabled: true, Outcome: scheduler.OutcomeSkipped, Detail: "3 unreviewed",
	}, {
		Module: "gone", Key: "x", Spec: "@daily",
	}}})

	got, err := app.ListSchedules(t.Context())

	require.NoError(t, err)
	require.Len(t, got, 2)
	row := got[0]
	assert.Equal(t, "smartcron", row.Module)
	assert.True(t, row.Available)
	assert.Equal(t, "skipped", row.Outcome)
	assert.Equal(t, "3 unreviewed", row.Detail)
	assert.False(t, got[1].Available, "a module without a handler is unavailable")
}

func TestListSchedulesWithoutASchedulerIsEmpty(t *testing.T) {
	got, err := New(nil, nil, nil).ListSchedules(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

type recordingEmitter struct {
	name string
	data []any
}

func (r *recordingEmitter) Emit(name string, data ...any) bool {
	r.name, r.data = name, data
	return true
}

func TestEmitScheduleRunsForwardsTheEvent(t *testing.T) {
	em := &recordingEmitter{}
	forward := EmitScheduleRuns(func() Emitter { return em })

	forward(scheduler.Event{Module: "smartcron", Key: "blk", Outcome: scheduler.OutcomeOK})

	require.Equal(t, ScheduleRunEvent, em.name)
	require.Len(t, em.data, 1)
	p, ok := em.data[0].(ScheduleRun)
	require.True(t, ok, "payload = %#v, want a ScheduleRun", em.data[0])
	assert.Equal(t, "blk", p.Key)
	assert.Equal(t, "ok", p.Outcome)
	assert.False(t, p.Running)
}

func TestEmitScheduleRunsIsQuietWithoutAnEmitter(t *testing.T) {
	forward := EmitScheduleRuns(func() Emitter { return nil })
	forward(scheduler.Event{Module: "m", Key: "k", Running: true}) // must not panic
}
