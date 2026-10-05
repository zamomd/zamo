package scheduler

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/jobs"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// monday9 is a Monday 09:00, so "0 9 * * *" has just fired.
var monday9 = time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

type fixture struct {
	*Scheduler
	store  *memStore
	clock  *clock
	events []Event
	evMu   sync.Mutex
}

// wait blocks until every run in flight has settled.
func (f *fixture) wait() { f.wg.Wait() }

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	f := &fixture{store: newMemStore(), clock: &clock{t: monday9}}
	opts.OnRun = func(e Event) {
		f.evMu.Lock()
		defer f.evMu.Unlock()
		f.events = append(f.events, e)
	}
	seq := 0
	var idMu sync.Mutex
	newID := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		seq++
		return fmt.Sprintf("id-%03d", seq)
	}
	s, err := New(f.store, jobs.New(t.Context(), newID, f.clock.now), f.clock.now, newID, opts)
	require.NoError(t, err, "New")
	f.Scheduler = s
	s.begin(t.Context())
	return f
}

// counting returns a handler that records its calls and answers ok.
func counting(calls *[]string, mu *sync.Mutex) Handler {
	return func(_ context.Context, key string, _ jsontext.Value) RunResult {
		mu.Lock()
		defer mu.Unlock()
		*calls = append(*calls, key)
		return RunResult{Outcome: OutcomeOK}
	}
}

func mustPut(t *testing.T, m *Module, key, spec string, enabled bool) Schedule {
	t.Helper()
	got, err := m.Put(t.Context(), key, spec, enabled, nil)
	require.NoError(t, err, "Put %s", key)
	return got
}

func mustRunNow(t *testing.T, m *Module, key string) {
	t.Helper()
	require.NoError(t, m.RunNow(t.Context(), key), "RunNow %s", key)
}

func mustStore(t *testing.T, store *memStore, rec Record) {
	t.Helper()
	_, err := store.Put(t.Context(), rec)
	require.NoError(t, err, "store.Put %s", rec.Key)
}

func (f *fixture) tickAndWait(t *testing.T) {
	t.Helper()
	f.tick(t.Context())
	f.wait()
}

func TestOverdueRecordRunsOnceHoweverManySlotsPassed(t *testing.T) {
	f := newFixture(t, Options{})
	var calls []string
	var mu sync.Mutex
	sched := f.For("m")
	sched.Handle(counting(&calls, &mu), HandlerOptions{})
	_, err := sched.Put(t.Context(), "k", "0 9 * * *", true, nil)
	require.NoError(t, err)

	f.clock.advance(14 * 24 * time.Hour)
	f.tickAndWait(t)
	f.tickAndWait(t)

	require.Len(t, calls, 1, "handler runs after two weeks away")
	got, _ := sched.Get(t.Context(), "k")
	assert.True(t, got.NextRun.After(f.clock.now()), "NextRun = %v, want in the future", got.NextRun)
	assert.Equal(t, OutcomeOK, got.Outcome)
	assert.WithinDuration(t, f.clock.now(), got.LastRun, 0, "LastRun")
}

func TestSavingAScheduleDoesNotRunIt(t *testing.T) {
	f := newFixture(t, Options{})
	var calls []string
	var mu sync.Mutex
	sched := f.For("m")
	sched.Handle(counting(&calls, &mu), HandlerOptions{})

	got, err := sched.Put(t.Context(), "k", "0 9 * * *", true, nil)
	f.tickAndWait(t)

	require.NoError(t, err)
	require.Empty(t, calls, "a saved schedule must wait for its slot")
	assert.WithinDuration(t, monday9.Add(24*time.Hour), got.NextRun, 0, "the slot that is now has already passed")
}

func TestDisabledScheduleHasNoNextRunAndEnablingCountsFromNow(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.For("m")
	sched.Handle(func(context.Context, string, jsontext.Value) RunResult { return RunResult{} }, HandlerOptions{})

	off, _ := sched.Put(t.Context(), "k", "0 9 * * *", false, nil)
	f.clock.advance(72 * time.Hour)
	on, _ := sched.Put(t.Context(), "k", "0 9 * * *", true, nil)

	assert.True(t, off.NextRun.IsZero(), "disabled NextRun = %v, want none", off.NextRun)
	assert.WithinDuration(t, monday9.Add(96*time.Hour), on.NextRun, 0, "enabled: the pause is not caught up")
}

