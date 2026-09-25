package wailsbind

import (
	"testing"
	"time"

	"github.com/cominddev/comind/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelGenerationEndsTheRegisteredContext(t *testing.T) {
	reg := jobs.New(t.Context(), func() string { return "job" }, time.Now)
	app := New(nil, nil, reg)

	ctx, done := app.generation(t.Context())
	defer done()

	require.Equal(t, 1, app.CancelGeneration(t.Context()))
	assert.Error(t, ctx.Err(), "generation context still alive after CancelGeneration")
}

func TestCancelGenerationWithNothingInFlight(t *testing.T) {
	app := New(nil, nil, jobs.New(t.Context(), func() string { return "job" }, time.Now))

	assert.Zero(t, app.CancelGeneration(t.Context()))
}
