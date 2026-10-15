package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAt(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func ptr[T any](v T) *T { return &v }

func TestResolveOverridesBeatConfig(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", `
[llm]
provider = "anthropic"
model = "claude-opus-4-5"
`)
	setPath := writeAt(t, "settings.toml", `
[llm]
model = "claude-sonnet-4-5"
`)

	r, err := ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-5", r.Config.LLM.Model, "want the GUI override")
	assert.Equal(t, OriginGUI, r.Origins.Model)
	assert.Equal(t, ProviderAnthropic, r.Config.LLM.Provider)
	assert.Equal(t, OriginConfig, r.Origins.Provider)
}

func TestResolveExplicitEmptyOverrideBeatsConfig(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", "[llm]\nmodel = \"declared-model\"\n")
	setPath := writeAt(t, "settings.toml", "[llm]\nmodel = \"\"\n")

	r, err := ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)
	assert.Empty(t, r.Config.LLM.Model, "want an explicit empty GUI override")
	assert.Equal(t, OriginGUI, r.Origins.Model)
}

func TestSchemaEnvironmentName(t *testing.T) {
	assert.Equal(t, "ZAMO_LLM_BASE_URL", environmentName("llm.base-url"))
}

func TestSchemaResolvesLLMEnvironment(t *testing.T) {
	t.Setenv("ZAMO_LLM_PROVIDER", ProviderOpenAICompat)
	t.Setenv("ZAMO_LLM_MODEL", "env-model")
	t.Setenv("ZAMO_LLM_BASE_URL", "http://localhost:11434/v1")

	r, err := ResolveWith(Config{LLM: LLM{Provider: ProviderAnthropic, Model: "config-model"}}, Overrides{
		LLM: LLMOverrides{Model: ptr("gui-model")},
	})
	require.NoError(t, err)
	assert.Equal(t, ProviderOpenAICompat, r.Config.LLM.Provider)
	assert.Equal(t, OriginEnv, r.Origins.Provider)
	assert.Equal(t, "env-model", r.Config.LLM.Model)
	assert.Equal(t, OriginEnv, r.Origins.Model)
	assert.Equal(t, "http://localhost:11434/v1", r.Config.LLM.BaseURL)
	assert.Equal(t, OriginEnv, r.Origins.BaseURL)
}

func TestResolveDeclarativeIgnoresOverrides(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", `
declarative = true

[llm]
model = "claude-opus-4-5"
`)
	setPath := filepath.Join(filepath.Dir(cfgPath), "settings.toml")
	require.NoError(t, os.WriteFile(setPath, []byte("[llm]\nmodel = \"stale\"\n"), 0o600))

	r, err := ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)
	require.True(t, r.Config.Declarative)
	assert.Equal(t, "claude-opus-4-5", r.Config.LLM.Model, "settings.toml must be ignored")
	assert.Equal(t, OriginConfig, r.Origins.Model)
}

func TestResolveMissingOverridesIsNotAnError(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", "")
	r, err := ResolveFrom(cfgPath, filepath.Join(t.TempDir(), "absent.toml"))
	require.NoError(t, err, "missing settings.toml must be fine")
	assert.Empty(t, r.Config.LLM.Provider, "no provider is the supported default")
	assert.Equal(t, OriginDefault, r.Origins.Provider)
}

func TestResolvedAPIKeyPrecedence(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	keyFile := writeAt(t, "key.txt", "from-file\n")
	cfgPath := writeAt(t, "config.toml", `
[llm]
provider = "anthropic"
api_key = "from-config"
api_key_file = "`+keyFile+`"
`)
	setPath := writeAt(t, "settings.toml", `
[llm]
api_key = "from-gui"
`)

	r, err := ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)

	key, err := r.APIKeyValue()
	require.NoError(t, err)
	assert.Equal(t, "from-gui", key)
	assert.Equal(t, OriginGUI, r.Origins.APIKey)

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	r, err = ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)
	key, err = r.APIKeyValue()
	require.NoError(t, err)
	assert.Equal(t, "from-env", key)
	assert.Equal(t, OriginEnv, r.Origins.APIKey)
}

func TestAPIKeyValueIsReadLazily(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	keyFile := writeAt(t, "key.txt", "old\n")
	cfgPath := writeAt(t, "config.toml", "[llm]\napi_key_file = \""+keyFile+"\"\n")

	r, err := ResolveFrom(cfgPath, filepath.Join(t.TempDir(), "absent.toml"))
	require.NoError(t, err)
	key, _ := r.APIKeyValue()
	require.Equal(t, "old", key)

	require.NoError(t, os.WriteFile(keyFile, []byte("new\n"), 0o600))
	key, err = r.APIKeyValue()
	require.NoError(t, err)
	assert.Equal(t, "new", key, "the file must be re-read on every call")
	assert.Equal(t, OriginKeyFile, r.Origins.APIKey)
}

func TestSaveOverridesRoundTripsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	o := Overrides{LLM: LLMOverrides{Model: ptr("qwen3"), APIKey: "secret"}}
	require.NoError(t, SaveOverrides(path, o))

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Windows keeps no Unix mode bits; the profile directory's ACL guards it.
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the file holds an API key")
	}

	got, err := LoadOverrides(path)
	require.NoError(t, err)
	assert.Nil(t, got.AIEnabled)
	assert.Nil(t, got.LLM.Provider)
	assert.Nil(t, got.LLM.BaseURL)
	assert.Equal(t, ptr("qwen3"), got.LLM.Model)
	assert.Equal(t, "secret", got.LLM.APIKey)
}

func TestSaveOverridesOmitsUnsetFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	require.NoError(t, SaveOverrides(path, Overrides{LLM: LLMOverrides{Model: ptr("qwen3")}}))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "provider", "file mentions an unset field")
}

func TestLoadOverridesAcceptsUnknownKeys(t *testing.T) {
	for _, content := range []string{
		"future = true\n[llm]\nmodel = \"qwen3\"\n",
		"[llm]\nmodel = \"qwen3\"\ntemperature = 0.7\n",
		"future = true\n\n[llm]\n",
	} {
		o, err := LoadOverrides(writeAt(t, "settings.toml", content))
		require.NoError(t, err, content)
		if strings.Contains(content, "qwen3") {
			assert.Equal(t, ptr("qwen3"), o.LLM.Model, content)
		}
	}
}

func TestSaveOverridesPreservesUnknownKeys(t *testing.T) {
	path := writeAt(t, "settings.toml", "future = false\n[llm]\nmodel = \"qwen3\"\ntemperature = 0.7\n")
	o, err := LoadOverrides(path)
	require.NoError(t, err)
	o.LLM.Model = ptr("llama4")
	require.NoError(t, SaveOverrides(path, o))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, want := range []string{"future", "false", "temperature", "0.7", "llama4"} {
		assert.Contains(t, string(raw), want)
	}
}

func TestAIEnabledPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		config     string
		settings   string
		want       bool
		wantOrigin Origin
	}{
		{
			name:       "on by default",
			config:     "",
			want:       true,
			wantOrigin: OriginDefault,
		},
		{
			name:       "config turns it off",
			config:     "ai_enabled = false\n",
			want:       false,
			wantOrigin: OriginConfig,
		},
		{
			name:       "config on is still a declared value",
			config:     "ai_enabled = true\n",
			want:       true,
			wantOrigin: OriginConfig,
		},
		{
			name:       "the GUI turns it off with nothing declared",
			config:     "",
			settings:   "ai_enabled = false\n",
			want:       false,
			wantOrigin: OriginGUI,
		},
		{
			name:       "the GUI turns it back on over a declared off",
			config:     "ai_enabled = false\n",
			settings:   "ai_enabled = true\n",
			want:       true,
			wantOrigin: OriginGUI,
		},
		{
			name:       "declarative pins the declared value",
			config:     "declarative = true\nai_enabled = false\n",
			settings:   "ai_enabled = true\n",
			want:       false,
			wantOrigin: OriginConfig,
		},
		{
			name:       "the environment beats every file",
			env:        "false",
			config:     "ai_enabled = true\n",
			settings:   "ai_enabled = true\n",
			want:       false,
			wantOrigin: OriginEnv,
		},
		{
			name:       "the environment turns it on over a declared off",
			env:        "true",
			config:     "ai_enabled = false\n",
			want:       true,
			wantOrigin: OriginEnv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(environmentName("ai_enabled"), tt.env)

			cfgPath := writeAt(t, "config.toml", tt.config)
			setPath := filepath.Join(filepath.Dir(cfgPath), "settings.toml")
			if tt.settings != "" {
				require.NoError(t, os.WriteFile(setPath, []byte(tt.settings), 0o600))
			}

			r, err := ResolveFrom(cfgPath, setPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, r.AIEnabled())
			assert.Equal(t, tt.wantOrigin, r.Origins.AIEnabled)
		})
	}
}

// An unparsable value must not silently read as "off": a typo in the
// environment would disable AI without saying so.
func TestAIEnabledRejectsUnparsableEnv(t *testing.T) {
	aiEnv := environmentName("ai_enabled")
	t.Setenv(aiEnv, "yes-please")

	cfgPath := writeAt(t, "config.toml", "")
	_, err := ResolveFrom(cfgPath, filepath.Join(filepath.Dir(cfgPath), "settings.toml"))
	require.ErrorContains(t, err, aiEnv)
}

// Turning AI off must not cost the user their provider configuration.
func TestAIDisabledKeepsProviderConfig(t *testing.T) {
	t.Setenv(environmentName("ai_enabled"), "")

	cfgPath := writeAt(t, "config.toml", `
ai_enabled = false

[llm]
provider = "openai-compatible"
model = "qwen2.5:3b"
base_url = "http://localhost:11434/v1"
`)

	r, err := ResolveFrom(cfgPath, filepath.Join(filepath.Dir(cfgPath), "settings.toml"))
	require.NoError(t, err)
	require.False(t, r.AIEnabled())
	assert.Equal(t, ProviderOpenAICompat, r.Config.LLM.Provider)
	assert.Equal(t, "qwen2.5:3b", r.Config.LLM.Model)
	assert.Equal(t, "http://localhost:11434/v1", r.Config.LLM.BaseURL)
}

func TestNoProviderIgnoresEnvAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	t.Setenv("OPENAI_API_KEY", "")

	cfgPath := writeAt(t, "config.toml", "")
	setPath := filepath.Join(filepath.Dir(cfgPath), "settings.toml")

	r, err := ResolveFrom(cfgPath, setPath)
	require.NoError(t, err)

	assert.Empty(t, r.Config.LLM.Provider)
	assert.NotEqual(t, OriginEnv, r.Origins.APIKey, "must not be env-locked when provider is empty")
	assert.False(t, r.APIKeySet(), "no provider means no key is set")
	key, _ := r.APIKeyValue()
	assert.Empty(t, key)
}
