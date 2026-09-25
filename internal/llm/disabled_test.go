package llm

import (
	"context"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisabledReturnsErrLLMUnavailable(t *testing.T) {
	var c Disabled
	ctx := context.Background()

	_, err := c.GenerateTree(ctx, "topic", "hint")
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable, "GenerateTree")
	_, err = c.GenerateEdit(ctx, nil, "instruction")
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable, "GenerateEdit")
	assert.ErrorIs(t, c.Ping(ctx), knowledge.ErrLLMUnavailable, "Ping")
}

func TestDisabledRefusesDocumentGeneration(t *testing.T) {
	var d Disabled
	_, err := d.GenerateDocument(t.Context(), "Topic", "")
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable, "GenerateDocument")
	_, err = d.GenerateDocumentEdit(t.Context(), knowledge.Block{}, "do it")
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable, "GenerateDocumentEdit")
}
