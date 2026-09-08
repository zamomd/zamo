package wailsbind

import (
	"testing"
	"time"

	"github.com/cominddev/comind/internal/jobs"
)

func TestCancelGenerationEndsTheRegisteredContext(t *testing.T) {
	reg := jobs.New(t.Context(), func() string { return "job" }, time.Now)
	app := New(nil, nil, reg, nil)

	ctx, done := app.generation(t.Context())
	defer done()

	if n := app.CancelGeneration(t.Context()); n != 1 {
		t.Fatalf("CancelGeneration = %d, want 1", n)
	}
	if ctx.Err() == nil {
		t.Fatal("generation context still alive after CancelGeneration")
	}
}

func TestCancelGenerationWithNothingInFlight(t *testing.T) {
	app := New(nil, nil, jobs.New(t.Context(), func() string { return "job" }, time.Now), nil)

	if n := app.CancelGeneration(t.Context()); n != 0 {
		t.Fatalf("CancelGeneration = %d, want 0", n)
	}
}
