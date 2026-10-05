package llm

import (
	"encoding/json/jsontext"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

// echoDialect spells a request as the text of its last message and reads the
// reply body as the model's text, so a test sees exactly what crossed.
type echoDialect struct {
	url   string
	reply Reply
}

func (d *echoDialect) CompletionURL() string          { return d.url }
func (d *echoDialect) ModelsURL() string              { return d.url }
func (d *echoDialect) KeyRequired() bool              { return false }
func (d *echoDialect) SetHeaders(http.Header, string) {}

func (d *echoDialect) EncodeChat(r Request) ([]byte, error) {
	return []byte(r.Messages[len(r.Messages)-1].Text), nil
}

func (d *echoDialect) DecodeChat(body []byte) (Reply, error) {
	if d.reply.Text != "" || len(d.reply.Calls) > 0 {
		return d.reply, nil
	}
	return Reply{Text: string(body)}, nil
}

func newTestProvider(t *testing.T, reply Reply, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewProvider(func() (string, error) { return "", nil }, &echoDialect{url: srv.URL, reply: reply})
}

func userTurn(text string) []Message {
	return []Message{{Role: RoleUser, Text: text}}
}

var lookupTool = []ToolDef{{
	Name: "lookup", Description: "look a thing up", Schema: jsontext.Value(`{"type":"object"}`),
}}

func TestChatSendsEncodedRequestAndReturnsText(t *testing.T) {
	p := newTestProvider(t, Reply{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = io.WriteString(w, "heard: "+string(body))
	})

	reply, err := p.Chat(t.Context(), Request{Messages: userTurn("hello")})

	require.NoError(t, err)
	assert.Equal(t, "heard: hello", reply.Text)
	assert.Empty(t, reply.Calls)
}

func TestChatReturnsToolCalls(t *testing.T) {
	want := Reply{Calls: []ToolCall{{ID: "c1", Name: "lookup", Args: jsontext.Value(`{"q":"go"}`)}}}
	p := newTestProvider(t, want, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{}")
	})

	reply, err := p.Chat(t.Context(), Request{Messages: userTurn("find go"), Tools: lookupTool})

	require.NoError(t, err)
	assert.Equal(t, want, reply)
}

func TestChatMapsServerErrorToUnavailable(t *testing.T) {
	p := newTestProvider(t, Reply{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"overloaded"}}`)
	})

	_, err := p.Chat(t.Context(), Request{Messages: userTurn("hello")})

	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	assert.ErrorContains(t, err, "overloaded")
}

func TestChatRefusedToolsIsNoToolSupport(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "ollama",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools","type":"api_error"}}`,
		},
		{
			name:   "openrouter",
			status: http.StatusNotFound,
			body:   `{"error":{"message":"No endpoints found that support tool use.","code":404}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, Reply{}, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})

			_, err := p.Chat(t.Context(), Request{Messages: userTurn("find go"), Tools: lookupTool})

			require.ErrorIs(t, err, knowledge.ErrLLMNoToolSupport)
			assert.NotErrorIs(t, err, knowledge.ErrLLMUnavailable)
		})
	}
}

func TestChatToolComplaintWithoutToolsStaysUnavailable(t *testing.T) {
	p := newTestProvider(t, Reply{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"model does not support tools"}}`)
	})

	_, err := p.Chat(t.Context(), Request{Messages: userTurn("hello")})

	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestChatReadsHTTPReplyUpToTheCap(t *testing.T) {
	p := newTestProvider(t, Reply{}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxHTTPReplyBytes+1))
	})

	reply, err := p.Chat(t.Context(), Request{Messages: userTurn("hello")})

	require.NoError(t, err)
	assert.Len(t, reply.Text, maxHTTPReplyBytes, "the body is read up to the cap and no further")
}
