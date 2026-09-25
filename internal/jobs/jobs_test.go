package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRegistry issues the IDs a, b, c… and a clock that advances one second
// per job, so order and identity are predictable.
func newRegistry(root context.Context) *jobs.Registry {
	n := 0
	clock := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return jobs.New(root, func() string {
		n++
		return string(rune('a' + n - 1))
	}, func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	})
}

func TestStartListsTheJobUntilDone(t *testing.T) {
	r := newRegistry(t.Context())

	_, done := r.Start(t.Context(), jobs.KindGeneration)
	got := r.Running()
	require.Len(t, got, 1)
	require.Equal(t, "a", got[0].ID)
	require.Equal(t, jobs.KindGeneration, got[0].Kind)

	done()
	require.Empty(t, r.Running())
}

func TestCancelEndsTheJobContext(t *testing.T) {
	r := newRegistry(t.Context())
	ctx, done := r.Start(t.Context(), jobs.KindGeneration)
	defer done()

	require.True(t, r.Cancel("a"), "Cancel of a running job")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, r.Cancel("zzz"), "Cancel of an unknown ID")
}

func TestCancelKindOnlyTouchesThatKind(t *testing.T) {
	r := newRegistry(t.Context())
	gen, doneGen := r.Start(t.Context(), jobs.KindGeneration)
	defer doneGen()
	other, doneOther := r.Start(t.Context(), jobs.Kind("export"))
	defer doneOther()

	require.Equal(t, 1, r.CancelKind(jobs.KindGeneration))
	require.Error(t, gen.Err(), "generation context still alive after CancelKind")
	require.NoError(t, other.Err(), "export context ended")
}

func TestRootShutdownCancelsEveryJob(t *testing.T) {
	root, stop := context.WithCancel(t.Context())
	r := newRegistry(root)
	ctx, done := r.Start(t.Context(), jobs.KindGeneration)
	defer done()

	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "job context did not end with the root context")
	}
}

func TestParentCancellationEndsTheJob(t *testing.T) {
	r := newRegistry(t.Context())
	parent, cancel := context.WithCancel(t.Context())
	ctx, done := r.Start(parent, jobs.KindGeneration)
	defer done()

	cancel()

	require.Error(t, ctx.Err(), "job context outlived its parent")
}

func TestRunningIsOldestFirst(t *testing.T) {
	r := newRegistry(t.Context())
	_, done1 := r.Start(t.Context(), jobs.KindGeneration)
	defer done1()
	_, done2 := r.Start(t.Context(), jobs.KindGeneration)
	defer done2()

	got := r.Running()
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "b", got[1].ID)
}
