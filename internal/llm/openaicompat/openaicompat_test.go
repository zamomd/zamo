package openaicompat

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
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &gotBody), "request body is not JSON")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	})

	ops, err := c.GenerateTree(t.Context(), "Go contexts", "cancellation")
	require.NoError(t, err)
	require.Len(t, ops, 2)
	assert.Equal(t, "# Go contexts", ops[0].Content)
	assert.Empty(t, ops[0].Parent, "want the topic heading at the root")
	assert.Equal(t, ops[0].TempID, ops[1].Parent, "child must hang under the root")
	model, _ := gotBody["model"].(string)
	assert.Equal(t, "llama3", model)
	mt, _ := gotBody["max_tokens"].(float64)
	assert.Positive(t, mt, "want a positive max_tokens budget")
	assert.Contains(t, bodyText(gotBody), "Go contexts", "request did not carry the topic to the model")
}

func TestGenerateEditParsesReplyAndCarriesScope(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &gotBody), "request body is not JSON")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(
			`[{"op":"edit","block_id":"01A","content":"revised"},`+
				`{"op":"delete","block_id":"01B"}]`))
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

func TestGenerateTreeWorksWithoutAPIKey(t *testing.T) {
	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		assert.NotContains(t, r.Header, "Authorization", "Authorization header sent with no key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	})

	ops, err := c.GenerateTree(t.Context(), "Go contexts", "")
	require.NoError(t, err)
	assert.Len(t, ops, 2)
}

func TestGenerateTreeTrimsTrailingSlashOnBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply(tree))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/", staticKey("test-key"), "llama3")
	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	require.NoError(t, err)
}

func TestGenerateTreeReadsOnlyTheFirstChoice(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[`+
			`{"message":{"role":"assistant","content":""}},`+
			`{"message":{"role":"assistant","content":`+jsonString(tree)+`}}]}`)
	})

	_, err := c.GenerateTree(t.Context(), "Go contexts", "")
	assert.ErrorIs(t, err, knowledge.ErrLLMBadResponse, "empty first choice, no splicing")
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
	require.NoError(t, c.Ping(t.Context()))
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "Bearer test-key", gotAuth)
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
	require.NoError(t, c.Ping(t.Context()))
	assert.False(t, hadAuth, "Authorization header sent with an empty key")
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
			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, ops)
			if tc.wantMsg != "" {
				assert.ErrorContains(t, err, tc.wantMsg, "error does not surface the API message")
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
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
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
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	require.ErrorContains(t, err, "307")
	assert.False(t, followed.Load(), "the prompt was re-POSTed to the redirect target")
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

	_, err := c.GenerateTree(ctx, "Go contexts", "")
	require.NoError(t, err)
	want := []knowledge.GenerationPhase{
		knowledge.PhaseRequesting, knowledge.PhaseAwaiting, knowledge.PhaseParsing,
	}
	assert.Equal(t, want, phases)
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
	_, err := c.GenerateEdit(ctx, scope, "revise it")
	require.NoError(t, err)
	want := []knowledge.GenerationPhase{
		knowledge.PhaseRequesting, knowledge.PhaseAwaiting, knowledge.PhaseParsing,
	}
	assert.Equal(t, want, phases)
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
	require.NoError(t, err)
	assert.Equal(t, doc, got, "want the model's text unparsed")
	want := []knowledge.GenerationPhase{knowledge.PhaseRequesting, knowledge.PhaseAwaiting}
	assert.Equal(t, want, phases, "no parsing phase for a document")
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
	require.NoError(t, err)
	assert.Equal(t, doc, got, "want the model's text unparsed")
	want := []knowledge.GenerationPhase{knowledge.PhaseRequesting, knowledge.PhaseAwaiting}
	assert.Equal(t, want, phases, "no parsing phase for a document")
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
