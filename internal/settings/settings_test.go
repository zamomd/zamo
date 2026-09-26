package settings

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/config"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
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
	require.NoError(t, os.WriteFile(cfgPath, []byte(configTOML), 0o600))
	t.Setenv("ZAMO_CONFIG", cfgPath)

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
	require.NoError(t, err)

	o, err := config.LoadOverrides(setPath)
	require.NoError(t, err)
	assert.Equal(t, ptr("qwen3"), o.LLM.Model)

	require.NoError(t, h.Ping(t.Context()))
	assert.True(t, reached, "the new provider was not reached; Save must swap the live client")

	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.Equal(t, config.ProviderOpenAICompat, v.Provider.Value)
	assert.Equal(t, string(config.OriginGUI), v.Provider.Origin)
}

func TestSaveKeepsKeysFromANewerBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")
	require.NoError(t, os.WriteFile(setPath, []byte("future_mode = false\n[llm]\nmodel = \"old\"\n"), 0o600))

	err := m.Save(t.Context(), In{
		Provider: ptr(config.ProviderOpenAICompat),
		Model:    ptr("qwen3"),
		BaseURL:  ptr(srv.URL + "/v1"),
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(setPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "future_mode", "the save dropped a key this build does not know")
	assert.Contains(t, string(raw), "qwen3", "the save lost the change it was made for")
}

func TestSaveRejectsUnknownProvider(t *testing.T) {
	m, _, setPath := newManager(t, "")
	err := m.Save(t.Context(), In{Provider: ptr("gemini")})
	require.Error(t, err)
	_, statErr := os.Stat(setPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist, "settings.toml was written for an invalid provider")
}

func TestSaveInDeclarativeModeIsRefused(t *testing.T) {
	m, _, setPath := newManager(t, "declarative = true\n")
	err := m.Save(t.Context(), In{Model: ptr("qwen3")})
	require.ErrorIs(t, err, ErrDeclarative)
	_, statErr := os.Stat(setPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist, "settings.toml was written in declarative mode")
}

func TestCurrentNeverExposesTheKey(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\napi_key = \"secret\"\n")
	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.True(t, v.APIKey.Set)
	assert.Equal(t, string(config.OriginConfig), v.APIKey.Origin)
}

func TestSaveResetToConfigSwapsLiveClient(t *testing.T) {
	m, h, _ := newManager(t, "")

	require.NoError(t, m.Save(t.Context(), In{Provider: ptr(config.ProviderAnthropic)}))
	require.NoError(t, m.Save(t.Context(), In{}))

	err := h.Ping(t.Context())
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)

	wantErr := llm.Disabled{}.Ping(t.Context())
	assert.EqualError(t, err, wantErr.Error(), "the disabled client's message, naming the absent provider, not the key")
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

	require.NoError(t, m.Save(t.Context(), In{Model: ptr("from-gui"), BaseURL: ptr("http://127.0.0.1:1/v1")}))
	require.NoError(t, m.Save(t.Context(), In{}))

	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-config", v.Model.Value)
	assert.Equal(t, string(config.OriginConfig), v.Model.Origin)

	require.NoError(t, h.Ping(t.Context()),
		"must fall back to config.toml's base URL, not keep the dropped override's unreachable one")
	assert.True(t, reached,
		"the config-declared server was not reached after reset; the live client kept the stale override's base URL")
}

func TestSaveCanOverrideDeclaredModelWithProviderDefault(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\nprovider = \"anthropic\"\nmodel = \"declared-model\"\n")

	require.NoError(t, m.Save(t.Context(), In{Model: ptr("")}))
	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.Empty(t, v.Model.Value, "want an explicit empty GUI override")
	assert.Equal(t, string(config.OriginGUI), v.Model.Origin)
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
	require.NoError(t, err, "a provider switch must not be blocked by the OTHER provider's env var")

	o, err := config.LoadOverrides(setPath)
	require.NoError(t, err)
	assert.Equal(t, "sk-openai", o.LLM.APIKey)
}

func TestSaveGuardFiresForSubmittedProvidersOwnEnvVar(t *testing.T) {
	m, _, setPath := newManager(t, "")
	t.Setenv("OPENAI_API_KEY", "ambient-openai-key")

	err := m.Save(t.Context(), In{
		Provider: ptr(config.ProviderOpenAICompat),
		BaseURL:  ptr("http://127.0.0.1:1/v1"),
		APIKey:   "sk-typed",
	})
	require.ErrorContains(t, err, "OPENAI_API_KEY",
		"the submitted provider's own env var must block an override and be named so the user knows what to clear")
	_, statErr := os.Stat(setPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist,
		"settings.toml was written even though the guard should have refused the submission")
}

