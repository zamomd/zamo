package openaicompat

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

const tree = `{"content":"# Go contexts","children":[` +
	`{"content":"context.Context carries deadlines","children":[]}]}`

func staticKey(key string) llm.KeySource {
	return func() (string, error) { return key, nil }
}

func chatReply(content string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + jsonString(content) + `}}]}`
}

func newTestClient(t *testing.T, apiKey string, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, staticKey(apiKey), "llama3")
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

func TestGenerateEditParsesReplyAndCarriesScope(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(
			`[{"op":"edit","block_id":"01A","content":"revised"},`+
				`{"op":"delete","block_id":"01B"}]`))
	})

	scope := []knowledge.Block{{ID: "01A", Content: "old text"}, {ID: "01B", Content: "remove me"}}
	ops, err := c.GenerateEdit(t.Context(), scope, "revise the first, drop the second")
	if err != nil {
		t.Fatalf("GenerateEdit: %v", err)
	}
	if len(ops) != 2 || ops[0].Kind != knowledge.OpEdit || ops[1].Kind != knowledge.OpDelete {
		t.Fatalf("ops = %+v, want an edit and a delete", ops)
	}
	prompt := bodyText(gotBody)
	if !strings.Contains(prompt, "revise the first, drop the second") || !strings.Contains(prompt, "01A") {
		t.Errorf("request did not carry the instruction and scope to the model:\n%s", prompt)
	}
}

func TestGenerateTreeWorksWithoutAPIKey(t *testing.T) {
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

func TestGenerateTreeTrimsTrailingSlashOnBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/", staticKey("test-key"), "llama3")
	if _, err := c.GenerateTree(t.Context(), "Go contexts", ""); err != nil {
		t.Fatalf("GenerateTree with trailing slash: %v", err)
	}
}

func TestGenerateTreeReadsOnlyTheFirstChoice(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[`+
			`{"message":{"role":"assistant","content":""}},`+
			`{"message":{"role":"assistant","content":`+jsonString(tree)+`}}]}`)
	})

	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if !errors.Is(err, knowledge.ErrLLMBadResponse) {
		t.Errorf("error = %v, want ErrLLMBadResponse (empty first choice, no splicing)", err)
	}
}

func TestPingChecksModelsEndpoint(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/v1", staticKey("test-key"), "llama3")
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping = %v, want nil", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
}

func TestPingWithoutKeyOmitsAuthorization(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/v1", staticKey(""), "llama3")
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping = %v, want nil", err)
	}
	if hadAuth {
		t.Error("Authorization header sent with an empty key, want none")
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
			name:    "3xx is not success",
			handler: respond(http.StatusMultipleChoices, chatReply(tree)),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			name:    "non-JSON envelope",
			handler: respond(http.StatusOK, "<html>gateway</html>"),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			name:    "valid JSON, not an envelope",
			handler: respond(http.StatusOK, `{"foo":"bar"}`),
			want:    knowledge.ErrLLMUnavailable,
		},
		{
			name:    "unparsable text",
			handler: respond(http.StatusOK, chatReply("sorry, I cannot help with that")),
			want:    knowledge.ErrLLMBadResponse,
		},
		{
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

	c := New(url, staticKey("test-key"), "llama3")
	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
}

func TestGenerateTreeDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Store(true)
	}))
	t.Cleanup(target.Close)
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})

	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	if !errors.Is(err, knowledge.ErrLLMUnavailable) || !strings.Contains(err.Error(), "307") {
		t.Errorf("error = %v, want ErrLLMUnavailable with HTTP 307", err)
	}
	if followed.Load() {
		t.Error("the prompt was re-POSTed to the redirect target")
	}
}

func TestGenerateTreeReportsProgressPhases(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	})

	var phases []knowledge.GenerationPhase
	ctx := knowledge.WithProgress(t.Context(), func(p knowledge.Progress) {
		phases = append(phases, p.Phase)
	})

	if _, err := c.GenerateTree(ctx, "Go contexts", ""); err != nil {
		t.Fatalf("GenerateTree: %v", err)
	}
	want := []knowledge.GenerationPhase{
		knowledge.PhaseRequesting, knowledge.PhaseAwaiting, knowledge.PhaseParsing,
	}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
}

func TestGenerateEditReportsProgressPhases(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(`[{"op":"edit","block_id":"01A","content":"x"}]`))
	})

	var phases []knowledge.GenerationPhase
	ctx := knowledge.WithProgress(t.Context(), func(p knowledge.Progress) {
		phases = append(phases, p.Phase)
	})

	scope := []knowledge.Block{{ID: "01A", Content: "old"}}
	if _, err := c.GenerateEdit(ctx, scope, "revise it"); err != nil {
		t.Fatalf("GenerateEdit: %v", err)
	}
	want := []knowledge.GenerationPhase{
		knowledge.PhaseRequesting, knowledge.PhaseAwaiting, knowledge.PhaseParsing,
	}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
}

func TestGenerateDocumentReportsProgressPhasesAndSkipsParsing(t *testing.T) {
	const doc = "# Go contexts\n\nBody."
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(doc))
	})

	var phases []knowledge.GenerationPhase
	ctx := knowledge.WithProgress(t.Context(), func(p knowledge.Progress) {
		phases = append(phases, p.Phase)
	})

	got, err := c.GenerateDocument(ctx, "Go contexts", "")
	if err != nil {
		t.Fatalf("GenerateDocument: %v", err)
	}
	if got != doc {
		t.Errorf("GenerateDocument = %q, want the model's text unparsed: %q", got, doc)
	}
	want := []knowledge.GenerationPhase{knowledge.PhaseRequesting, knowledge.PhaseAwaiting}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v (no parsing phase for a document)", phases, want)
	}
}

func TestGenerateDocumentEditReportsProgressPhasesAndSkipsParsing(t *testing.T) {
	const doc = "# Go contexts\n\nNew body."
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(doc))
	})

	var phases []knowledge.GenerationPhase
	ctx := knowledge.WithProgress(t.Context(), func(p knowledge.Progress) {
		phases = append(phases, p.Phase)
	})

	original := knowledge.Block{ID: "01A", Content: "# Go contexts\n\nOld body."}
	got, err := c.GenerateDocumentEdit(ctx, original, "rewrite the body")
	if err != nil {
		t.Fatalf("GenerateDocumentEdit: %v", err)
	}
	if got != doc {
		t.Errorf("GenerateDocumentEdit = %q, want the model's text unparsed: %q", got, doc)
	}
	want := []knowledge.GenerationPhase{knowledge.PhaseRequesting, knowledge.PhaseAwaiting}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v (no parsing phase for a document)", phases, want)
	}
}

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

func apiErrBody(msg string) string {
	return `{"error":{"message":"` + msg + `","type":"invalid_request_error"}}`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
