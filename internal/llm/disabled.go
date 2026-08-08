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

// GenerateDocument implements knowledge.LLMClient.
func (Disabled) GenerateDocument(ctx context.Context, topic, hint string) (string, error) {
	return "", fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// GenerateDocumentEdit implements knowledge.LLMClient.
func (Disabled) GenerateDocumentEdit(ctx context.Context, doc knowledge.Block, instruction string) (string, error) {
	return "", fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}

// Ping implements knowledge.LLMClient.
func (Disabled) Ping(ctx context.Context) error {
	return fmt.Errorf("%w: no LLM provider configured", knowledge.ErrLLMUnavailable)
}
