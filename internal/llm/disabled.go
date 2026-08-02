package llm

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// Disabled is a null-object knowledge.LLMClient for the "no provider
// configured" state — legal and supported, see config.LLM.Provider. Wiring it
// in instead of leaving the client nil turns "no provider" into a clear
// ErrLLMUnavailable at the point of use rather than a nil-pointer panic.
type Disabled struct{}

var _ knowledge.LLMClient = Disabled{}

// GenerateTree implements knowledge.LLMClient.
func (Disabled) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	return nil, fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// GenerateEdit implements knowledge.LLMClient.
func (Disabled) GenerateEdit(ctx context.Context, scope []knowledge.Block, instruction string) ([]knowledge.Op, error) {
	return nil, fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// Ping implements knowledge.LLMClient.
func (Disabled) Ping(ctx context.Context) error {
	return fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// Off is a null-object knowledge.LLMClient for the owner's application-wide
// "no AI" policy. It is a separate state from Disabled: the provider stays
// configured and reachable, so the answer names the switch rather than sending
// the owner off to fix credentials that are not broken.
type Off struct{}

var _ knowledge.LLMClient = Off{}

// GenerateTree implements knowledge.LLMClient.
func (Off) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	return nil, offError()
}

// GenerateEdit implements knowledge.LLMClient.
func (Off) GenerateEdit(ctx context.Context, scope []knowledge.Block, instruction string) ([]knowledge.Op, error) {
	return nil, offError()
}

// Ping implements knowledge.LLMClient.
func (Off) Ping(ctx context.Context) error { return offError() }

func offError() error {
	return fmt.Errorf("%w: turn AI back on in settings to use this", knowledge.ErrAIDisabled)
}
