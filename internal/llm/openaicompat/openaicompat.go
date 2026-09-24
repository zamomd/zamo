// Package openaicompat implements knowledge.LLMClient on top of any
// OpenAI-compatible API. One implementation covers OpenAI, OpenRouter,
// DeepSeek and local models (Ollama and LM Studio expose the same API
// on localhost). Only the dialect lives here — the round trip and the error
// taxonomy are llm.Provider's.
package openaicompat

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

// Client talks to a chat-completions API at a configurable base URL: an
// llm.Provider wired to the chat-completions dialect, which Client itself
// implements. It sticks to the lowest common denominator of the OpenAI
// dialects: provider quirks stay inside this package.
type Client struct {
	*llm.Provider
	baseURL string // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	model   string
}

var (
	_ knowledge.LLMClient = (*Client)(nil)
	_ llm.Dialect         = (*Client)(nil)
)

// New creates a Client. The key source may yield an empty key for local
// providers; it is read on every request, never cached (see llm.KeySource). A
// trailing slash on baseURL is trimmed so a hand-edited ".../v1/" config does
// not become ".../v1//chat/completions" and 404.
func New(baseURL string, key llm.KeySource, model string) *Client {
	c := &Client{baseURL: strings.TrimRight(baseURL, "/"), model: model}
	c.Provider = llm.NewProvider(key, c)
	return c
}

// CompletionURL implements llm.Dialect.
func (c *Client) CompletionURL() string { return c.baseURL + "/chat/completions" }

// ModelsURL implements llm.Dialect. baseURL already carries the /v1 prefix, so
// the models endpoint hangs directly off it.
func (c *Client) ModelsURL() string { return c.baseURL + "/models" }

// KeyRequired implements llm.Dialect: a local model is reached without a key.
func (c *Client) KeyRequired() bool { return false }

// SetHeaders implements llm.Dialect.
func (c *Client) SetHeaders(h http.Header, key string) {
	if key != "" { // a local model is reached without a key
		h.Set("Authorization", "Bearer "+key)
	}
}

type chatRequest struct {
	Model     string     `json:"model"`
	MaxTokens int        `json:"max_tokens"`
	Messages  []chatTurn `json:"messages"`
}

// chatTurn is one message. Content is read as a plain string — the common
// dialect; a provider that returns an array of parts fails to decode and
// surfaces as unavailable.
type chatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatChoice struct {
	Message chatTurn `json:"message"`
}

// chatResponse is the subset of the 200 body we read. Choices is a pointer so a
// body missing the "choices" key (a proxy or CDN page, not the chat envelope)
// is distinguishable from an envelope whose choice list is empty.
type chatResponse struct {
	Choices *[]chatChoice `json:"choices"`
}

// EncodeRequest implements llm.Dialect.
func (c *Client) EncodeRequest(prompt string) ([]byte, error) {
	return json.Marshal(chatRequest{
		Model:     c.model,
		MaxTokens: llm.MaxTokens,
		Messages:  []chatTurn{{Role: "user", Content: prompt}},
	})
}

// DecodeReply implements llm.Dialect: the content of the reply's first choice.
func (c *Client) DecodeReply(body []byte) (string, error) {
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Choices == nil {
		// A 2xx body without the chat envelope means a proxy or CDN answered,
		// not the model — unavailable, not a bad response.
		return "", fmt.Errorf("%w: response is not a chat-completions envelope", knowledge.ErrLLMUnavailable)
	}
	// Further choices are alternative completions, never continuations, so
	// only the first is read.
	if choices := *parsed.Choices; len(choices) > 0 {
		return choices[0].Message.Content, nil
	}
	return "", nil
}
