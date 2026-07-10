package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	// The rendered prompt (carrying the topic) must reach the model.
	model, _ := gotBody["model"].(string)
	if model == "" {
		t.Error("request omitted the model")
	}
	if !strings.Contains(bodyText(gotBody), "Go contexts") {
		t.Errorf("request did not carry the topic to the model: %v", gotBody["messages"])
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

// jsonString quotes s as a JSON string literal for embedding in a canned reply.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