func TestInvalidSpecIsRefusedTheSameWayByPutAndPreview(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.For("m")

	_, putErr := sched.Put(t.Context(), "k", "every monday", true, nil)
	_, previewErr := sched.Preview("every monday", 2)

	require.ErrorIs(t, putErr, ErrInvalidSpec, "Put")
	require.ErrorIs(t, previewErr, ErrInvalidSpec, "Preview")
	assert.EqualError(t, previewErr, putErr.Error())
}

func TestPreviewListsTheNextRuns(t *testing.T) {
	f := newFixture(t, Options{})

	got, err := f.For("m").Preview("0 9 * * *", 2)

	want := []time.Time{monday9.Add(24 * time.Hour), monday9.Add(48 * time.Hour)}
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.WithinDuration(t, want[0], got[0], 0)
	assert.WithinDuration(t, want[1], got[1], 0)
}

func TestSecondDueRecordWaitsUnclaimedForAFreeSlot(t *testing.T) {
	f := newFixture(t, Options{})
	release := make(chan struct{})
	started := make(chan string, 2)
	sched := f.For("m")
	sched.Handle(func(_ context.Context, key string, _ jsontext.Value) RunResult {
		started <- key
		<-release
		return RunResult{Outcome: OutcomeOK}
	}, HandlerOptions{MaxConcurrent: 1})
	mustPut(t, sched, "a", "0 9 * * *", true)
	mustPut(t, sched, "b", "0 9 * * *", true)
	f.clock.advance(24 * time.Hour)

	f.tick(t.Context())
	first := <-started

	second := "a"
	if first == "a" {
		second = "b"
	}
	waiting, _ := f.store.Get(t.Context(), "m", second)
	assert.Empty(t, waiting.ClaimedBy,
		"%s is claimed while waiting, want it unclaimed so the wait does not spend its lease", second)
	close(release)
	f.wait()
	f.tickAndWait(t)
	select {
	case got := <-started:
		assert.Equal(t, second, got, "second run")
	default:
		assert.Failf(t, "second run missing", "%s never ran after the slot was freed", second)
	}
}

func TestRecordWithoutAHandlerNeverRunsAndIsListedAsUnavailable(t *testing.T) {
	f := newFixture(t, Options{})
	mustStore(t, f.store, Record{ID: "x", Module: "gone", Key: "k", Spec: "0 9 * * *", Enabled: true, NextRun: monday9})

	f.clock.advance(time.Hour)
	f.tickAndWait(t)

	rec, _ := f.store.Get(t.Context(), "gone", "k")
	assert.True(t, rec.LastRun.IsZero(), "LastRun = %v, want the record left alone", rec.LastRun)
	assert.Empty(t, rec.ClaimedBy)
	all, err := f.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.False(t, all[0].Available)
	assert.Equal(t, "gone", all[0].Module)
}

func TestStartDropsTheSlotsThatPassedWhileNothingRan(t *testing.T) {
	f := newFixture(t, Options{})
	var calls []string
	var mu sync.Mutex
	sched := f.For("m")
	sched.Handle(counting(&calls, &mu), HandlerOptions{})
	mustStore(t, f.store, Record{
		ID: "x", Module: "m", Key: "late", Spec: "0 9 * * *", Enabled: true, NextRun: monday9.Add(-72 * time.Hour),
	})

	f.Start(t.Context())
	f.tickAndWait(t)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, calls, "a slot missed while closed must not run at launch")
	got, _ := sched.Get(t.Context(), "late")
	assert.WithinDuration(t, monday9.Add(24*time.Hour), got.NextRun, 0, "NextRun")
	assert.True(t, got.LastRun.IsZero(), "LastRun = %v, want nothing recorded", got.LastRun)
}

