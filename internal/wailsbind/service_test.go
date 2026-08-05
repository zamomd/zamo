package wailsbind

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// TestServiceOptionsMarshalErrorsWithCode pins the wiring the frontend depends
// on: the marshaler travels with the service, so a domain error reaches the
// caller carrying its wire code. Registering the App without it leaves the
// binding engine's default marshaler in place, which renders a wrapped error as
// an empty object and costs every mapped message its code.
func TestServiceOptionsMarshalErrorsWithCode(t *testing.T) {
	marshal := ServiceOptions().MarshalError
	if marshal == nil {
		t.Fatal("MarshalError is nil, so the binding engine falls back to its default marshaler")
	}

	err := fmt.Errorf("%w: dial tcp: connection refused", knowledge.ErrLLMUnavailable)

	var payload ErrorPayload
	raw := marshal(err)
	if unmarshalErr := json.Unmarshal(raw, &payload); unmarshalErr != nil {
		t.Fatalf("marshalled %s, which is not an ErrorPayload: %v", raw, unmarshalErr)
	}
	if payload.Code != "llm_unavailable" {
		t.Errorf("code = %q, want %q", payload.Code, "llm_unavailable")
	}
}
