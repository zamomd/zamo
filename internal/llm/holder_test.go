package llm

import (
	"context"
	"sync"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubClient struct{ name string }

func (s stubClient) GenerateTree(context.Context, string, string) ([]knowledge.Op, error) {
	return []knowledge.Op{{Kind: knowledge.OpAdd, Content: s.name}}, nil
}

func (s stubClient) GenerateEdit(context.Context, []knowledge.Block, string) ([]knowledge.Op, error) {
	return nil, nil
}

func (s stubClient) GenerateDocument(context.Context, string, string) (string, error) {
	return s.name, nil
}

func (s stubClient) GenerateDocumentEdit(context.Context, knowledge.Block, string) (string, error) {
	return s.name, nil
}

func (s stubClient) Ping(context.Context) error { return nil }

func TestHolderSetSwapsClient(t *testing.T) {
	h := NewHolder(stubClient{name: "first"})

	ops, err := h.GenerateTree(t.Context(), "topic", "")
	require.NoError(t, err)
	require.Equal(t, "first", ops[0].Content)

	h.Set(stubClient{name: "second"})

	ops, err = h.GenerateTree(t.Context(), "topic", "")
	require.NoError(t, err)
	assert.Equal(t, "second", ops[0].Content, "Set must swap the client")
}

func TestHolderRace(t *testing.T) {
	h := NewHolder(stubClient{name: "first"})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { h.Set(stubClient{name: "swapped"}) })
		wg.Go(func() {
			_, err := h.GenerateTree(t.Context(), "topic", "")
			assert.NoError(t, err, "call %d", i)
		})
	}
	wg.Wait()
}
