package wailsbind

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/settings"
)

func unmarshalPayload(t *testing.T, raw []byte) ErrorPayload {
	t.Helper()
	require.NotNil(t, raw, "MarshalError returned nil")
	var p ErrorPayload
	require.NoError(t, json.Unmarshal(raw, &p), "MarshalError returned invalid JSON %q", raw)
	return p
}

func TestMarshalErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{knowledge.ErrNotFound, "not_found"},
		{knowledge.ErrCycle, "cycle"},
		{knowledge.ErrInvalidOps, "invalid_ops"},
		{knowledge.ErrEmptyOps, "empty_ops"},
		{knowledge.ErrInvalidRollback, "invalid_rollback"},
		{knowledge.ErrGroupWrittenOver, "group_written_over"},
		{knowledge.ErrDuplicateTopic, "duplicate_topic"},
		{knowledge.ErrDocumentHasChildren, "document_has_children"},
		{knowledge.ErrUnreferenceableTopic, "unreferenceable_topic"},
		{knowledge.ErrAtomicGroup, "atomic_group"},
		{knowledge.ErrBusy, "busy"},
		{knowledge.ErrAIDisabled, "ai_disabled"},
		{knowledge.ErrLLMUnavailable, "llm_unavailable"},
		{knowledge.ErrLLMBadResponse, "llm_bad_response"},
		{knowledge.ErrLLMNoToolSupport, "llm_no_tools"},
		{settings.ErrDeclarative, "declarative"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			wrapped := fmt.Errorf("apply ops: %w", c.err)
			p := unmarshalPayload(t, MarshalError(wrapped))
			assert.Equal(t, c.code, p.Code)
			assert.Equal(t, wrapped.Error(), p.Message)
		})
	}
}

func TestMarshalErrorCancelledWinsOverProviderFailure(t *testing.T) {
	err := fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, context.Canceled)
	p := unmarshalPayload(t, MarshalError(err))
	assert.Equal(t, "cancelled", p.Code)
}

func TestMarshalErrorUnknownIsInternal(t *testing.T) {
	p := unmarshalPayload(t, MarshalError(fmt.Errorf("disk on fire")))
	assert.Equal(t, "internal", p.Code)
	assert.Equal(t, "disk on fire", p.Message)
}

type codedErr struct{ code string }

func (e codedErr) Error() string { return "module failure" }
func (e codedErr) Code() string  { return e.code }

func TestMarshalErrorReadsAModuleCode(t *testing.T) {
	err := fmt.Errorf("preview: %w", codedErr{"import_stale_preview"})
	assert.Equal(t, "import_stale_preview", unmarshalPayload(t, MarshalError(err)).Code)
}

func TestMarshalErrorDomainCodeWinsOverAModuleWrapper(t *testing.T) {
	err := fmt.Errorf("apply: %w", knowledge.ErrDuplicateTopic)
	assert.Equal(t, "duplicate_topic", unmarshalPayload(t, MarshalError(err)).Code)
}
