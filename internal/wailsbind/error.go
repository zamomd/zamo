package wailsbind

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/settings"
)

// ErrorPayload is the machine-readable envelope every binding error crosses
// the Wails boundary in. The frontend switches on Code; Message is for the
// human and stays free to change.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// codes maps sentinel errors to their stable wire codes. The list is ordered
// but order does not matter: the sentinels are distinct errors and at most one
// can sit in a chain. Anything unlisted crosses as "internal".
var codes = []struct {
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
	{knowledge.ErrConcurrentWrite, "concurrent_write"},
	{knowledge.ErrAIDisabled, "ai_disabled"},
	{knowledge.ErrLLMUnavailable, "llm_unavailable"},
	{knowledge.ErrLLMBadResponse, "llm_bad_response"},
	{knowledge.ErrLLMNoToolSupport, "llm_no_tools"},
	{settings.ErrDeclarative, "declarative"},
}

// MarshalError is plugged into application.Options.MarshalError: Wails calls
// it for every error a bound method returns and delivers the JSON to the
// frontend as the call error's cause. Mapping happens here and nowhere else —
// the frontend must never match on error text (see #58).
func MarshalError(err error) []byte {
	p := ErrorPayload{Code: "internal", Message: err.Error()}
	switch {
	// A cancelled generation reaches here wrapped in a provider failure; the
	// user asked for it, so it must not read as the provider being down.
	case errors.Is(err, context.Canceled):
		p.Code = "cancelled"
	default:
		for _, c := range codes {
			if errors.Is(err, c.err) {
				p.Code = c.code
				break
			}
		}
		if p.Code == "internal" {
			if coded, ok := errors.AsType[host.Coded](err); ok {
				p.Code = coded.Code()
			}
		}
	}
	b, jsonErr := json.Marshal(p)
	if jsonErr != nil {
		// The contract forbids failing; nil defers to Wails' default marshaling.
		return nil
	}
	return b
}
