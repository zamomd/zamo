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
	baseURL string        // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	key     llm.KeySource // may yield an empty key (local models)
	model   string
	http    *http.Client
}

var _ knowledge.LLMClient = (*Client)(nil)

// New creates a Client. The key source may yield an empty key for local
// providers; it is read on every request, never cached (see llm.KeySource). A
// trailing slash on baseURL is trimmed so a hand-edited ".../v1/" config does
// not become ".../v1//chat/completions" and 404.
func New(baseURL string, key llm.KeySource, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
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
		// The template is embedded; failing to render it is a build defect,
		// still "the request cannot be made".
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	text, err := c.complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseParsing})
	return llm.ParseOps([]byte(text))
}

// complete performs one chat-completions call and returns the content of the
// reply's first choice.
func (c *Client) complete(ctx context.Context, prompt string) (string, error) {
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseRequesting})
	key, err := c.key()
	if err != nil {
		return "", fmt.Errorf("%w: read API key: %w", knowledge.ErrLLMUnavailable, err)
	}

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
	if key != "" { // a local model is reached without a key
		req.Header.Set("Authorization", "Bearer "+key)
	}

	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseAwaiting})
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

// Ping implements knowledge.LLMClient: it checks that the endpoint is
// reachable and the key, if any, is accepted. baseURL already carries the /v1
// prefix, so the models endpoint hangs directly off it. It deliberately does
// not exercise the configured model — a typo in the model name surfaces on the
// first expansion, not here.
func (c *Client) Ping(ctx context.Context) error {
	key, err := c.key()
	if err != nil {
		return fmt.Errorf("%w: read API key: %w", knowledge.ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
		return statusError(resp.StatusCode, body)
	}
	return nil
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

// GenerateEdit implements knowledge.LLMClient: it renders the edit prompt over
// the scope subtree and the instruction, calls the chat-completions API, and
// parses the reply into edit operations. Mirrors GenerateTree; the error
// taxonomy is the same (ErrLLMUnavailable / ErrLLMBadResponse via ParseEditOps).
func (c *Client) GenerateEdit(
	ctx context.Context,
	scope []knowledge.Block,
	instruction string,
) ([]knowledge.Op, error) {
	prompt, err := llm.EditPrompt(scope, instruction)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	text, err := c.complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseParsing})
	return llm.ParseEditOps([]byte(text))
}