func TestCancellingATimedRunGivesUpItsSlot(t *testing.T) {
	f := newFixture(t, Options{})
	started := make(chan struct{}, 2)
	sched := f.For("m")
	sched.Handle(func(ctx context.Context, _ string, _ jsontext.Value) RunResult {
		started <- struct{}{}
		<-ctx.Done()
		return RunResult{}
	}, HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)
	f.clock.advance(24 * time.Hour)
	f.tick(t.Context())
	<-started

	require.True(t, sched.Cancel(t.Context(), "k"), "Cancel must find the run")
	f.wait()
	f.tickAndWait(t)

	assert.Empty(t, started, "the cancelled run started again")
	got, _ := sched.Get(t.Context(), "k")
	assert.WithinDuration(t, monday9.Add(48*time.Hour), got.NextRun, 0, "NextRun")
	assert.Empty(t, got.Outcome)
	f.evMu.Lock()
	defer f.evMu.Unlock()
	require.Len(t, f.events, 2)
	assert.False(t, f.events[1].Running, "the frontend must hear that the run is over")
}

func TestRunNowIsRefusedWhileTheRecordRuns(t *testing.T) {
	f := newFixture(t, Options{})
	release := make(chan struct{})
	started := make(chan struct{})
	sched := f.For("m")
	sched.Handle(func(context.Context, string, jsontext.Value) RunResult {
		close(started)
		<-release
		return RunResult{Outcome: OutcomeOK}
	}, HandlerOptions{MaxConcurrent: 2})
	mustPut(t, sched, "k", "0 9 * * *", true)

	require.NoError(t, sched.RunNow(t.Context(), "k"), "first RunNow")
	<-started
	err := sched.RunNow(t.Context(), "k")
	close(release)
	f.wait()

	assert.ErrorIs(t, err, ErrBusy, "second RunNow")
}

func TestCancelledRunReleasesTheClaimAndRecordsNothing(t *testing.T) {
	f := newFixture(t, Options{})
	started := make(chan struct{})
	sched := f.For("m")
	sched.Handle(func(ctx context.Context, _ string, _ jsontext.Value) RunResult {
		close(started)
		<-ctx.Done()
		return RunResult{Outcome: OutcomeFailed, Detail: "context canceled"}
	}, HandlerOptions{})
	saved, _ := sched.Put(t.Context(), "k", "0 9 * * *", true, nil)
	mustRunNow(t, sched, "k")
	<-started

	require.True(t, sched.Cancel(t.Context(), "k"), "Cancel must find the run")
	f.wait()

	rec, _ := f.store.Get(t.Context(), "m", "k")
	assert.Empty(t, rec.ClaimedBy, "claim must be released")
	assert.True(t, rec.LastRun.IsZero(), "LastRun = %v, want nothing recorded", rec.LastRun)
	assert.Empty(t, rec.Outcome)
	assert.WithinDuration(t, saved.NextRun, rec.NextRun, 0, "NextRun")
}

func TestDisablingOrDeletingCancelsTheRun(t *testing.T) {
	for _, stop := range []string{"disable", "delete"} {
		t.Run(stop, func(t *testing.T) {
			f := newFixture(t, Options{})
			started := make(chan struct{})
			cancelled := make(chan struct{})
			sched := f.For("m")
			sched.Handle(func(ctx context.Context, _ string, _ jsontext.Value) RunResult {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return RunResult{}
			}, HandlerOptions{})
			mustPut(t, sched, "k", "0 9 * * *", true)
			mustRunNow(t, sched, "k")
			<-started

			if stop == "disable" {
				mustPut(t, sched, "k", "0 9 * * *", false)
			} else {
				require.NoError(t, sched.Delete(t.Context(), "k"))
			}

			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				require.FailNow(t, "the run was not cancelled")
			}
			f.wait()
		})
	}
}

