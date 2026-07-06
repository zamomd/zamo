package knowledge

import "context"

// LLMClient is the boundary with an LLM provider.
type LLMClient interface {
	// GenerateTree expands a topic into a block tree (add operations only).
	GenerateTree(ctx context.Context, topic, hint string) ([]Op, error)
	// GenerateEdit proposes a structure edit for the given scope
	// (a block or a subtree) based on a user's text instruction.
	GenerateEdit(ctx context.Context, scope []Block, instruction string) ([]Op, error)
}