func TestSaveRejectsOpenAICompatWithoutBaseURL(t *testing.T) {
	m, _, setPath := newManager(t, "")

	err := m.Save(t.Context(), In{Provider: ptr(config.ProviderOpenAICompat)})
	require.Error(t, err, "openai-compatible needs a base URL")
	_, statErr := os.Stat(setPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist, "settings.toml was written despite the missing base URL")
}

func TestSaveAllowsOpenAICompatWithBaseURLFromConfig(t *testing.T) {
	configTOML := "[llm]\nprovider = \"openai-compatible\"\nbase_url = \"http://127.0.0.1:1/v1\"\n"
	m, _, setPath := newManager(t, configTOML)

	err := m.Save(t.Context(), In{Provider: ptr(config.ProviderOpenAICompat)})
	require.NoError(t, err, "a base URL declared in config.toml should satisfy the guard")
	_, statErr := os.Stat(setPath)
	assert.NoError(t, statErr, "settings.toml was not written")
}

func TestCheckDoesNotSave(t *testing.T) {
	m, _, setPath := newManager(t, "")
	_ = m.Check(t.Context(), In{
		Provider: ptr(config.ProviderOpenAICompat),
		BaseURL:  ptr("http://127.0.0.1:1/v1"),
	})
	_, err := os.Stat(setPath)
	assert.ErrorIs(t, err, fs.ErrNotExist, "Check wrote settings.toml; it must not save")
}

func TestCurrentReportsTheAIPolicy(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")
	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.False(t, v.AIEnabled.Value)
	assert.Equal(t, string(config.OriginConfig), v.AIEnabled.Origin)
	assert.Equal(t, config.ProviderAnthropic, v.Provider.Value, "want it still reported while AI is off")
}

func TestSaveTurnsAIOffLiveAndKeepsTheProvider(t *testing.T) {
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	require.NoError(t, m.Save(t.Context(), In{AIEnabled: &no}))

	assert.False(t, m.policy.AIEnabled(), "the live policy is still enabled after AI was switched off")

	o, err := config.LoadOverrides(setPath)
	require.NoError(t, err)
	assert.Equal(t, ptr(false), o.AIEnabled)

	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.Equal(t, config.ProviderAnthropic, v.Provider.Value, "want the declared provider kept while AI is off")
}

func TestSaveTurnsAIBackOn(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n\n[llm]\nprovider = \"anthropic\"\n")

	yes := true
	require.NoError(t, m.Save(t.Context(), In{AIEnabled: &yes, Provider: ptr(config.ProviderAnthropic)}))
	assert.True(t, m.policy.AIEnabled(), "the live policy is still disabled after AI was switched back on")
}

func TestCurrentReportsTheStoredSwitchUnderAnEnvOverride(t *testing.T) {
	m, _, setPath := newManager(t, "[llm]\nprovider = \"anthropic\"\n")

	no := false
	require.NoError(t, m.Save(t.Context(), In{AIEnabled: &no, Provider: ptr(config.ProviderAnthropic)}))

	t.Setenv("ZAMO_AI_ENABLED", "true")
	v, err := m.Current(t.Context())
	require.NoError(t, err)
	require.True(t, v.AIEnabled.Value)
	require.Equal(t, string(config.OriginEnv), v.AIEnabled.Origin)
	require.Equal(t, ptr(false), v.AIEnabled.Override, "want the saved false")

	err = m.Save(t.Context(), In{
		AIEnabled: v.AIEnabled.Override,
		Provider:  ptr(config.ProviderAnthropic),
		Model:     ptr("claude-sonnet-5"),
	})
	require.NoError(t, err)
	o, err := config.LoadOverrides(setPath)
	require.NoError(t, err)
	assert.Equal(t, ptr(false), o.AIEnabled, "want it kept at false")
}

func TestCurrentReportsStoredStringOverrideUnderEnvironment(t *testing.T) {
	m, _, _ := newManager(t, "[llm]\nprovider = \"anthropic\"\nmodel = \"config-model\"\n")

	require.NoError(t, m.Save(t.Context(), In{Model: ptr("gui-model")}))
	t.Setenv("ZAMO_LLM_MODEL", "env-model")

	v, err := m.Current(t.Context())
	require.NoError(t, err)
	require.Equal(t, "env-model", v.Model.Value)
	require.Equal(t, string(config.OriginEnv), v.Model.Origin)
	require.Equal(t, ptr("gui-model"), v.Model.Override)
}

func TestCurrentReportsNoStoredSwitchWhenUntouched(t *testing.T) {
	m, _, _ := newManager(t, "ai_enabled = false\n")
	v, err := m.Current(t.Context())
	require.NoError(t, err)
	assert.False(t, v.AIEnabled.Value, "want the declared false")
	assert.Nil(t, v.AIEnabled.Override, "config.toml declared it, not the GUI")
}
