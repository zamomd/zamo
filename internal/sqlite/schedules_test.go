package sqlite

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/scheduler"
)

const lease = 15 * time.Minute

var t0 = time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

func testSchedules(t *testing.T) *Schedules {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewSchedules(db, func() time.Time { return t0 })
}

func due(module, key string, next time.Time) scheduler.Record {
	return scheduler.Record{
		ID: module + "/" + key, Module: module, Key: key,
		Spec: "0 9 * * *", Enabled: true, Payload: []byte(`{"v":1}`), NextRun: next,
	}
}

func mustClaim(t *testing.T, s *Schedules, id, instance string, at time.Time) bool {
	t.Helper()
	ok, err := s.Claim(t.Context(), id, instance, at, lease)
	require.NoError(t, err, "Claim %s by %s", id, instance)
	return ok
}

func mustPut(t *testing.T, s *Schedules, rec scheduler.Record) scheduler.Record {
	t.Helper()
	got, err := s.Put(t.Context(), rec)
	require.NoError(t, err, "Put %s/%s", rec.Module, rec.Key)
	return got
}

func TestSchedulesPutThenGetRoundTrips(t *testing.T) {
	s := testSchedules(t)
	mustPut(t, s, due("smartcron", "blk-1", t0.Add(time.Hour)))

	got, err := s.Get(t.Context(), "smartcron", "blk-1")
	require.NoError(t, err)
	assert.Equal(t, "0 9 * * *", got.Spec)
	assert.True(t, got.Enabled)
	assert.JSONEq(t, `{"v":1}`, string(got.Payload))
	assert.WithinDuration(t, t0.Add(time.Hour), got.NextRun, 0, "NextRun")
	assert.True(t, got.LastRun.IsZero(), "LastRun = %v, want none", got.LastRun)
}

func TestSchedulesGetMissingIsNotFound(t *testing.T) {
	s := testSchedules(t)

	_, err := s.Get(t.Context(), "smartcron", "nope")

	assert.ErrorIs(t, err, scheduler.ErrNotFound)
}

func TestSchedulesPutAgainKeepsIdentityAndRunHistory(t *testing.T) {
	s := testSchedules(t)
	first := mustPut(t, s, due("smartcron", "blk-1", t0))
	ok, err := s.Claim(t.Context(), first.ID, "me", t0, lease)
	require.NoError(t, err)
	require.True(t, ok, "Claim")
	_, err = s.Finish(t.Context(), first.ID, "me", scheduler.Completion{
		At: t0, Outcome: "ok", Detail: "", NextRun: t0.Add(24 * time.Hour),
	})
	require.NoError(t, err)

	edited := due("smartcron", "blk-1", t0.Add(2*time.Hour))
	edited.ID = "a-different-id"
	edited.Spec = "0 11 * * *"
	got := mustPut(t, s, edited)

	assert.Equal(t, first.ID, got.ID, "an edit is not a new schedule")
	assert.Equal(t, "0 11 * * *", got.Spec)
	assert.WithinDuration(t, t0.Add(2*time.Hour), got.NextRun, 0, "NextRun")
	assert.Equal(t, "ok", got.Outcome, "the last run is kept across an edit")
	assert.WithinDuration(t, t0, got.LastRun, 0, "the last run is kept across an edit")
}

