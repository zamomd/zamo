package llm

import (
	"context"
	"sync"
)

// Holder is a Client whose implementation can be replaced while the
// application runs. Its users take it once at wiring time and keep that
// reference; the settings screen swaps what is inside, so changing provider or
// key takes effect without a restart.
type Holder struct {
	mu     sync.RWMutex
	client Client
}

var _ Client = (*Holder)(nil)

// NewHolder wraps the client the application starts with.
func NewHolder(c Client) *Holder { return &Holder{client: c} }

// Set replaces the underlying client. Calls already in flight finish against
// the previous one.
func (h *Holder) Set(c Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = c
}

// current reads the client under the read lock; the call itself must not hold
// the lock, or a slow request would block every swap.
func (h *Holder) current() Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.client
}

// Chat implements Client.
func (h *Holder) Chat(ctx context.Context, r Request) (Reply, error) {
	return h.current().Chat(ctx, r)
}

// Ping implements Client.
func (h *Holder) Ping(ctx context.Context) error { return h.current().Ping(ctx) }
