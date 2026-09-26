// Package settings is the use case for the provider settings screen: read the
// effective configuration, save the mutable layer, swap the live client.
//
// It sits outside knowledge on purpose: the domain only ever sees an
// LLMClient and must not learn about providers, TOML or paths.
package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/zamomd/zamo/internal/config"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
	"github.com/zamomd/zamo/internal/llm/anthropic"
	"github.com/zamomd/zamo/internal/llm/openaicompat"
)

// ErrDeclarative is returned when config.toml manages the settings and the
// mutable layer is off. It is enforced here rather than in the UI: the screen
// is not the only caller.
var ErrDeclarative = errors.New("settings are managed by config.toml")

// Field describes a resolved setting and its mutable override.
type Field[T any] struct {
	Value    T      `json:"value"`
	Origin   string `json:"origin"`
	Override *T     `json:"override"`
}

// SecretField describes a resolved secret without exposing its value.
type SecretField struct {
	Set    bool   `json:"set"`
	Origin string `json:"origin"`
}

// View is what the settings screen renders. It carries no key value in any
// form — only whether one is set and where it comes from.
type View struct {
	AIEnabled Field[bool]   `json:"aiEnabled"`
	Provider  Field[string] `json:"provider"`
	Model     Field[string] `json:"model"`
	BaseURL   Field[string] `json:"baseUrl"`
	APIKey    SecretField   `json:"apiKey"`
	// Declarative means every field is read-only: config.toml owns them.
	Declarative bool `json:"declarative"`
}

// In is a settings submission. A nil field means "not overridden" and is
// dropped from the mutable layer, which is how "reset to config" is expressed.
// A non-nil empty string is an explicit empty override. An empty APIKey leaves
// the stored key alone; ClearAPIKey removes it.
type In struct {
	// AIEnabled is nil when the mutable override must be removed.
	AIEnabled   *bool   `json:"aiEnabled"`
	Provider    *string `json:"provider"`
	Model       *string `json:"model"`
	BaseURL     *string `json:"baseUrl"`
	APIKey      string  `json:"apiKey"`
	ClearAPIKey bool    `json:"clearApiKey"`
}

// Manager owns the settings use case.
type Manager struct {
	holder *llm.Holder
	policy *Policy
	path   string
}

// New returns a manager backed by path. Policy may be nil.
func New(holder *llm.Holder, policy *Policy, path string) *Manager {
	return &Manager{holder: holder, policy: policy, path: path}
}

// BuildClient creates the configured provider client.
// An empty provider returns llm.Disabled.
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
		AIEnabled: Field[bool]{
			Value: r.AIEnabled(), Origin: string(r.Origins.AIEnabled), Override: r.Overrides.AIEnabled,
		},
		Provider: Field[string]{
			Value: r.Config.LLM.Provider, Origin: string(r.Origins.Provider), Override: r.Overrides.LLM.Provider,
		},
		Model: Field[string]{
			Value: r.Config.LLM.Model, Origin: string(r.Origins.Model), Override: r.Overrides.LLM.Model,
		},
		BaseURL: Field[string]{
			Value: r.Config.LLM.BaseURL, Origin: string(r.Origins.BaseURL), Override: r.Overrides.LLM.BaseURL,
		},
		APIKey: SecretField{
			Set: r.APIKeySet(), Origin: string(r.Origins.APIKey),
		},
		Declarative: r.Config.Declarative,
	}, nil
}

// Save validates and persists mutable settings, then updates runtime state.
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
	if resolved.Config.LLM.Provider == config.ProviderOpenAICompat && resolved.Config.LLM.BaseURL == "" {
		return errors.New("openai-compatible requires a base URL")
	}
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
	if m.policy != nil {
		m.policy.Set(resolved.AIEnabled())
	}
	return nil
}

// Check tests an unsaved submission without changing runtime state or files.
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
	if !resolved.AIEnabled() {
		return knowledge.ErrAIDisabled
	}
	client, err := BuildClient(resolved)
	if err != nil {
		return err
	}
	return client.Ping(ctx)
}

// overrides converts a submission to the mutable configuration layer.
func (m *Manager) overrides(current config.Resolved, in In) (config.Overrides, error) {
	if in.Provider != nil {
		if err := config.ValidateProvider(*in.Provider); err != nil {
			return config.Overrides{}, err
		}
	}

	o := current.Overrides
	o.AIEnabled = in.AIEnabled
	o.LLM = config.LLMOverrides{
		Provider: in.Provider,
		Model:    in.Model,
		BaseURL:  in.BaseURL,
		APIKey:   current.Overrides.LLM.APIKey,
	}
	switch {
	case in.ClearAPIKey:
		o.LLM.APIKey = ""
	case in.APIKey != "":
		o.LLM.APIKey = in.APIKey
	}
	return o, nil
}
