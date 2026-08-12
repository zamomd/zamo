package wailsbind

import (
	"context"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
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

	if len(fe.events) != 1 {
		t.Fatalf("emitter saw %d events, want 1", len(fe.events))
	}
	e := fe.events[0]
	if e.name != GenerationProgressEvent {
		t.Errorf("event name = %q, want %q", e.name, GenerationProgressEvent)
	}
	if len(e.data) != 1 {
		t.Fatalf("event carried %d payloads, want 1", len(e.data))
	}
	p, ok := e.data[0].(progressPayload)
	if !ok {
		t.Fatalf("payload type = %T, want progressPayload", e.data[0])
	}
	if p.Phase != string(knowledge.PhaseAwaiting) {
		t.Errorf("payload phase = %q, want %q", p.Phase, knowledge.PhaseAwaiting)
	}
}

func TestWithProgressWithoutEmitterIsPassthrough(t *testing.T) {
	app := New(nil, nil, nil) // no SetEmitter
	ctx := context.Background()
	if got := app.withProgress(ctx); got != ctx {
		t.Error("withProgress changed the context with no emitter set")
	}
	knowledge.Report(app.withProgress(ctx), knowledge.Progress{Phase: knowledge.PhaseRequesting})
}
