// Package openaicompat implements llm.Client on top of any
// OpenAI-compatible API. One implementation covers OpenAI, OpenRouter,
// DeepSeek and local models (Ollama and LM Studio expose the same API
// on localhost). Only the dialect lives here — the round trip and the error
// taxonomy are llm.Provider's.
package openaicompat

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
	_ llm.Client  = (*Client)(nil)
	_ llm.Dialect = (*Client)(nil)
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
	Tools     []chatTool `json:"tools,omitempty"`
}

// chatTurn is one message. Content is read as a plain string — the common
// dialect; a provider that returns an array of parts fails to decode and
// surfaces as unavailable.
type chatTurn struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []chatCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

// chatFunction names the function of a call. Arguments is a JSON object
// carried as a string, which is how this API spells it in both directions.
type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string      `json:"type"`
	Function chatToolDef `json:"function"`
}

type chatToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  jsontext.Value `json:"parameters"`
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

const functionType = "function"

// EncodeChat implements llm.Dialect.
func (c *Client) EncodeChat(r llm.Request) ([]byte, error) {
	req := chatRequest{Model: c.model, MaxTokens: llm.MaxTokens}
	for _, t := range r.Tools {
		req.Tools = append(req.Tools, chatTool{
			Type:     functionType,
			Function: chatToolDef{Name: t.Name, Description: t.Description, Parameters: t.Schema},
		})
	}
	if r.System != "" {
		req.Messages = append(req.Messages, chatTurn{Role: "system", Content: r.System})
	}
	for _, m := range r.Messages {
		turn := chatTurn{Role: string(m.Role), Content: m.Text}
		if m.Role == llm.RoleTool {
			turn.ToolCallID = m.CallID
			// The API has no error flag on a tool message; the text carries it.
			if m.Failed {
				turn.Content = "error: " + m.Text
			}
		}
		for _, call := range m.Calls {
			turn.ToolCalls = append(turn.ToolCalls, chatCall{
				ID:       call.ID,
				Type:     functionType,
				Function: chatFunction{Name: call.Name, Arguments: cmp.Or(string(call.Args), "{}")},
			})
		}
		req.Messages = append(req.Messages, turn)
	}
	return json.Marshal(req)
}

// DecodeChat implements llm.Dialect: the content and tool calls of the reply's
// first choice.
func (c *Client) DecodeChat(body []byte) (llm.Reply, error) {
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llm.Reply{}, fmt.Errorf("%w: decode response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if parsed.Choices == nil {
		// A 2xx body without the chat envelope means a proxy or CDN answered,
		// not the model — unavailable, not a bad response.
		return llm.Reply{}, fmt.Errorf("%w: response is not a chat-completions envelope", knowledge.ErrLLMUnavailable)
	}
	// Further choices are alternative completions, never continuations, so
	// only the first is read.
	choices := *parsed.Choices
	if len(choices) == 0 {
		return llm.Reply{}, nil
	}
	msg := choices[0].Message
	reply := llm.Reply{Text: msg.Content}
	for _, call := range msg.ToolCalls {
		args := jsontext.Value(cmp.Or(call.Function.Arguments, "{}"))
		if !args.IsValid() {
			return llm.Reply{}, fmt.Errorf(
				"%w: arguments of %s are not JSON", knowledge.ErrLLMBadResponse, call.Function.Name)
		}
		reply.Calls = append(reply.Calls, llm.ToolCall{ID: call.ID, Name: call.Function.Name, Args: args})
	}
	return reply, nil
}