func TestSchedulesAreScopedByModule(t *testing.T) {
	s := testSchedules(t)
	mustPut(t, s, due("smartcron", "k", t0))
	mustPut(t, s, due("export", "k", t0))

	require.NoError(t, s.Delete(t.Context(), "export", "k"))

	mine, err := s.List(t.Context(), "smartcron")
	require.NoError(t, err)
	require.Len(t, mine, 1, "the record must be untouched by the other module's delete")
	all, err := s.List(t.Context(), "")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestSchedulesDueSkipsDisabledFutureAndClaimed(t *testing.T) {
	s := testSchedules(t)
	mustPut(t, s, due("m", "late", t0.Add(-time.Hour)))
	mustPut(t, s, due("m", "later", t0.Add(-time.Minute)))
	mustPut(t, s, due("m", "future", t0.Add(time.Minute)))
	off := due("m", "off", time.Time{})
	off.Enabled = false
	mustPut(t, s, off)
	held := mustPut(t, s, due("m", "held", t0.Add(-time.Hour)))
	ok, err := s.Claim(t.Context(), held.ID, "other", t0, lease)
	require.NoError(t, err)
	require.True(t, ok, "Claim")

	got, err := s.Due(t.Context(), t0, lease)
	require.NoError(t, err)
	var keys []string
	for _, r := range got {
		keys = append(keys, r.Key)
	}
	assert.Equal(t, []string{"late", "later"}, keys, "earliest first, nothing disabled, future or claimed")
}

func TestSchedulesClaimIsExclusiveUntilTheLeaseExpires(t *testing.T) {
	s := testSchedules(t)
	rec := mustPut(t, s, due("m", "k", t0))

	first := mustClaim(t, s, rec.ID, "a", t0)
	second := mustClaim(t, s, rec.ID, "b", t0.Add(time.Minute))
	afterLease := mustClaim(t, s, rec.ID, "b", t0.Add(lease+time.Second))

	assert.True(t, first, "first claim")
	assert.False(t, second, "claim within the lease")
	assert.True(t, afterLease, "claim after the lease")
}

func TestSchedulesFinishFromAStaleClaimWritesNothing(t *testing.T) {
	s := testSchedules(t)
	rec := mustPut(t, s, due("m", "k", t0))
	mustClaim(t, s, rec.ID, "stale", t0)
	mustClaim(t, s, rec.ID, "fresh", t0.Add(lease+time.Second))

	wrote, err := s.Finish(t.Context(), rec.ID, "stale", scheduler.Completion{
		At: t0.Add(time.Hour), Outcome: "ok", NextRun: t0.Add(48 * time.Hour),
	})

	require.NoError(t, err)
	require.False(t, wrote, "Finish(stale) must write nothing")
	got, _ := s.Get(t.Context(), "m", "k")
	assert.Equal(t, "fresh", got.ClaimedBy)
	assert.Empty(t, got.Outcome)
}

func TestSchedulesReleaseKeepsTheRunTimes(t *testing.T) {
	s := testSchedules(t)
	rec := mustPut(t, s, due("m", "k", t0))
	mustClaim(t, s, rec.ID, "me", t0)

	require.NoError(t, s.Release(t.Context(), rec.ID, "me"))

	got, _ := s.Get(t.Context(), "m", "k")
	assert.Empty(t, got.ClaimedBy)
	assert.WithinDuration(t, t0, got.NextRun, 0, "NextRun")
	assert.True(t, got.LastRun.IsZero(), "LastRun = %v, want none", got.LastRun)
}

func TestSchedulesRescheduleMovesOnlyAnEnabledRecord(t *testing.T) {
	s := testSchedules(t)
	on := mustPut(t, s, due("m", "on", t0))
	paused := due("m", "off", time.Time{})
	paused.Enabled = false
	off := mustPut(t, s, paused)
	next := t0.Add(24 * time.Hour)

	require.NoError(t, s.Reschedule(t.Context(), on.ID, next))
	require.NoError(t, s.Reschedule(t.Context(), off.ID, next))

	got, _ := s.Get(t.Context(), "m", "on")
	assert.WithinDuration(t, next, got.NextRun, 0, "NextRun")
	assert.True(t, got.LastRun.IsZero(), "LastRun = %v, want none", got.LastRun)
	got, _ = s.Get(t.Context(), "m", "off")
	assert.True(t, got.NextRun.IsZero(), "disabled NextRun = %v, want none", got.NextRun)
}

func TestSchedulesConcurrentClaimHasOneWinner(t *testing.T) {
	s := testSchedules(t)
	rec := mustPut(t, s, due("m", "k", t0))

	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for i := range 8 {
		wg.Go(func() {
			ok, err := s.Claim(t.Context(), rec.ID, string(rune('a'+i)), t0, lease)
			assert.NoError(t, err)
			wins <- ok
		})
	}
	wg.Wait()
	close(wins)

	won := 0
	for ok := range wins {
		if ok {
			won++
		}
	}
	assert.Equal(t, 1, won, "claims that succeeded")
}
