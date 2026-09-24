// Package anthropic implements knowledge.LLMClient on top of the
// Anthropic Messages API. Only the dialect lives here — the round trip and the
// error taxonomy are llm.Provider's.
package anthropic

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

// DefaultModel is used when no model is configured in the settings.
const DefaultModel = "claude-sonnet-5"

const (
	// defaultBaseURL is the Anthropic API root; tests point baseURL elsewhere.
	defaultBaseURL = "https://api.anthropic.com"
	// apiVersion is the required anthropic-version header value.
	apiVersion = "2023-06-01"
)

// Client talks to the Anthropic Messages API: an llm.Provider wired to the
// Messages dialect, which Client itself implements.
type Client struct {
	*llm.Provider
	model   string
	baseURL string
}

var (
	_ knowledge.LLMClient = (*Client)(nil)
	_ llm.Dialect         = (*Client)(nil)
)

// New creates a Client. An empty model selects DefaultModel. The key is read
// through the source on every request, never cached: see llm.KeySource.
func New(key llm.KeySource, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	c := &Client{model: model, baseURL: defaultBaseURL}
	c.Provider = llm.NewProvider(key, c)
	return c
}

// CompletionURL implements llm.Dialect.
func (c *Client) CompletionURL() string { return c.baseURL + "/v1/messages" }

// ModelsURL implements llm.Dialect.
func (c *Client) ModelsURL() string { return c.baseURL + "/v1/models" }

// KeyRequired implements llm.Dialect: the Messages API has no anonymous access.
func (c *Client) KeyRequired() bool { return true }

// SetHeaders implements llm.Dialect.
func (c *Client) SetHeaders(h http.Header, key string) {
	h.Set("X-Api-Key", key)
	h.Set("Anthropic-Version", apiVersion)
}

type messagesRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []messagesTurn `json:"messages"`
}

type messagesTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messagesResponse is the subset of the 200 body we read. Content is a pointer
// so a body missing the "content" key (a proxy or CDN page, not the Messages
// envelope) is distinguishable from an envelope whose content list is empty.
type messagesResponse struct {
	Content *[]contentBlock `json:"content"`
}

// EncodeRequest implements llm.Dialect.
func (c *Client) EncodeRequest(prompt string) ([]byte, error) {
	return json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: llm.MaxTokens,
		Messages:  []messagesTurn{{Role: "user", Content: prompt}},
	})
}

// DecodeReply implements llm.Dialect: the concatenated text of the reply's text
// blocks.
func (c *Client) DecodeReply(body []byte) (string, error) {
	var parsed messagesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Content == nil {
		// A 2xx body without the Messages envelope means a proxy or CDN
		// answered, not the model — unavailable, not a bad response.
		return "", fmt.Errorf("%w: response is not a Messages envelope", knowledge.ErrLLMUnavailable)
	}
	var text bytes.Buffer
	for _, block := range *parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String(), nil
}
