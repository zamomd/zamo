package wailsbind

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

type emitted struct {
	name string
	data []any
}

type fakeEmitter struct {
	events []emitted
}

func (f *fakeEmitter) Emit(name string, data ...any) bool {
	f.events = append(f.events, emitted{name: name, data: data})
	return true
}

func TestWithProgressForwardsReportsAsEvents(t *testing.T) {
	fe := &fakeEmitter{}
	app := New(nil, nil, nil)
	SetEmitter(app, fe)

	ctx := app.withProgress(context.Background())
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseAwaiting})

	require.Len(t, fe.events, 1)
	e := fe.events[0]
	assert.Equal(t, GenerationProgressEvent, e.name)
	require.Len(t, e.data, 1)
	p, ok := e.data[0].(progressPayload)
	require.True(t, ok, "payload type = %T, want progressPayload", e.data[0])
	assert.Equal(t, string(knowledge.PhaseAwaiting), p.Phase)
}

func TestWithProgressWithoutEmitterIsPassthrough(t *testing.T) {
	app := New(nil, nil, nil) // no SetEmitter
	ctx := context.Background()
	assert.Equal(t, ctx, app.withProgress(ctx), "withProgress changed the context with no emitter set")
	knowledge.Report(app.withProgress(ctx), knowledge.Progress{Phase: knowledge.PhaseRequesting})
}
