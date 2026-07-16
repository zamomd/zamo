package llm

import (
	"context"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// Disabled is a null-object knowledge.LLMClient for the "no provider
// configured" state. A provider is optional (see config.LLM.Provider):
// comind mcp never touches this interface, since there the AI is the MCP
// client, not us; only the GUI's ExpandTopic and ProposeEdit use cases need
// a real client. Wiring this in for the empty-provider case, instead of
// leaving the client nil, turns "no provider" into a clear domain error
// (ErrLLMUnavailable) at the point of use rather than a nil-pointer panic.
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
