// Package llm is the boundary with LLM providers: one chat round with tools,
// spelled per provider by a Dialect, and the system prompts the agent sends.
package llm

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zamomd/zamo/internal/knowledge"
)

const (
	// MaxTokens bounds one reply: an answer, or a write batch of a reviewable
	// size. A dialect puts it in the request body, so it is theirs to read.
	MaxTokens = 4096

	// maxHTTPReplyBytes bounds how much of the HTTP response is read into
	// memory. The reply crosses the untrusted model boundary; anything larger
	// is hostile or broken.
	maxHTTPReplyBytes = 2 << 20 // 2 MiB
)

// Dialect is the provider-specific half of an HTTP LLM provider: how one API
// spells a chat request. Everything around it — the round trip, the byte and
// status limits and the mapping onto the domain's error vocabulary — belongs
// to Provider and is shared.
type Dialect interface {
	// CompletionURL is the endpoint a completion is POSTed to. It is asked for
	// per request rather than cached, so a client may retarget its base URL.
	CompletionURL() string
	// ModelsURL is the cheapest reachability probe the API offers; Ping GETs it.
	ModelsURL() string
	// KeyRequired reports whether an empty key is a configuration error rather
	// than a supported state — a local model is reached without one.
	KeyRequired() bool
	// SetHeaders applies the dialect's authorization and versioning headers.
	// key is empty only when KeyRequired reports false.
	SetHeaders(h http.Header, key string)
	// EncodeChat builds the request body for one chat round.
	EncodeChat(r Request) ([]byte, error)
	// DecodeChat extracts the model's turn from a 2xx body. A body that is not
	// this API's envelope — a proxy or CDN page rather than the model — must
	// yield ErrLLMUnavailable, not an empty reply.
	DecodeChat(body []byte) (Reply, error)
}

// Provider implements Client over an HTTP API described by a
// Dialect. Transport, key and HTTP-status failures map to ErrLLMUnavailable;
// a reply that will not parse maps to ErrLLMBadResponse (via the parsers).
type Provider struct {
	key     KeySource
	dialect Dialect
	http    *http.Client
}

// NewProvider wires a dialect to its key source. The key is read through the
// source on every request, never cached: see KeySource.
func NewProvider(key KeySource, d Dialect) *Provider {
	return &Provider{
		key:     key,
		dialect: d,
		http: &http.Client{
			Timeout: 60 * time.Second,
			// These APIs never redirect; following one would re-POST the prompt
			// to wherever Location points.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Chat implements Client: one round trip to the completion endpoint. A
// request that carried tools and was refused over them fails with
// ErrLLMNoToolSupport rather than ErrLLMUnavailable.
func (p *Provider) Chat(ctx context.Context, r Request) (Reply, error) {
	key, err := p.apiKey()
	if err != nil {
		return Reply{}, err
	}
	payload, err := p.dialect.EncodeChat(r)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: encode request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.dialect.CompletionURL(), bytes.NewReader(payload))
	if err != nil {
		return Reply{}, fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	p.dialect.SetHeaders(req.Header, key)

	body, err := p.do(req)
	if err != nil {
		if refused, ok := errors.AsType[*statusErr](err); ok && len(r.Tools) > 0 && refused.blamesTools() {
			return Reply{}, fmt.Errorf("%w: %s", knowledge.ErrLLMNoToolSupport, refused.message)
		}
		return Reply{}, err
	}
	return p.dialect.DecodeChat(body)
}

// Ping implements Client: it checks that the endpoint is reachable
// and the key, if the dialect needs one, is accepted. It deliberately does not
// exercise the configured model — a typo in the model name surfaces on the
// first request, not here.
func (p *Provider) Ping(ctx context.Context) error {
	key, err := p.apiKey()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.dialect.ModelsURL(), nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	p.dialect.SetHeaders(req.Header, key)
	_, err = p.do(req)
	return err
}

// apiKey reads the current key and enforces the dialect's requirement.
func (p *Provider) apiKey() (string, error) {
	key, err := p.key()
	if err != nil {
		return "", fmt.Errorf("%w: read API key: %w", knowledge.ErrLLMUnavailable, err)
	}
	if key == "" && p.dialect.KeyRequired() {
		return "", fmt.Errorf("%w: no API key configured", knowledge.ErrLLMUnavailable)
	}
	return key, nil
}

// do performs the request and returns the 2xx body.
func (p *Provider) do(req *http.Request) ([]byte, error) {
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPReplyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", knowledge.ErrLLMUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError(resp.StatusCode, body)
	}
	return body, nil
}

// apiError is the non-2xx body shape both dialects happen to share.
type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// statusErr is a non-2xx response: ErrLLMUnavailable carrying the status and
// the API's own message when the body had one.
type statusErr struct {
	status  int
	message string
}

func (e *statusErr) Error() string {
	if e.message == "" {
		return fmt.Sprintf("%v: HTTP %d", knowledge.ErrLLMUnavailable, e.status)
	}
	return fmt.Sprintf("%v: HTTP %d: %s", knowledge.ErrLLMUnavailable, e.status, e.message)
}

func (e *statusErr) Unwrap() error { return knowledge.ErrLLMUnavailable }

// blamesTools reports whether the API refused the request because the model
// cannot be given tools. No API has a code for it; they say so in the message.
func (e *statusErr) blamesTools() bool {
	return e.status >= 400 && e.status < 500 &&
		strings.Contains(strings.ToLower(e.message), "support tool")
}

func statusError(status int, body []byte) error {
	var e apiError
	_ = json.Unmarshal(body, &e)
	return &statusErr{status: status, message: e.Error.Message}
}
