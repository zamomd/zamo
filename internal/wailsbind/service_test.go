package wailsbind

import (
	"encoding/json/v2"
	"fmt"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceOptionsMarshalErrorsWithCode(t *testing.T) {
	marshal := ServiceOptions().MarshalError
	require.NotNil(t, marshal, "MarshalError is nil, so the binding engine falls back to its default marshaler")

	err := fmt.Errorf("%w: dial tcp: connection refused", knowledge.ErrLLMUnavailable)

	var payload ErrorPayload
	raw := marshal(err)
	require.NoError(t, json.Unmarshal(raw, &payload), "marshalled %s, which is not an ErrorPayload", raw)
	assert.Equal(t, "llm_unavailable", payload.Code)
}
