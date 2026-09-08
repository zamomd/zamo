package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/jobs"
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
	if got := r.Running(); len(got) != 1 || got[0].ID != "a" || got[0].Kind != jobs.KindGeneration {
		t.Fatalf("Running() = %+v, want one generation job with ID a", got)
	}

	done()
	if got := r.Running(); len(got) != 0 {
		t.Fatalf("Running() after done = %+v, want none", got)
	}
}

func TestCancelEndsTheJobContext(t *testing.T) {
	r := newRegistry(t.Context())
	ctx, done := r.Start(t.Context(), jobs.KindGeneration)
	defer done()

	if !r.Cancel("a") {
		t.Fatal("Cancel(a) = false, want true for a running job")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("ctx.Err() = %v, want Canceled", ctx.Err())
	}
	if r.Cancel("zzz") {
		t.Fatal("Cancel of an unknown ID = true, want false")
	}
}

func TestCancelKindOnlyTouchesThatKind(t *testing.T) {
	r := newRegistry(t.Context())
	gen, doneGen := r.Start(t.Context(), jobs.KindGeneration)
	defer doneGen()
	other, doneOther := r.Start(t.Context(), jobs.Kind("export"))
	defer doneOther()

	if n := r.CancelKind(jobs.KindGeneration); n != 1 {
		t.Fatalf("CancelKind = %d, want 1", n)
	}
	if gen.Err() == nil {
		t.Fatal("generation context still alive after CancelKind")
	}
	if other.Err() != nil {
		t.Fatalf("export context ended: %v", other.Err())
	}
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
		t.Fatal("job context did not end with the root context")
	}
}

func TestParentCancellationEndsTheJob(t *testing.T) {
	r := newRegistry(t.Context())
	parent, cancel := context.WithCancel(t.Context())
	ctx, done := r.Start(parent, jobs.KindGeneration)
	defer done()

	cancel()

	if ctx.Err() == nil {
		t.Fatal("job context outlived its parent")
	}
}

func TestRunningIsOldestFirst(t *testing.T) {
	r := newRegistry(t.Context())
	_, done1 := r.Start(t.Context(), jobs.KindGeneration)
	defer done1()
	_, done2 := r.Start(t.Context(), jobs.KindGeneration)
	defer done2()

	got := r.Running()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("Running() = %+v, want a then b", got)
	}
}