func TestEditDuringARunMakesTheNextRunFollowTheNewSpec(t *testing.T) {
	f := newFixture(t, Options{})
	release := make(chan struct{})
	started := make(chan struct{})
	sched := f.For("m")
	sched.Handle(func(context.Context, string, jsontext.Value) RunResult {
		close(started)
		<-release
		return RunResult{Outcome: OutcomeOK}
	}, HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)
	mustRunNow(t, sched, "k")
	<-started

	mustPut(t, sched, "k", "0 17 * * *", true)
	close(release)
	f.wait()

	got, _ := sched.Get(t.Context(), "k")
	assert.WithinDuration(t, monday9.Add(8*time.Hour), got.NextRun, 0, "next run must follow the new spec")
	assert.Equal(t, OutcomeOK, got.Outcome)
}

func TestPanickingHandlerIsRecordedAsFailed(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.For("m")
	sched.Handle(func(context.Context, string, jsontext.Value) RunResult { panic("boom") }, HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)

	mustRunNow(t, sched, "k")
	f.wait()

	got, _ := sched.Get(t.Context(), "k")
	assert.Equal(t, OutcomeFailed, got.Outcome)
	assert.NotEmpty(t, got.Detail)
}

func TestRunTimeoutIsRecordedAsFailed(t *testing.T) {
	f := newFixture(t, Options{RunTimeout: 20 * time.Millisecond, Lease: time.Minute})
	sched := f.For("m")
	sched.Handle(func(ctx context.Context, _ string, _ jsontext.Value) RunResult {
		<-ctx.Done()
		return RunResult{}
	}, HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)

	mustRunNow(t, sched, "k")
	f.wait()

	got, _ := sched.Get(t.Context(), "k")
	assert.Equal(t, OutcomeFailed, got.Outcome)
}

func TestLeaseMustOutliveTheRunTimeout(t *testing.T) {
	_, err := New(newMemStore(), nil, time.Now, func() string { return "id" },
		Options{Lease: time.Minute, RunTimeout: 2 * time.Minute})

	assert.Error(t, err)
}

func TestModulesSeeOnlyTheirOwnRecords(t *testing.T) {
	f := newFixture(t, Options{})
	mustPut(t, f.For("a"), "k", "0 9 * * *", true)

	_, err := f.For("b").Get(t.Context(), "k")
	list, _ := f.For("b").List(t.Context())

	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, list)
}

func TestRunEventsBracketTheRun(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.For("m")
	sched.Handle(func(context.Context, string, jsontext.Value) RunResult {
		return RunResult{Outcome: OutcomeSkipped, Detail: "3 unreviewed changes in scope"}
	}, HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)

	mustRunNow(t, sched, "k")
	f.wait()

	f.evMu.Lock()
	defer f.evMu.Unlock()
	require.Len(t, f.events, 2)
	assert.True(t, f.events[0].Running)
	assert.False(t, f.events[1].Running)
	assert.Equal(t, OutcomeSkipped, f.events[1].Outcome)
	assert.Equal(t, "k", f.events[1].Key)
	assert.Equal(t, "m", f.events[1].Module)
}

// racingStore is a Store whose record gets claimed by another process between
// Due and Claim, the window two schedulers on one database race in.
type racingStore struct {
	*memStore
	other string
}

func (r *racingStore) Due(ctx context.Context, now time.Time, lease time.Duration) ([]Record, error) {
	due, err := r.memStore.Due(ctx, now, lease)
	for _, rec := range due {
		if _, err := r.Claim(ctx, rec.ID, r.other, now, lease); err != nil {
			return nil, err
		}
	}
	return due, err
}

func TestTickDoesNotRunARecordAnotherProcessClaimedFirst(t *testing.T) {
	f := newFixture(t, Options{})
	racing := &racingStore{memStore: f.store, other: "other-process"}
	f.Scheduler.store = racing
	var calls []string
	var mu sync.Mutex
	sched := f.For("m")
	sched.Handle(counting(&calls, &mu), HandlerOptions{})
	mustPut(t, sched, "k", "0 9 * * *", true)
	f.clock.advance(24 * time.Hour)

	f.tickAndWait(t)

	assert.Empty(t, calls, "the other process holds the claim")
	rec, _ := f.store.Get(t.Context(), "m", "k")
	assert.Equal(t, "other-process", rec.ClaimedBy)
}
