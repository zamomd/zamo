package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func TestDisabledReturnsErrLLMUnavailable(t *testing.T) {
	var c Disabled
	ctx := context.Background()

	if _, err := c.GenerateTree(ctx, "topic", "hint"); !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("GenerateTree error = %v, want wrapping %v", err, knowledge.ErrLLMUnavailable)
	}
	if _, err := c.GenerateEdit(ctx, nil, "instruction"); !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("GenerateEdit error = %v, want wrapping %v", err, knowledge.ErrLLMUnavailable)
	}
	if err := c.Ping(ctx); !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("Ping error = %v, want wrapping %v", err, knowledge.ErrLLMUnavailable)
	}
}
