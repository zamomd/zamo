package llm

import (
	"context"
	"sync"

	"github.com/cominddev/comind/internal/knowledge"
)

// Holder is an LLMClient whose implementation can be replaced while the
// application runs. The service takes it once at wiring time and keeps that
// reference; the settings screen swaps what is inside, so changing provider or
// key takes effect without a restart.
type Holder struct {
	mu     sync.RWMutex
	client knowledge.LLMClient
}

var _ knowledge.LLMClient = (*Holder)(nil)

// NewHolder wraps the client the application starts with.
func NewHolder(c knowledge.LLMClient) *Holder { return &Holder{client: c} }

// Set replaces the underlying client. Calls already in flight finish against
// the previous one.
func (h *Holder) Set(c knowledge.LLMClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = c
}

// current reads the client under the read lock; the call itself must not hold
// the lock, or a slow request would block every swap.
func (h *Holder) current() knowledge.LLMClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.client
}

// GenerateTree implements knowledge.LLMClient.
func (h *Holder) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	return h.current().GenerateTree(ctx, topic, hint)
}

// GenerateEdit implements knowledge.LLMClient.
func (h *Holder) GenerateEdit(
	ctx context.Context,
	scope []knowledge.Block,
	instruction string,
) ([]knowledge.Op, error) {
	return h.current().GenerateEdit(ctx, scope, instruction)
}

// Ping implements knowledge.LLMClient.
func (h *Holder) Ping(ctx context.Context) error { return h.current().Ping(ctx) }
