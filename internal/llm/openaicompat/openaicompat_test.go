package openaicompat

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

func chatReply(content string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + jsonString(content) + `}}]}`
}

func newTestClient(t *testing.T, apiKey string, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, staticKey(apiKey), "llama3")
}

var hello = llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "what is a context?"}}}

func TestChatPostsToTheCompletionsEndpoint(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &gotBody), "request body is not JSON")

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply("a deadline carrier"))
	})

	reply, err := c.Chat(t.Context(), hello)

	require.NoError(t, err)
	assert.Equal(t, "a deadline carrier", reply.Text)
	assert.Equal(t, "llama3", gotBody["model"])
}

func TestChatWorksWithoutAPIKey(t *testing.T) {
	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		assert.NotContains(t, r.Header, "Authorization", "Authorization header sent with no key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply("ok"))
	})

	reply, err := c.Chat(t.Context(), hello)
	require.NoError(t, err)
	assert.Equal(t, "ok", reply.Text)
}

func TestChatTrimsTrailingSlashOnBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatReply("ok"))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/", staticKey("test-key"), "llama3")
	_, err := c.Chat(t.Context(), hello)
	require.NoError(t, err)
}

func TestChatReadsOnlyTheFirstChoice(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[`+
			`{"message":{"role":"assistant","content":""}},`+
			`{"message":{"role":"assistant","content":"an alternative"}}]}`)
	})

	reply, err := c.Chat(t.Context(), hello)

	require.NoError(t, err)
	assert.Empty(t, reply.Text, "empty first choice, no splicing")
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
		wantMsg string
	}{
		{
			name:    "server error",
			handler: respond(http.StatusInternalServerError, apiErrBody("overloaded")),
			wantMsg: "overloaded",
		},
		{
			name:    "unauthorized",
			handler: respond(http.StatusUnauthorized, apiErrBody("invalid api key")),
			wantMsg: "invalid api key",
		},
		{
			name:    "3xx is not success",
			handler: respond(http.StatusMultipleChoices, chatReply("ok")),
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
			c := newTestClient(t, "test-key", tc.handler)
			reply, err := c.Chat(t.Context(), hello)
			require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
			assert.Empty(t, reply)
			if tc.wantMsg != "" {
				assert.ErrorContains(t, err, tc.wantMsg, "error does not surface the API message")
			}
		})
	}
}

func TestChatWithNoChoicesIsAnEmptyReply(t *testing.T) {
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	})

	reply, err := c.Chat(t.Context(), hello)

	require.NoError(t, err)
	assert.Empty(t, reply)
}

func TestChatTransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // Do() now fails at the transport layer

	c := New(url, staticKey("test-key"), "llama3")
	_, err := c.Chat(t.Context(), hello)
	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestChatDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Store(true)
	}))
	t.Cleanup(target.Close)
	c := newTestClient(t, "test-key", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})

	_, err := c.Chat(t.Context(), hello)
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	require.ErrorContains(t, err, "307")
	assert.False(t, followed.Load(), "the conversation was re-POSTed to the redirect target")
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
