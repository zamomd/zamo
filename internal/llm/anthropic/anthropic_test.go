package anthropic

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
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

var hello = llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "what is a context?"}}}

const textReply = `{"content":[{"type":"text","text":"a deadline carrier"}]}`

func TestChatPostsToTheMessagesEndpoint(t *testing.T) {
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
		_, _ = io.WriteString(w, textReply)
	})

	reply, err := c.Chat(t.Context(), hello)

	require.NoError(t, err)
	assert.Equal(t, "a deadline carrier", reply.Text)
	assert.Equal(t, DefaultModel, gotBody["model"], "an empty model selects the default")
	mt, _ := gotBody["max_tokens"].(float64)
	assert.Positive(t, mt, "want a positive max_tokens budget")
}

func TestChatReadsKeyPerRequest(t *testing.T) {
	var gotKeys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, textReply)
	}))
	t.Cleanup(srv.Close)

	key := "old"
	c := New(func() (string, error) { return key, nil }, "")
	c.baseURL = srv.URL

	_, err := c.Chat(t.Context(), hello)
	require.NoError(t, err)
	key = "new"
	_, err = c.Chat(t.Context(), hello)
	require.NoError(t, err)

	assert.Equal(t, []string{"old", "new"}, gotKeys, "key must be read per request, not cached")
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

func TestChatMissingKeyIsUnavailable(t *testing.T) {
	c := New(staticKey(""), "")
	_, err := c.Chat(t.Context(), hello)
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestChatMapsErrors(t *testing.T) {
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
		wantMsg string // when set, the API's error message must reach the caller
	}{
		{
			name:    "server error",
			handler: respond(http.StatusInternalServerError, apiErrBody("overloaded_error", "overloaded")),
			wantMsg: "overloaded",
		},
		{
			name:    "unauthorized",
			handler: respond(http.StatusUnauthorized, apiErrBody("authentication_error", "invalid x-api-key")),
			wantMsg: "invalid x-api-key",
		},
		{
			name:    "non-2xx boundary",
			handler: respond(http.StatusMultipleChoices, apiErrBody("redirect", "choose")),
		},
		{
			name:    "non-JSON envelope",
			handler: respond(http.StatusOK, "<html>gateway</html>"),
		},
		{
			name:    "valid JSON, not an envelope",
			handler: respond(http.StatusOK, `{"foo":"bar"}`),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, tc.handler)
			reply, err := c.Chat(t.Context(), hello)
			require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
			assert.Empty(t, reply)
			if tc.wantMsg != "" {
				assert.ErrorContains(t, err, tc.wantMsg, "error does not surface the API message")
			}
		})
	}
}

func TestChatWithAnEmptyContentListIsAnEmptyReply(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[]}`)
	})

	reply, err := c.Chat(t.Context(), hello)

	require.NoError(t, err)
	assert.Empty(t, reply)
}

func TestChatTransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := New(staticKey("test-key"), "")
	c.baseURL = url

	_, err := c.Chat(t.Context(), hello)
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestChatDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Store(true)
	}))
	t.Cleanup(target.Close)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})

	_, err := c.Chat(t.Context(), hello)
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	require.ErrorContains(t, err, "307")
	assert.False(t, followed.Load(), "the conversation was re-POSTed to the redirect target")
}

func apiErrBody(kind, msg string) string {
	return `{"type":"error","error":{"type":"` + kind + `","message":"` + msg + `"}}`
}
