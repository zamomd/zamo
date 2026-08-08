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

func ptr[T any](v T) *T { return &v }

func (stubClient) GenerateTree(_ context.Context, _, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) GenerateEdit(_ context.Context, _ []knowledge.Block, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) GenerateDocument(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (stubClient) GenerateDocumentEdit(_ context.Context, _ knowledge.Block, _ string) (string, error) {
	return "", nil
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
	return New(h, NewPolicy(true), setPath), h, setPath
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
		Provider: ptr(config.ProviderOpenAICompat),
		Model:    ptr("qwen3"),
		BaseURL:  ptr(srv.URL + "/v1"),
	})
	if err != nil {
		t.Fatal(err)
	}

	o, err := config.LoadOverrides(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if o.LLM.Model == nil || *o.LLM.Model != "qwen3" {
		t.Errorf("saved model = %v, want qwen3", o.LLM.Model)
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

func TestSaveKeepsKeysFromANewerBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")
	if err := os.WriteFile(setPath, []byte("future_mode = false\n[llm]\nmodel = \"old\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := m.Save(t.Context(), In{
		Provider: ptr(config.ProviderOpenAICompat),
		Model:    ptr("qwen3"),
		BaseURL:  ptr(srv.URL + "/v1"),
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(setPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "future_mode") {
		t.Errorf("the save dropped a key this build does not know:\n%s", raw)
	}
	if !strings.Contains(string(raw), "qwen3") {
		t.Errorf("the save lost the change it was made for:\n%s", raw)
	}
}

func TestSaveRejectsUnknownProvider(t *testing.T) {
	m, _, setPath := newManager(t, "")
	err := m.Save(t.Context(), In{Provider: ptr("gemini")})
	if err == nil {
		t.Fatal("Save = nil, want an error for an unknown provider")
	}
	if _, statErr := os.Stat(setPath); !os.IsNotExist(statErr) {
		t.Error("settings.toml was written for an invalid provider")
	}
}

func TestSaveInDeclarativeModeIsRefused(t *testing.T) {
	m, _, setPath := newManager(t, "declarative = true\n")
	err := m.Save(t.Context(), In{Model: ptr("qwen3")})
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
	if !v.APIKey.Set {
		t.Error("APIKeySet = false, want true")
	}
	if v.APIKey.Origin != string(config.OriginConfig) {
		t.Errorf("key origin = %q, want config", v.APIKey.Origin)
	}
}

func TestSaveResetToConfigSwapsLiveClient(t *testing.T) {
	m, h, _ := newManager(t, "")

	if err := m.Save(t.Context(), In{Provider: ptr(config.ProviderAnthropic)}); err != nil {
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

	if err := m.Save(t.Context(), In{Model: ptr("from-gui"), BaseURL: ptr("http://127.0.0.1:1/v1")}); err != nil {
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

func TestSaveCanOverrideDeclaredModelWithProviderDefault(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\nprovider = \"anthropic\"\nmodel = \"declared-model\"\n")

	if err := m.Save(t.Context(), In{Model: ptr("")}); err != nil {
		t.Fatal(err)
	}
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Value != "" || v.Model.Origin != string(config.OriginGUI) {
		t.Errorf("model = %+v, want an explicit empty GUI override", v.Model)
	}
}

func TestSaveProviderSwitchNotBlockedByOtherProvidersEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-anthropic-key")
	t.Setenv("OPENAI_API_KEY", "")
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")
	t.Setenv("ANTHROPIC_API_KEY", "ambient-anthropic-key")

	err := m.Save(t.Context(), In{
		Provider: ptr(config.ProviderOpenAICompat),
		BaseURL:  ptr("http://127.0.0.1:1/v1"),
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
		Provider: ptr(config.ProviderOpenAICompat),
		BaseURL:  ptr("http://127.0.0.1:1/v1"),
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

	err := m.Save(t.Context(), In{Provider: ptr(config.ProviderOpenAICompat)})
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

	err := m.Save(t.Context(), In{Provider: ptr(config.ProviderOpenAICompat)})
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
		Provider: ptr(config.ProviderOpenAICompat),
		BaseURL:  ptr("http://127.0.0.1:1/v1"),
	})
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("Check wrote settings.toml; it must not save")
	}
}

func TestCurrentReportsTheAIPolicy(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.AIEnabled.Value {
		t.Error("AIEnabled = true, want false")
	}
	if v.AIEnabled.Origin != string(config.OriginConfig) {
		t.Errorf("origin = %q, want config", v.AIEnabled.Origin)
	}
	if v.Provider.Value != config.ProviderAnthropic {
		t.Errorf("provider = %q, want it still reported while AI is off", v.Provider.Value)
	}
}

func TestSaveTurnsAIOffLiveAndKeepsTheProvider(t *testing.T) {
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	if err := m.Save(t.Context(), In{AIEnabled: &no}); err != nil {
		t.Fatal(err)
	}

	if m.policy.AIEnabled() {
		t.Error("the live policy is still enabled after AI was switched off")
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
	m, _, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")

	yes := true
	if err := m.Save(t.Context(), In{AIEnabled: &yes, Provider: ptr(config.ProviderAnthropic)}); err != nil {
		t.Fatal(err)
	}
	if !m.policy.AIEnabled() {
		t.Error("the live policy is still disabled after AI was switched back on")
	}
}

func TestCurrentReportsTheStoredSwitchUnderAnEnvOverride(t *testing.T) {
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	if err := m.Save(t.Context(), In{AIEnabled: &no, Provider: ptr(config.ProviderAnthropic)}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COMIND_AI_ENABLED", "true")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !v.AIEnabled.Value || v.AIEnabled.Origin != string(config.OriginEnv) {
		t.Fatalf("effective = %v from %q, want true from env", v.AIEnabled.Value, v.AIEnabled.Origin)
	}
	if v.AIEnabled.Override == nil || *v.AIEnabled.Override {
		t.Fatalf("stored override = %v, want the saved false", v.AIEnabled.Override)
	}

	err = m.Save(t.Context(), In{
		AIEnabled: v.AIEnabled.Override,
		Provider:  ptr(config.ProviderAnthropic),
		Model:     ptr("claude-sonnet-5"),
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

func TestCurrentReportsStoredStringOverrideUnderEnvironment(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\nprovider = \"anthropic\"\nmodel = \"config-model\"\n")

	if err := m.Save(t.Context(), In{Model: ptr("gui-model")}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMIND_LLM_MODEL", "env-model")

	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Value != "env-model" || v.Model.Origin != string(config.OriginEnv) {
		t.Fatalf("effective model = %q from %q", v.Model.Value, v.Model.Origin)
	}
	if v.Model.Override == nil || *v.Model.Override != "gui-model" {
		t.Fatalf("stored model override = %v, want gui-model", v.Model.Override)
	}
}

func TestCurrentReportsNoStoredSwitchWhenUntouched(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n")
	v, err := m.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.AIEnabled.Value {
		t.Error("effective = true, want the declared false")
	}
	if v.AIEnabled.Override != nil {
		t.Errorf("stored override = %v, want nil — config.toml declared it, not the GUI", v.AIEnabled.Override)
	}
}
