package wailsbind

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/settings"
)

// unmarshalPayload round-trips MarshalError output, failing the test on
// anything that is not the documented JSON shape.
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
	// Every sentinel the frontend can react to must keep its wire code: the
	// stale/decided/busy reactions in the GUI switch on these strings.
	cases := []struct {
		err  error
		code string
	}{
		{knowledge.ErrNotFound, "not_found"},
		{knowledge.ErrStale, "stale"},
		{knowledge.ErrCycle, "cycle"},
		{knowledge.ErrNotPending, "not_pending"},
		{knowledge.ErrInvalidProposal, "invalid_proposal"},
		{knowledge.ErrEmptyProposal, "empty_proposal"},
		{knowledge.ErrBusy, "busy"},
		{knowledge.ErrLLMUnavailable, "llm_unavailable"},
		{knowledge.ErrLLMBadResponse, "llm_bad_response"},
		{settings.ErrDeclarative, "declarative"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			// Wrapped like the service does it: the code must survive the
			// chain, and the message must keep the wrapping context.
			wrapped := fmt.Errorf("apply proposal: %w", c.err)
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

func TestMarshalErrorUnknownIsInternal(t *testing.T) {
	p := unmarshalPayload(t, MarshalError(fmt.Errorf("disk on fire")))
	if p.Code != "internal" {
		t.Errorf("code = %q, want %q", p.Code, "internal")
	}
	if p.Message != "disk on fire" {
		t.Errorf("message = %q, want %q", p.Message, "disk on fire")
	}
}
