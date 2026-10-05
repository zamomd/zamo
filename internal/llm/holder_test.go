package llm

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubClient struct{ name string }

func (s stubClient) Chat(context.Context, Request) (Reply, error) {
	return Reply{Text: s.name}, nil
}

func (s stubClient) Ping(context.Context) error { return nil }

func TestHolderSetSwapsClient(t *testing.T) {
	h := NewHolder(stubClient{name: "first"})

	reply, err := h.Chat(t.Context(), Request{})
	require.NoError(t, err)
	require.Equal(t, "first", reply.Text)

	h.Set(stubClient{name: "second"})

	reply, err = h.Chat(t.Context(), Request{})
	require.NoError(t, err)
	assert.Equal(t, "second", reply.Text, "Set must swap the client")
}

func TestHolderRace(t *testing.T) {
	h := NewHolder(stubClient{name: "first"})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { h.Set(stubClient{name: "swapped"}) })
		wg.Go(func() {
			_, err := h.Chat(t.Context(), Request{})
			assert.NoError(t, err, "call %d", i)
		})
	}
	wg.Wait()
}
