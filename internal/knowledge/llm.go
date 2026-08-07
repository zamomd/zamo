package knowledge

import "context"

// LLMClient is the boundary with an LLM provider.
type LLMClient interface {
	// GenerateTree expands a topic into a block tree (add operations only).
	GenerateTree(ctx context.Context, topic, hint string) ([]Op, error)
	// GenerateEdit produces a structure edit for the given scope
	// (a block or a subtree) based on a user's text instruction.
	GenerateEdit(ctx context.Context, scope []Block, instruction string) ([]Op, error)
	// GenerateDocument writes a whole document for a topic that is (or is
	// becoming) a document root, from its name and an optional hint.
	GenerateDocument(ctx context.Context, topic, hint string) (string, error)
	// GenerateDocumentEdit rewrites a document whole, applying a user's text
	// instruction to it.
	GenerateDocumentEdit(ctx context.Context, doc Block, instruction string) (string, error)
	// Ping reports whether the provider is reachable and the credentials are
	// accepted. The settings screen calls it; no use case depends on it.
	Ping(ctx context.Context) error
}
