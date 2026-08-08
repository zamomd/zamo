package llm

import (
	"context"
	"sync"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
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
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].Content != "first" {
		t.Fatalf("content = %q, want first", ops[0].Content)
	}

	h.Set(stubClient{name: "second"})

	ops, err = h.GenerateTree(t.Context(), "topic", "")
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].Content != "second" {
		t.Errorf("content = %q, want second (Set must swap the client)", ops[0].Content)
	}
}

func TestHolderRace(t *testing.T) {
	h := NewHolder(stubClient{name: "first"})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { h.Set(stubClient{name: "swapped"}) })
		wg.Go(func() {
			if _, err := h.GenerateTree(t.Context(), "topic", ""); err != nil {
				t.Errorf("call %d: %v", i, err)
			}
		})
	}
	wg.Wait()
}
