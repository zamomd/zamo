package openaicompat

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// tree is a canned expansion the stub server returns as the model's message
// content: a topic root with one child, in the JSON shape ParseOps expects.
const tree = `{"content":"# Go contexts","children":[` +
	`{"content":"context.Context carries deadlines","children":[]}]}`

// chatReply wraps model text in an OpenAI chat-completions envelope.
func chatReply(content string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + jsonString(content) + `}}]}`
}

func newTestClient(t *testing.T, apiKey string, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, apiKey, "llama3")
}

func TestGenerateTreeParsesReply(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("request = %s %s, want POST /chat/completions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	})

	ops, err := c.GenerateTree(t.Context(), "Go contexts", "cancellation")
	if err != nil {
		t.Fatalf("GenerateTree: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2: %+v", len(ops), ops)
	}
	if ops[0].Content != "# Go contexts" || ops[0].Parent != "" {
		t.Errorf("root op = %+v, want the topic heading at the root", ops[0])
	}
	if ops[1].Parent != ops[0].TempID {
		t.Errorf("child parent = %q, want the root temp id %q", ops[1].Parent, ops[0].TempID)
	}
	// The rendered prompt (carrying the topic) and a token budget reach the model.
	if model, _ := gotBody["model"].(string); model != "llama3" {
		t.Errorf("model = %q, want llama3", gotBody["model"])
	}
	if mt, _ := gotBody["max_tokens"].(float64); mt <= 0 {
		t.Errorf("max_tokens = %v, want a positive budget", gotBody["max_tokens"])
	}
	if !strings.Contains(bodyText(gotBody), "Go contexts") {
		t.Errorf("request did not carry the topic to the model: %v", gotBody["messages"])
	}
}

func TestGenerateTreeWorksWithoutAPIKey(t *testing.T) {
	// A local model (Ollama, LM Studio) needs no key; the request must still be
	// made and must NOT carry an Authorization header. This is the "data never
	// leaves the machine" path.
	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Errorf("Authorization header sent with no key: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	})

	ops, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if err != nil {
		t.Fatalf("GenerateTree without a key: %v", err)
	}
	if len(ops) != 2 {
		t.Errorf("got %d ops, want 2", len(ops))
	}
}

func TestGenerateTreeMapsErrors(t *testing.T) {
	respond := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
		wantMsg string
	}{
		{
			name:    "server error",
			handler: respond(http.StatusInternalServerError, apiErrBody("overloaded")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "overloaded",
		},
		{
			name:    "unauthorized",
			handler: respond(http.StatusUnauthorized, apiErrBody("invalid api key")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "invalid api key",
		},
		{
			name:    "non-2xx boundary",
			handler: respond(http.StatusMultipleChoices, apiErrBody("choose")),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			// A 200 that isn't the chat envelope (a proxy or CDN HTML page).
			name:    "non-JSON envelope",
			handler: respond(http.StatusOK, "<html>gateway</html>"),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			// Valid JSON, but without "choices": a gateway answered, not the model.
			name:    "valid JSON, not an envelope",
			handler: respond(http.StatusOK, `{"foo":"bar"}`),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			// The model answered, but the text is not the expected JSON tree.
			name:    "unparsable text",
			handler: respond(http.StatusOK, chatReply("sorry, I cannot help with that")),
			want:    knowledge.ErrLLMBadResponse,
		},
		{
			// A 200 with no choices yields nothing to parse.
			name:    "no choices",
			handler: respond(http.StatusOK, `{"choices":[]}`),
			want:    knowledge.ErrLLMBadResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, "test-key", tc.handler)
			ops, err := c.GenerateTree(t.Context(), "Go contexts", "")
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if ops != nil {
				t.Errorf("ops = %+v, want nil on error", ops)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not surface the API message %q", err, tc.wantMsg)
			}
		})
	}
}

func TestGenerateTreeTransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // Do() now fails at the transport layer

	c := New(url, "test-key", "llama3")
	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
}

// bodyText concatenates the user message text of a decoded request body.
func bodyText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	var b strings.Builder
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if s, ok := mm["content"].(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}

// apiErrBody builds an OpenAI-style error body with the given message.
func apiErrBody(msg string) string {
	return `{"error":{"message":"` + msg + `","type":"invalid_request_error"}}`
}

// jsonString quotes s as a JSON string literal for embedding in a canned reply.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
