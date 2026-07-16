// Package settings is the use case for the provider settings screen: read the
// effective configuration, save the mutable layer, swap the live client.
//
// It sits outside knowledge on purpose. The domain knows nothing about
// providers, TOML or paths, and must not learn: it only ever sees an
// LLMClient. Nor does this belong in wailsbind, which stays a pass-through —
// the GUI is not the only entry point.
package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/cominddev/comind/internal/config"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/cominddev/comind/internal/llm/anthropic"
	"github.com/cominddev/comind/internal/llm/openaicompat"
)

// ErrDeclarative is returned when config.toml manages the settings and the
// mutable layer is off. It is enforced here rather than in the UI: the screen
// is not the only caller.
var ErrDeclarative = errors.New("settings are managed by config.toml")

// Field is one settable value plus where it came from.
type Field struct {
	Value  string `json:"value"`
	Origin string `json:"origin"`
}

// View is what the settings screen renders. It carries no key value in any
// form — only whether one is set and where it comes from.
type View struct {
	Provider  Field `json:"provider"`
	Model     Field `json:"model"`
	BaseURL   Field `json:"baseUrl"`
	APIKeySet bool  `json:"apiKeySet"`
	// APIKeyOrigin uses config.Origin's string form directly.
	APIKeyOrigin string `json:"apiKeyOrigin"`
	// Declarative means every field is read-only: config.toml owns them.
	Declarative bool `json:"declarative"`
}

