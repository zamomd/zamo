// Package openaicompat implements knowledge.LLMClient on top of any
// OpenAI-compatible API. One implementation covers OpenAI, OpenRouter,
// DeepSeek and local models (Ollama and LM Studio expose the same API
// on localhost).
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

const (
	// maxTokens bounds the reply. A reviewable expansion is a few dozen short
	// blocks, which fits comfortably; the parser caps the tree regardless.
	maxTokens = 4096
	// maxReplyBytes bounds how much of the HTTP response we read into memory
	// before ParseOps applies its own (smaller) cap on the model text.
	maxReplyBytes = 2 << 20 // 2 MiB
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

// New creates a Client. apiKey may be empty for local providers. A trailing
// slash on baseURL is trimmed so a hand-edited ".../v1/" config does not become
// ".../v1//chat/completions" and 404.
func New(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http: &http.Client{
			Timeout: 60 * time.Second,
			// The API never redirects; following one would re-POST the prompt
			// to wherever Location points.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
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

// apiError is the shape of an OpenAI-style error body.
type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateTree implements knowledge.LLMClient. It renders the expansion prompt,
// calls the chat-completions API, and parses the reply into add operations.
// Transport and HTTP-status failures map to ErrLLMUnavailable; a reply that
// will not parse maps to ErrLLMBadResponse (via ParseOps). No API key is
// required — a local model runs without one.
func (c *Client) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	prompt, err := llm.ExpandPrompt(topic, hint)
	if err != nil {
		// The prompt template is embedded, so this only fails on a build defect;
		// map it to unavailable so the request-cannot-be-made contract holds.
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	text, err := c.complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return llm.ParseOps([]byte(text))
}

// complete performs one chat-completions call and returns the content of the
// reply's first choice.
func (c *Client) complete(ctx context.Context, prompt string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  []chatTurn{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("%w: encode request: %w", knowledge.ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" { // a local model is reached without a key
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if err != nil {
		return "", fmt.Errorf("%w: read response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", statusError(resp.StatusCode, body)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Choices == nil {
		// A 2xx body that isn't the chat envelope means something between us and
		// the model (proxy, CDN) answered — the model did not. Only the model's
		// own text failing to parse (ParseOps, downstream) is a bad response.
		return "", fmt.Errorf("%w: response is not a chat-completions envelope", knowledge.ErrLLMUnavailable)
	}
	// The answer is the first choice; further choices are alternative completions,
	// never continuations, so they are not concatenated. An empty choice list
	// yields empty text, which ParseOps reports as a bad response.
	if choices := *parsed.Choices; len(choices) > 0 {
		return choices[0].Message.Content, nil
	}
	return "", nil
}

// statusError turns a non-2xx response into ErrLLMUnavailable, folding in the
// API's error message when the body carries one.
func statusError(status int, body []byte) error {
	var e apiError
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("%w: HTTP %d: %s", knowledge.ErrLLMUnavailable, status, e.Error.Message)
	}
	return fmt.Errorf("%w: HTTP %d", knowledge.ErrLLMUnavailable, status)
}

// GenerateEdit implements knowledge.LLMClient.
func (c *Client) GenerateEdit(
	ctx context.Context,
	scope []knowledge.Block,
	instruction string,
) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}
