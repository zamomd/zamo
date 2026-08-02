package settings

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/config"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

type stubClient struct{}

func (stubClient) GenerateTree(_ context.Context, _, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) GenerateEdit(_ context.Context, _ []knowledge.Block, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) Ping(_ context.Context) error { return nil }

func newManager(t *testing.T, configTOML string) (*Manager, *llm.Holder, string) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(configTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMIND_CONFIG", cfgPath)

	setPath := filepath.Join(dir, "settings.toml")
	h := llm.NewHolder(stubClient{})
	return New(h, NewSwitch(true), setPath), h, setPath
}

func TestSaveWritesOverridesAndSwapsClient(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	m, h, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	err := m.Save(t.Context(), In{
		Provider: config.ProviderOpenAICompat,
		Model:    "qwen3",
		BaseURL:  srv.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	o, err := config.LoadOverrides(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.LLM.Model != "qwen3" {
		t.Errorf("saved model = %q, want qwen3", o.LLM.Model)
	}

	if err := h.Ping(t.Context()); err != nil {
		t.Fatalf("Ping through the holder = %v, want nil", err)
	}
	if !reached {
		t.Error("the new provider was not reached; Save must swap the live client")
	}

	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Provider.Value != config.ProviderOpenAICompat || v.Provider.Origin != string(config.OriginGUI) {
		t.Errorf("provider = %+v, want openai-compatible from gui", v.Provider)
	}
}

func TestSaveRejectsUnknownProvider(t *testing.T) {
	m, _, setPath := newManager(t, "")
	err := m.Save(t.Context(), In{Provider: "gemini"})
	if err == nil {
		t.Fatal("Save = nil, want an error for an unknown provider")
	}
	if _, statErr := os.Stat(setPath); !os.IsNotExist(statErr) {
		t.Error("settings.toml was written for an invalid provider")
	}
}

func TestSaveInDeclarativeModeIsRefused(t *testing.T) {
	m, _, setPath := newManager(t, "declarative = true\n")
	err := m.Save(t.Context(), In{Model: "qwen3"})
	if !errors.Is(err, ErrDeclarative) {
		t.Fatalf("error = %v, want ErrDeclarative", err)
	}
	if _, statErr := os.Stat(setPath); !os.IsNotExist(statErr) {
		t.Error("settings.toml was written in declarative mode")
	}
}

func TestCurrentNeverExposesTheKey(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\napi_key = \"secret\"\n")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !v.APIKeySet {
		t.Error("APIKeySet = false, want true")
	}
	if v.APIKeyOrigin != string(config.OriginConfig) {
		t.Errorf("key origin = %q, want config", v.APIKeyOrigin)
	}
}

func TestSaveResetToConfigSwapsLiveClient(t *testing.T) {
	m, h, _ := newManager(t, "")

	if err := m.Save(t.Context(), In{Provider: config.ProviderAnthropic}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(t.Context(), In{}); err != nil {
		t.Fatal(err)
	}

	err := h.Ping(t.Context())
	if !errors.Is(err, knowledge.ErrLLMUnavailable) {
		t.Fatalf("Ping error = %v, want ErrLLMUnavailable", err)
	}

	wantErr := llm.Disabled{}.Ping(t.Context())
	if err.Error() != wantErr.Error() {
		t.Errorf("Ping error = %q, want %q (the disabled client's message, naming the absent provider, not the key)",
			err.Error(), wantErr.Error())
	}
}

func TestSaveResetFallsBackToConfigNotOverride(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	configTOML := "[llm]\nprovider = \"openai-compatible\"\nmodel = \"from-config\"\nbase_url = \"" + srv.URL + "/v1\"\n"
	m, h, _ := newManager(t, configTOML)

	if err := m.Save(t.Context(), In{Model: "from-gui", BaseURL: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(t.Context(), In{}); err != nil {
		t.Fatal(err)
	}

	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Value != "from-config" || v.Model.Origin != string(config.OriginConfig) {
		t.Errorf("model = %+v, want {from-config config}", v.Model)
	}

	if err := h.Ping(t.Context()); err != nil {
		t.Fatalf("Ping through the holder after reset = %v, want nil\n"+
			"(must fall back to config.toml's base URL, not keep the dropped override's unreachable one)", err)
	}
	if !reached {
		t.Error("the config-declared server was not reached after reset; the live client kept the stale override's base URL")
	}
}

func TestSaveProviderSwitchNotBlockedByOtherProvidersEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-anthropic-key")
	t.Setenv("OPENAI_API_KEY", "")
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")
	t.Setenv("ANTHROPIC_API_KEY", "ambient-anthropic-key")

	err := m.Save(t.Context(), In{
		Provider: config.ProviderOpenAICompat,
		BaseURL:  "http://127.0.0.1:1/v1",
		APIKey:   "sk-openai",
	})
	if err != nil {
		t.Fatalf("Save = %v, want nil; a provider switch must not be blocked by the OTHER provider's env var", err)
	}

	o, err := config.LoadOverrides(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.LLM.APIKey != "sk-openai" {
		t.Errorf("saved api key = %q, want sk-openai", o.LLM.APIKey)
	}
}

func TestSaveGuardFiresForSubmittedProvidersOwnEnvVar(t *testing.T) {
	m, _, setPath := newManager(t, "")
	t.Setenv("OPENAI_API_KEY", "ambient-openai-key")

	err := m.Save(t.Context(), In{
		Provider: config.ProviderOpenAICompat,
		BaseURL:  "http://127.0.0.1:1/v1",
		APIKey:   "sk-typed",
	})
	if err == nil {
		t.Fatal("Save = nil, want an error: the submitted provider's own env var must block an override")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error = %q, want it to name OPENAI_API_KEY so the user knows what to clear", err.Error())
	}
	if _, statErr := os.Stat(setPath); !os.IsNotExist(statErr) {
		t.Error("settings.toml was written even though the guard should have refused the submission")
	}
}

func TestSaveRejectsOpenAICompatWithoutBaseURL(t *testing.T) {
	m, _, setPath := newManager(t, "")

	err := m.Save(t.Context(), In{Provider: config.ProviderOpenAICompat})
	if err == nil {
		t.Fatal("Save = nil, want an error: openai-compatible needs a base URL")
	}
	if _, statErr := os.Stat(setPath); !os.IsNotExist(statErr) {
		t.Error("settings.toml was written despite the missing base URL")
	}
}

func TestSaveAllowsOpenAICompatWithBaseURLFromConfig(t *testing.T) {
	configTOML := "[llm]\nprovider = \"openai-compatible\"\nbase_url = \"http://127.0.0.1:1/v1\"\n"
	m, _, setPath := newManager(t, configTOML)

	err := m.Save(t.Context(), In{Provider: config.ProviderOpenAICompat})
	if err != nil {
		t.Fatalf("Save = %v, want nil: a base URL declared in config.toml should satisfy the guard", err)
	}
	if _, statErr := os.Stat(setPath); statErr != nil {
		t.Errorf("settings.toml was not written: %v", statErr)
	}
}

func TestCheckDoesNotSave(t *testing.T) {
	m, _, setPath := newManager(t, "")
	_ = m.Check(t.Context(), In{
		Provider: config.ProviderOpenAICompat,
		BaseURL:  "http://127.0.0.1:1/v1",
	})
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("Check wrote settings.toml; it must not save")
	}
}

// off reports whether the client refuses to reach a model because AI is
// switched off, as opposed to any other unavailability.
func off(t *testing.T, c knowledge.LLMClient) bool {
	t.Helper()
	_, err := c.GenerateTree(t.Context(), "topic", "")
	return errors.Is(err, knowledge.ErrAIDisabled)
}

func TestBuildClientRefusesWhileAIIsOff(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantOff bool
	}{
		{
			name:    "anthropic stays configured but unreachable",
			config:  "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n",
			wantOff: true,
		},
		{
			name:    "openai-compatible stays configured but unreachable",
			config:  "ai_enabled = false\n\n[llm]\nprovider = \"openai-compatible\"\nbase_url = \"http://localhost:11434/v1\"\n",
			wantOff: true,
		},
		{
			name:    "no provider is a different state, not this one",
			config:  "",
			wantOff: false,
		},
		{
			name:    "AI on with a provider builds a real client",
			config:  "ai_enabled = true\n\n[llm]\nprovider = \"anthropic\"\n",
			wantOff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("COMIND_AI_ENABLED", "")

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(cfgPath, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			r, err := config.ResolveFrom(cfgPath, filepath.Join(dir, "settings.toml"))
			if err != nil {
				t.Fatal(err)
			}
			c, err := BuildClient(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := off(t, c); got != tt.wantOff {
				t.Errorf("client refuses with ErrAIDisabled = %v, want %v", got, tt.wantOff)
			}
		})
	}
}

func TestCurrentReportsTheAIPolicy(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.AIEnabled {
		t.Error("AIEnabled = true, want false")
	}
	if v.AIEnabledOrigin != string(config.OriginConfig) {
		t.Errorf("origin = %q, want config", v.AIEnabledOrigin)
	}
	if v.Provider.Value != config.ProviderAnthropic {
		t.Errorf("provider = %q, want it still reported while AI is off", v.Provider.Value)
	}
}

func TestSaveTurnsAIOffLiveAndKeepsTheProvider(t *testing.T) {
	m, h, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	if err := m.Save(t.Context(), In{AIEnabled: &no}); err != nil {
		t.Fatal(err)
	}

	if !off(t, h) {
		t.Error("the live client still reaches a model after AI was switched off")
	}

	o, err := config.LoadOverrides(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.AIEnabled == nil || *o.AIEnabled {
		t.Errorf("saved ai_enabled = %v, want false", o.AIEnabled)
	}

	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Provider.Value != config.ProviderAnthropic {
		t.Errorf("provider = %q, want the declared provider kept while AI is off", v.Provider.Value)
	}
}

func TestSaveTurnsAIBackOn(t *testing.T) {
	m, h, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")

	yes := true
	if err := m.Save(t.Context(), In{AIEnabled: &yes, Provider: config.ProviderAnthropic}); err != nil {
		t.Fatal(err)
	}
	if off(t, h) {
		t.Error("the live client still refuses after AI was switched back on")
	}
}

// The winning layer and the stored override are different questions. Once the
// environment wins, the screen must still know what settings.toml holds, or a
// save of an unrelated field submits "not overridden" and silently erases the
// owner's decision — which comes back as AI switching itself on the day the
// variable goes away.
func TestCurrentReportsTheStoredSwitchUnderAnEnvOverride(t *testing.T) {
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	if err := m.Save(t.Context(), In{AIEnabled: &no, Provider: config.ProviderAnthropic}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COMIND_AI_ENABLED", "true")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !v.AIEnabled || v.AIEnabledOrigin != string(config.OriginEnv) {
		t.Fatalf("effective = %v from %q, want true from env", v.AIEnabled, v.AIEnabledOrigin)
	}
	if v.AIEnabledOverride == nil || *v.AIEnabledOverride {
		t.Fatalf("stored override = %v, want the saved false", v.AIEnabledOverride)
	}

	// Resubmitting what the screen was told keeps the decision on disk.
	err = m.Save(t.Context(), In{
		AIEnabled: v.AIEnabledOverride,
		Provider:  config.ProviderAnthropic,
		Model:     "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := config.LoadOverrides(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.AIEnabled == nil || *o.AIEnabled {
		t.Errorf("stored ai_enabled = %v, want it kept at false", o.AIEnabled)
	}
}

func TestCurrentReportsNoStoredSwitchWhenUntouched(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.AIEnabled {
		t.Error("effective = true, want the declared false")
	}
	if v.AIEnabledOverride != nil {
		t.Errorf("stored override = %v, want nil — config.toml declared it, not the GUI", v.AIEnabledOverride)
	}
}
