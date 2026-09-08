package wailsbind

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/jobs"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/sqlite"
	"github.com/cominddev/comind/internal/ulid"
)

// hangingLLM behaves like a provider that never answers: every generation
// waits for its context to end and reports the transport failure a real
// provider would wrap.
type hangingLLM struct{ started chan struct{} }

func (h hangingLLM) wait(ctx context.Context) error {
	h.started <- struct{}{}
	<-ctx.Done()
	return errors.Join(knowledge.ErrLLMUnavailable, ctx.Err())
}

func (h hangingLLM) GenerateTree(ctx context.Context, _, _ string) ([]knowledge.Op, error) {
	return nil, h.wait(ctx)
}

func (h hangingLLM) GenerateEdit(ctx context.Context, _ []knowledge.Block, _ string) ([]knowledge.Op, error) {
	return nil, h.wait(ctx)
}

func (h hangingLLM) GenerateDocument(ctx context.Context, _, _ string) (string, error) {
	return "", h.wait(ctx)
}

func (h hangingLLM) GenerateDocumentEdit(ctx context.Context, _ knowledge.Block, _ string) (string, error) {
	return "", h.wait(ctx)
}

func (h hangingLLM) Ping(ctx context.Context) error { return h.wait(ctx) }

func TestCancelGenerationInterruptsARunningExpansion(t *testing.T) {
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "comind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	llm := hangingLLM{started: make(chan struct{})}
	svc := knowledge.NewService(llm, sqlite.NewRepo(db, time.Now, newID), newID)
	app := New(svc, nil, jobs.New(t.Context(), ulid.New, time.Now))

	result := make(chan error, 1)
	go func() {
		_, err := app.ExpandTopic(t.Context(), "Go contexts", "")
		result <- err
	}()

	<-llm.started
	if n := app.CancelGeneration(t.Context()); n != 1 {
		t.Fatalf("CancelGeneration = %d, want 1", n)
	}

	select {
	case err := <-result:
		if p := unmarshalPayload(t, MarshalError(err)); p.Code != "cancelled" {
			t.Fatalf("error crossed as %q (%v), want cancelled", p.Code, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the expansion did not return after cancellation")
	}

	if n := app.CancelGeneration(t.Context()); n != 0 {
		t.Fatalf("job still registered after it returned: CancelGeneration = %d", n)
	}
}
