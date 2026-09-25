package anthropic

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func staticKey(key string) llm.KeySource {
	return func() (string, error) { return key, nil }
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(staticKey("test-key"), "")
	c.baseURL = srv.URL
	return c
}

func TestGenerateTreeParsesReply(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		assert.NotEmpty(t, r.Header.Get("Anthropic-Version"), "Anthropic-Version header is missing")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &gotBody), "request body is not JSON")

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+
			jsonString(`{"content":"# Go contexts","children":[`+
				`{"content":"context.Context carries deadlines","children":[]}]}`)+
			`}]}`)
	})

	ops, err := c.GenerateTree(t.Context(), "Go contexts", "cancellation")
	require.NoError(t, err)
	require.Len(t, ops, 2)
	assert.Equal(t, "# Go contexts", ops[0].Content)
	assert.Empty(t, ops[0].Parent, "want the topic heading at the root")
	assert.Equal(t, ops[0].TempID, ops[1].Parent, "child must hang under the root")

	model, _ := gotBody["model"].(string)
	assert.NotEmpty(t, model, "request omitted the model")
	mt, _ := gotBody["max_tokens"].(float64)
	assert.Positive(t, mt, "want a positive max_tokens budget")
	assert.Contains(t, bodyText(gotBody), "Go contexts", "request did not carry the topic to the model")
}

func TestGenerateTreeReadsKeyPerRequest(t *testing.T) {
	var gotKeys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"{\"content\":\"root\"}"}]}`)
	}))
	t.Cleanup(srv.Close)

	key := "old"
	c := New(func() (string, error) { return key, nil }, "")
	c.baseURL = srv.URL

	_, err := c.GenerateTree(t.Context(), "go", "")
	require.NoError(t, err)
	key = "new"
	_, err = c.GenerateTree(t.Context(), "go", "")
	require.NoError(t, err)

	assert.Equal(t, []string{"old", "new"}, gotKeys, "key must be read per request, not cached")
}

func TestGenerateEditParsesReplyAndCarriesScope(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+
			jsonString(`[{"op":"edit","block_id":"01A","content":"revised"},`+
				`{"op":"delete","block_id":"01B"}]`)+`}]}`)
	})

	scope := []knowledge.Block{{ID: "01A", Content: "old text"}, {ID: "01B", Content: "remove me"}}
	ops, err := c.GenerateEdit(t.Context(), scope, "revise the first, drop the second")
	require.NoError(t, err)
	require.Len(t, ops, 2)
	require.Equal(t, knowledge.OpEdit, ops[0].Kind)
	require.Equal(t, knowledge.OpDelete, ops[1].Kind)
	prompt := bodyText(gotBody)
	assert.Contains(t, prompt, "revise the first, drop the second", "request did not carry the instruction")
	assert.Contains(t, prompt, "01A", "request did not carry the scope")
}

func TestPingChecksModelsEndpoint(t *testing.T) {
	var gotPath, gotKey string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	require.NoError(t, c.Ping(t.Context()))
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "test-key", gotKey)
}

func TestPingMapsUnauthorized(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid x-api-key"}}`)
	})
	err := c.Ping(t.Context())
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestGenerateEditMissingKeyIsUnavailable(t *testing.T) {
	c := New(staticKey(""), "")
	_, err := c.GenerateEdit(t.Context(), []knowledge.Block{{ID: "01A", Content: "x"}}, "do a thing")
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
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
			name:    "server error",
			handler: respond(http.StatusInternalServerError, apiErrBody("overloaded_error", "overloaded")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "overloaded",
		},
		{
			name:    "unauthorized",
			handler: respond(http.StatusUnauthorized, apiErrBody("authentication_error", "invalid x-api-key")),
			want:    knowledge.ErrLLMUnavailable,
			wantMsg: "invalid x-api-key",
		},
		{
			name:    "non-2xx boundary",
			handler: respond(http.StatusMultipleChoices, apiErrBody("redirect", "choose")),
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
			handler: respond(http.StatusOK, `{"content":[{"type":"text","text":"sorry, I cannot help with that"}]}`),
			want:    knowledge.ErrLLMBadResponse,
		},
		{
			name:    "no text blocks",
			handler: respond(http.StatusOK, `{"content":[]}`),
			want:    knowledge.ErrLLMBadResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, tc.handler)
			ops, err := c.GenerateTree(t.Context(), "Go contexts", "")
			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, ops)
			if tc.wantMsg != "" {
				assert.ErrorContains(t, err, tc.wantMsg, "error does not surface the API message")
			}
		})
	}
}

func TestGenerateTreeMissingKeyIsUnavailable(t *testing.T) {
	c := New(staticKey(""), "")
	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestGenerateTreeTransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := New(staticKey("test-key"), "")
	c.baseURL = url

	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestGenerateTreeDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Store(true)
	}))
	t.Cleanup(target.Close)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})

	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	require.ErrorContains(t, err, "307")
	assert.False(t, followed.Load(), "the prompt was re-POSTed to the redirect target")
}

func TestGenerateTreeReportsProgressPhases(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+
			jsonString(`{"content":"root","children":[]}`)+`}]}`)
	})

	var phases []knowledge.GenerationPhase
	ctx := knowledge.WithProgress(t.Context(), func(p knowledge.Progress) {
		phases = append(phases, p.Phase)
	})

	_, err := c.GenerateTree(ctx, "Go contexts", "")
	require.NoError(t, err)
	want := []knowledge.GenerationPhase{
		knowledge.PhaseRequesting, knowledge.PhaseAwaiting, knowledge.PhaseParsing,
	}
	assert.Equal(t, want, phases)
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

func apiErrBody(kind, msg string) string {
	return `{"type":"error","error":{"type":"` + kind + `","message":"` + msg + `"}}`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
