package anthropic

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

// newTestClient points a Client at a stub Messages API server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-key", "")
	c.baseURL = srv.URL
	return c
}

func TestGenerateTreeParsesReply(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The request must be a well-formed Messages API call.
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("request = %s %s, want POST /v1/messages", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		if r.Header.Get("Anthropic-Version") == "" {
			t.Error("Anthropic-Version header is missing")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+
			jsonString(`{"content":"# Go contexts","children":[`+
				`{"content":"context.Context carries deadlines","children":[]}]}`)+
			`}]}`)
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

	// The rendered prompt (carrying the topic) must reach the model, with a
	// token budget for the reply.
	model, _ := gotBody["model"].(string)
	if model == "" {
		t.Error("request omitted the model")
	}
	if mt, _ := gotBody["max_tokens"].(float64); mt <= 0 {
		t.Errorf("max_tokens = %v, want a positive budget", gotBody["max_tokens"])
	}
	if !strings.Contains(bodyText(gotBody), "Go contexts") {
		t.Errorf("request did not carry the topic to the model: %v", gotBody["messages"])
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
		wantMsg string // when set, the API's error message must reach the caller
	}{
		{
			// A server-side failure is transient — the caller may retry.
			name:    "server error",
			handler: respond(http.StatusInternalServerError, apiErrBody("overloaded_error", "overloaded")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "overloaded",
		},
		{
			// A bad key is an availability problem, not a content one.
			name:    "unauthorized",
			handler: respond(http.StatusUnauthorized, apiErrBody("authentication_error", "invalid x-api-key")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "invalid x-api-key",
		},
		{
			// The 2xx boundary: even a 300 is not success and must not be parsed.
			name:    "non-2xx boundary",
			handler: respond(http.StatusMultipleChoices, apiErrBody("redirect", "choose")),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			// A 200 whose body is not the Messages JSON envelope (a proxy or CDN
			// returning an HTML page) is an infrastructure failure, not the model
			// answering with bad content.
			name:    "non-JSON envelope",
			handler: respond(http.StatusOK, "<html>gateway</html>"),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			// The model answered, but the text is not the expected JSON tree.
			name:    "unparsable text",
			handler: respond(http.StatusOK, `{"content":[{"type":"text","text":"sorry, I cannot help with that"}]}`),
			want:    knowledge.ErrLLMBadResponse,
		},
		{
			// A 200 with no text block yields nothing to parse.
			name:    "no text blocks",
			handler: respond(http.StatusOK, `{"content":[]}`),
			want:    knowledge.ErrLLMBadResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, tc.handler)
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

func TestGenerateTreeMissingKeyIsUnavailable(t *testing.T) {
	// No key configured must fail before any network call, as unavailable.
	c := New("", "")
	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
}

func TestGenerateTreeTransportFailureIsUnavailable(t *testing.T) {
	// Point the client at a server that is already closed: Do() fails at the
	// transport layer, which must surface as ErrLLMUnavailable.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := New("test-key", "")
	c.baseURL = url

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

// apiErrBody builds a Messages API error body with the given error type and message.
func apiErrBody(kind, msg string) string {
	return `{"type":"error","error":{"type":"` + kind + `","message":"` + msg + `"}}`
}

// jsonString quotes s as a JSON string literal for embedding in a canned reply.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
