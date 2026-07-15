// Package anthropic implements knowledge.LLMClient on top of the
// Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
	// maxTokens bounds the reply. A reviewable expansion is a few dozen short
	// blocks, which fits comfortably; the parser caps the tree regardless.
	maxTokens = 4096
	// maxReplyBytes bounds how much of the HTTP response we read into memory
	// before ParseOps applies its own (smaller) cap on the model text.
	maxReplyBytes = 2 << 20 // 2 MiB
)

// Client talks to the Anthropic Messages API.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

var _ knowledge.LLMClient = (*Client)(nil)

// New creates a Client. An empty model selects DefaultModel.
func New(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
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

// apiError is the shape of a non-2xx Messages API error body.
type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateTree implements knowledge.LLMClient. It renders the expansion prompt,
// calls the Messages API, and parses the reply into add operations. Transport,
// key and HTTP-status failures map to ErrLLMUnavailable; a reply that will not
// parse maps to ErrLLMBadResponse (via ParseOps).
func (c *Client) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("%w: no API key configured", knowledge.ErrLLMUnavailable)
	}
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

// complete performs one Messages API call and returns the concatenated text of
// the reply's text blocks.
func (c *Client) complete(ctx context.Context, prompt string) (string, error) {
	payload, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  []messagesTurn{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("%w: encode request: %w", knowledge.ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", apiVersion)

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

	var parsed messagesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Content == nil {
		// A 2xx body that isn't the Messages envelope means something between us
		// and the model (proxy, CDN) answered — the model did not. Only the
		// model's own text failing to parse (ParseOps, downstream) is a bad response.
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
