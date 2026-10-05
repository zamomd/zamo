package llm

import (
	"context"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// Disabled is a null-object Client for the "no provider configured" state —
// legal and supported, see config.LLM.Provider. Wiring it in instead of
// leaving the client nil turns "no provider" into a clear ErrLLMUnavailable at
// the point of use rather than a nil-pointer panic.
type Disabled struct{}

var _ Client = Disabled{}

// Chat implements Client.
func (Disabled) Chat(context.Context, Request) (Reply, error) {
	return Reply{}, fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// Ping implements Client.
func (Disabled) Ping(context.Context) error {
	return fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}
