package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

const (
	// MaxTokens bounds the reply. A reviewable expansion is a few dozen short
	// blocks, which fits comfortably; the parser caps the tree regardless. A
	// dialect puts it in the request body, so it is theirs to read.
	MaxTokens = 4096

	// maxHTTPReplyBytes bounds how much of the HTTP response is read into memory
	// before the parsers apply their own (smaller) cap on the model text.
	maxHTTPReplyBytes = 2 << 20 // 2 MiB
)

// Dialect is the provider-specific half of an HTTP LLM provider: how one API
// spells a chat request. Everything around it — prompt rendering, the round
// trip, the byte and status limits, progress reporting and the mapping onto the
// domain's error vocabulary — belongs to Provider and is shared.
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
	// EncodeRequest builds the completion request body.
	EncodeRequest(prompt string) ([]byte, error)
	// DecodeReply extracts the model's text from a 2xx body. A body that is not
	// this API's envelope — a proxy or CDN page rather than the model — must
	// yield ErrLLMUnavailable, not an empty string.
	DecodeReply(body []byte) (string, error)
}

// Provider implements knowledge.LLMClient over an HTTP API described by a
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

// GenerateTree implements knowledge.LLMClient: it renders the expansion prompt,
// calls the API, and parses the reply into add operations.
func (p *Provider) GenerateTree(ctx context.Context, topic, hint string) ([]knowledge.Op, error) {
	prompt, err := ExpandPrompt(topic, hint)
	if err != nil {
		// The template is embedded; failing to render it is a build defect,
		// still "the request cannot be made".
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	return p.generate(ctx, prompt, ParseOps)
}

// GenerateEdit implements knowledge.LLMClient: it renders the edit prompt over
// the scope subtree and the instruction, calls the API, and parses the reply
// into edit operations.
func (p *Provider) GenerateEdit(
	ctx context.Context,
	scope []knowledge.Block,
	instruction string,
) ([]knowledge.Op, error) {
	prompt, err := EditPrompt(scope, instruction)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMUnavailable, err)
	}
	return p.generate(ctx, prompt, ParseEditOps)
}

// generate is one completion round trip followed by the caller's parser.
func (p *Provider) generate(
	ctx context.Context,
	prompt string,
	parse func([]byte) ([]knowledge.Op, error),
) ([]knowledge.Op, error) {
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseRequesting})
	key, err := p.apiKey()
	if err != nil {
		return nil, err
	}
	payload, err := p.dialect.EncodeRequest(prompt)
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.dialect.CompletionURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", knowledge.ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	p.dialect.SetHeaders(req.Header, key)

	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseAwaiting})
	body, err := p.do(req)
	if err != nil {
		return nil, err
	}
	text, err := p.dialect.DecodeReply(body)
	if err != nil {
		return nil, err
	}
	knowledge.Report(ctx, knowledge.Progress{Phase: knowledge.PhaseParsing})
	return parse([]byte(text))
}

// Ping implements knowledge.LLMClient: it checks that the endpoint is reachable
// and the key, if the dialect needs one, is accepted. It deliberately does not
// exercise the configured model — a typo in the model name surfaces on the
// first expansion, not here.
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

// statusError turns a non-2xx response into ErrLLMUnavailable, folding in the
// API's error message when the body carries one.
func statusError(status int, body []byte) error {
	var e apiError
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("%w: HTTP %d: %s", knowledge.ErrLLMUnavailable, status, e.Error.Message)
	}
	return fmt.Errorf("%w: HTTP %d", knowledge.ErrLLMUnavailable, status)
}