// In is a settings submission. An empty field means "not overridden" and is
// dropped from the mutable layer, which is how "reset to config" is expressed.
// An empty APIKey leaves the stored key alone; ClearAPIKey removes it.
type In struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"apiKey"`
	ClearAPIKey bool   `json:"clearApiKey"`
}

// Manager owns the settings use case.
type Manager struct {
	holder *llm.Holder
	path   string
}

// New wires the manager to the live client holder and the mutable layer's path.
func New(holder *llm.Holder, path string) *Manager {
	return &Manager{holder: holder, path: path}
}

// BuildClient assembles a provider client from a resolved configuration. The
// key is passed as a source, not a value: it must be read per request so a
// rotated key file takes effect (see llm.KeySource). An empty provider is a
// supported state — AI features off — and yields llm.Disabled rather than an
// error; config.Load and config.ResolveWith already reject any other unknown
// provider before a Resolved reaches here.
func BuildClient(r config.Resolved) (knowledge.LLMClient, error) {
	switch r.Config.LLM.Provider {
	case "":
		return llm.Disabled{}, nil
	case config.ProviderAnthropic:
		return anthropic.New(r.APIKeyValue, r.Config.LLM.Model), nil
	case config.ProviderOpenAICompat:
		return openaicompat.New(r.Config.LLM.BaseURL, r.APIKeyValue, r.Config.LLM.Model), nil
	default:
		return nil, fmt.Errorf("unknown llm provider %q", r.Config.LLM.Provider)
	}
}

// Current reports the effective settings and the origin of each field.
func (m *Manager) Current(_ context.Context) (View, error) {
	r, err := config.ResolveFrom("", m.path)
	if err != nil {
		return View{}, err
	}
	return View{
		Provider:     Field{Value: r.Config.LLM.Provider, Origin: string(r.Origins.Provider)},
		Model:        Field{Value: r.Config.LLM.Model, Origin: string(r.Origins.Model)},
		BaseURL:      Field{Value: r.Config.LLM.BaseURL, Origin: string(r.Origins.BaseURL)},
		APIKeySet:    r.APIKeySet(),
		APIKeyOrigin: string(r.Origins.APIKey),
		Declarative:  r.Config.Declarative,
	}, nil
}

// Save validates the submission, writes the mutable layer, and swaps the live
// client. The order matters: a submission that cannot produce a client must
// leave both the file and the running client untouched.
//
// It resolves against the raw config.toml (config.Load), not against
// current.Config: ResolveFrom already folds settings.toml into current.Config,
// so passing that merged value back into config.ResolveWith together with the
// new overrides would double-merge — any field the submission drops (an empty
// override, meaning "reset to config") would keep its stale settings.toml
// value baked into the "config" layer instead of falling back to config.toml.
// current itself is still the right source for the currently stored API key
// and the declarative flag, both of which are read from the merged view on
// purpose (see overrides and the check above).
func (m *Manager) Save(_ context.Context, in In) error {
	current, err := config.ResolveFrom("", m.path)
	if err != nil {
		return err
	}
	if current.Config.Declarative {
		return ErrDeclarative
	}

	next, err := m.overrides(current, in)
	if err != nil {
		return err
	}

	base, err := config.Load("")
	if err != nil {
		return err
	}
	resolved, err := config.ResolveWith(base, next)
	if err != nil {
		return err
	}
	// Checked against the resolved value, not in.BaseURL: a submission that
	// relies on a base_url already declared in config.toml must still be
	// accepted. openai-compatible only exists to point at some endpoint
	// (OpenAI, OpenRouter, Ollama, LM Studio...), so without one it saves a
	// provider that can never work and fails later with an opaque transport
	// error instead of this clear one.
	if resolved.Config.LLM.Provider == config.ProviderOpenAICompat && resolved.Config.LLM.BaseURL == "" {
		return errors.New("openai-compatible requires a base URL")
	}
	// Checked against the newly-resolved state, i.e. the provider being
	// submitted: config.keyOrigin already looks at whichever provider it is
	// given, so this correctly guards the submitted provider's env var rather
	// than the one currently in effect, allowing a switch away from a provider
	// whose env var happens to be set. This still runs before BuildClient and
	// SaveOverrides, so a refusal leaves both the file and the live client
	// untouched.
	if resolved.Origins.APIKey == config.OriginEnv && in.APIKey != "" {
		envName := config.KeyEnvName(resolved.Config.LLM.Provider)
		return fmt.Errorf("%s already supplies the API key and is in use; "+
			"clear the key field and save again to keep the rest of your change", envName)
	}
	client, err := BuildClient(resolved)
	if err != nil {
		return err
	}
	if err := config.SaveOverrides(m.path, next); err != nil {
		return err
	}
	m.holder.Set(client)
	return nil
}

// Check builds a client from a submission that has not been saved and asks the
// provider whether it is reachable. It writes nothing and does not swap the
// live client, so it works in declarative mode too.
//
// Like Save, it resolves against the raw config.toml rather than current's
// already-merged Config, for the same double-merge reason.
func (m *Manager) Check(ctx context.Context, in In) error {
	current, err := config.ResolveFrom("", m.path)
	if err != nil {
		return err
	}
	next := current.Overrides
	if !current.Config.Declarative {
		if next, err = m.overrides(current, in); err != nil {
			return err
		}
	}
	base, err := config.Load("")
	if err != nil {
		return err
	}
	resolved, err := config.ResolveWith(base, next)
	if err != nil {
		return err
	}
	client, err := BuildClient(resolved)
	if err != nil {
		return err
	}
	return client.Ping(ctx)
}

// overrides folds a submission into the mutable layer, validating the
// provider. An empty field drops the override rather than storing an empty
// string: absent means "not overridden", "" would be a real override. An
// empty provider is accepted too — it means "not overridden", not "AI off";
// BuildClient is what turns an all-empty result into the disabled state.
func (m *Manager) overrides(current config.Resolved, in In) (config.Overrides, error) {
	if in.Provider != "" && in.Provider != config.ProviderAnthropic && in.Provider != config.ProviderOpenAICompat {
		return config.Overrides{}, fmt.Errorf("unknown llm provider %q (available: %s, %s)",
			in.Provider, config.ProviderAnthropic, config.ProviderOpenAICompat)
	}

	o := config.Overrides{LLM: config.LLMOverrides{
		Provider: in.Provider,
		Model:    in.Model,
		BaseURL:  in.BaseURL,
		APIKey:   current.Overrides.LLM.APIKey,
	}}
	switch {
	case in.ClearAPIKey:
		o.LLM.APIKey = ""
	case in.APIKey != "":
		o.LLM.APIKey = in.APIKey
	}
	return o, nil
}
