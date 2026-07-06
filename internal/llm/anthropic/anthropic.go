// Package anthropic implements knowledge.LLMClient on top of the
// Anthropic Messages API.
package anthropic

import (
	"context"
	"net/http"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// DefaultModel is used when no model is configured in the settings.
const DefaultModel = "claude-sonnet-5"

// Client talks to the Anthropic Messages API.
type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

var _ knowledge.LLMClient = (*Client)(nil)

// New creates a Client. An empty model selects DefaultModel.
func New(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 5 * time.Minute},
	}
}

// GenerateTree implements knowledge.LLMClient.
func (c *Client) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}

// GenerateEdit implements knowledge.LLMClient.
func (c *Client) GenerateEdit(ctx context.Context, scope []knowledge.Block, instruction string) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}
