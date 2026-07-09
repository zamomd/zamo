// Package openaicompat implements knowledge.LLMClient on top of any
// OpenAI-compatible API. One implementation covers OpenAI, OpenRouter,
// DeepSeek and local models (Ollama and LM Studio expose the same API
// on localhost).
package openaicompat

import (
	"context"
	"net/http"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// Client talks to a chat-completions API at a configurable base URL.
// It sticks to the lowest common denominator of the OpenAI dialects:
// provider quirks stay inside this package.
type Client struct {
	baseURL string // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	apiKey  string // may be empty (local models)
	model   string
	http    *http.Client
}

var _ knowledge.LLMClient = (*Client)(nil)

// New creates a Client. apiKey may be empty for local providers.
func New(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// GenerateTree implements knowledge.LLMClient.
func (c *Client) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}

// GenerateEdit implements knowledge.LLMClient.
func (c *Client) GenerateEdit(
	ctx context.Context,
	scope []knowledge.Block,
	instruction string,
) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}
