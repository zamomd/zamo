// Package anthropic implements llm.Client on top of the
// Anthropic Messages API. Only the dialect lives here — the round trip and the
// error taxonomy are llm.Provider's.
package anthropic

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
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
	_ llm.Client  = (*Client)(nil)
	_ llm.Dialect = (*Client)(nil)
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
	System    string         `json:"system,omitempty"`
	Messages  []messagesTurn `json:"messages"`
	Tools     []toolDef      `json:"tools,omitempty"`
}

// messagesTurn is one message. Content is a plain string for a turn of text
// alone and a list of blocks for anything carrying a tool call or result.
type messagesTurn struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// contentBlock is a text, tool_use or tool_result block; Type says which of
// the remaining fields are meaningful.
type contentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     jsontext.Value `json:"input,omitzero"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitzero"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema jsontext.Value `json:"input_schema"`
}

// messagesResponse is the subset of the 200 body we read. Content is a pointer
// so a body missing the "content" key (a proxy or CDN page, not the Messages
// envelope) is distinguishable from an envelope whose content list is empty.
type messagesResponse struct {
	Content *[]contentBlock `json:"content"`
}

var emptyObject = jsontext.Value("{}")

// EncodeChat implements llm.Dialect.
func (c *Client) EncodeChat(r llm.Request) ([]byte, error) {
	req := messagesRequest{Model: c.model, MaxTokens: llm.MaxTokens, System: r.System}
	for _, t := range r.Tools {
		req.Tools = append(req.Tools, toolDef{Name: t.Name, Description: t.Description, InputSchema: t.Schema})
	}
	// The API wants every result of one assistant turn in a single user
	// message, so consecutive tool turns fold into one.
	var results []contentBlock
	flush := func() {
		if len(results) > 0 {
			req.Messages = append(req.Messages, messagesTurn{Role: "user", Content: results})
			results = nil
		}
	}
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool {
			results = append(results, contentBlock{
				Type: "tool_result", ToolUseID: m.CallID, Content: m.Text, IsError: m.Failed,
			})
			continue
		}
		flush()
		if len(m.Calls) == 0 {
			req.Messages = append(req.Messages, messagesTurn{Role: string(m.Role), Content: m.Text})
			continue
		}
		var blocks []contentBlock
		if m.Text != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: m.Text})
		}
		for _, call := range m.Calls {
			blocks = append(blocks, contentBlock{
				Type: "tool_use", ID: call.ID, Name: call.Name, Input: jsontext.Value(cmp.Or(string(call.Args), "{}")),
			})
		}
		req.Messages = append(req.Messages, messagesTurn{Role: string(m.Role), Content: blocks})
	}
	flush()
	return json.Marshal(req)
}

// DecodeChat implements llm.Dialect: the concatenated text of the reply's text
// blocks, and its tool_use blocks as calls.
func (c *Client) DecodeChat(body []byte) (llm.Reply, error) {
	var parsed messagesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llm.Reply{}, fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Content == nil {
		// A 2xx body without the Messages envelope means a proxy or CDN
		// answered, not the model — unavailable, not a bad response.
		return llm.Reply{}, fmt.Errorf("%w: response is not a Messages envelope", knowledge.ErrLLMUnavailable)
	}
	var reply llm.Reply
	var text strings.Builder
	for _, block := range *parsed.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			args := block.Input
			if len(args) == 0 {
				args = emptyObject
			}
			reply.Calls = append(reply.Calls, llm.ToolCall{ID: block.ID, Name: block.Name, Args: args})
		}
	}
	reply.Text = text.String()
	return reply, nil
}
