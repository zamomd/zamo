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
	return New(h, setPath), h, setPath
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
