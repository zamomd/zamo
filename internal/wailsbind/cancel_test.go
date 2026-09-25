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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
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
	require.Equal(t, 1, app.CancelGeneration(t.Context()))

	select {
	case err := <-result:
		require.Equal(t, "cancelled", unmarshalPayload(t, MarshalError(err)).Code, "error: %v", err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the expansion did not return after cancellation")
	}

	assert.Zero(t, app.CancelGeneration(t.Context()), "job still registered after it returned")
}
