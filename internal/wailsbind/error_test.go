package wailsbind

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/settings"
)

func unmarshalPayload(t *testing.T, raw []byte) ErrorPayload {
	t.Helper()
	if raw == nil {
		t.Fatal("MarshalError returned nil")
	}
	var p ErrorPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("MarshalError returned invalid JSON %q: %v", raw, err)
	}
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
		{settings.ErrDeclarative, "declarative"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			wrapped := fmt.Errorf("apply ops: %w", c.err)
			p := unmarshalPayload(t, MarshalError(wrapped))
			if p.Code != c.code {
				t.Errorf("code = %q, want %q", p.Code, c.code)
			}
			if p.Message != wrapped.Error() {
				t.Errorf("message = %q, want %q", p.Message, wrapped.Error())
			}
		})
	}
}

func TestMarshalErrorCancelledWinsOverProviderFailure(t *testing.T) {
	err := fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, context.Canceled)
	p := unmarshalPayload(t, MarshalError(err))
	if p.Code != "cancelled" {
		t.Errorf("code = %q, want cancelled", p.Code)
	}
}

func TestMarshalErrorUnknownIsInternal(t *testing.T) {
	p := unmarshalPayload(t, MarshalError(fmt.Errorf("disk on fire")))
	if p.Code != "internal" {
		t.Errorf("code = %q, want %q", p.Code, "internal")
	}
	if p.Message != "disk on fire" {
		t.Errorf("message = %q, want %q", p.Message, "disk on fire")
	}
}

type codedErr struct{ code string }

func (e codedErr) Error() string { return "module failure" }
func (e codedErr) Code() string  { return e.code }

func TestMarshalErrorReadsAModuleCode(t *testing.T) {
	err := fmt.Errorf("preview: %w", codedErr{"import_stale_preview"})
	if p := unmarshalPayload(t, MarshalError(err)); p.Code != "import_stale_preview" {
		t.Errorf("code = %q, want the module's own code", p.Code)
	}
}

func TestMarshalErrorDomainCodeWinsOverAModuleWrapper(t *testing.T) {
	err := fmt.Errorf("apply: %w", knowledge.ErrDuplicateTopic)
	if p := unmarshalPayload(t, MarshalError(err)); p.Code != "duplicate_topic" {
		t.Errorf("code = %q, want duplicate_topic", p.Code)
	}
}
