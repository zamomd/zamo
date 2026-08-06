package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAt(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.LLM.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want the GUI override", r.Config.LLM.Model)
	}
	if r.Origins.Model != OriginGUI {
		t.Errorf("model origin = %q, want %q", r.Origins.Model, OriginGUI)
	}
	if r.Config.LLM.Provider != ProviderAnthropic {
		t.Errorf("provider = %q, want anthropic", r.Config.LLM.Provider)
	}
	if r.Origins.Provider != OriginConfig {
		t.Errorf("provider origin = %q, want %q", r.Origins.Provider, OriginConfig)
	}
}

func TestResolveExplicitEmptyOverrideBeatsConfig(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", "[llm]\nmodel = \"declared-model\"\n")
	setPath := writeAt(t, "settings.toml", "[llm]\nmodel = \"\"\n")

	r, err := ResolveFrom(cfgPath, setPath)
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.LLM.Model != "" || r.Origins.Model != OriginGUI {
		t.Errorf("model = %q from %q, want an explicit empty GUI override", r.Config.LLM.Model, r.Origins.Model)
	}
}

func TestSchemaEnvironmentName(t *testing.T) {
	if got := environmentName("llm.base-url"); got != "COMIND_LLM_BASE_URL" {
		t.Errorf("envName = %q, want COMIND_LLM_BASE_URL", got)
	}
}

func TestSchemaResolvesLLMEnvironment(t *testing.T) {
	t.Setenv("COMIND_LLM_PROVIDER", ProviderOpenAICompat)
	t.Setenv("COMIND_LLM_MODEL", "env-model")
	t.Setenv("COMIND_LLM_BASE_URL", "http://localhost:11434/v1")

	r, err := ResolveWith(Config{LLM: LLM{Provider: ProviderAnthropic, Model: "config-model"}}, Overrides{
		LLM: LLMOverrides{Model: ptr("gui-model")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.LLM.Provider != ProviderOpenAICompat || r.Origins.Provider != OriginEnv {
		t.Errorf("provider = %q from %q", r.Config.LLM.Provider, r.Origins.Provider)
	}
	if r.Config.LLM.Model != "env-model" || r.Origins.Model != OriginEnv {
		t.Errorf("model = %q from %q", r.Config.LLM.Model, r.Origins.Model)
	}
	if r.Config.LLM.BaseURL != "http://localhost:11434/v1" || r.Origins.BaseURL != OriginEnv {
		t.Errorf("base URL = %q from %q", r.Config.LLM.BaseURL, r.Origins.BaseURL)
	}
}

func TestResolveDeclarativeIgnoresOverrides(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", `
declarative = true

[llm]
model = "claude-opus-4-5"
`)
	setPath := filepath.Join(filepath.Dir(cfgPath), "settings.toml")
	if err := os.WriteFile(setPath, []byte("[llm]\nmodel = \"stale\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := ResolveFrom(cfgPath, setPath)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Config.Declarative {
		t.Fatal("Declarative = false, want true")
	}
	if r.Config.LLM.Model != "claude-opus-4-5" {
		t.Errorf("model = %q, want the declared value (settings.toml must be ignored)", r.Config.LLM.Model)
	}
	if r.Origins.Model != OriginConfig {
		t.Errorf("model origin = %q, want %q", r.Origins.Model, OriginConfig)
	}
}

func TestResolveMissingOverridesIsNotAnError(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", "")
	r, err := ResolveFrom(cfgPath, filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("missing settings.toml must be fine, got: %v", err)
	}
	if r.Config.LLM.Provider != "" {
		t.Errorf("provider = %q, want empty (no provider is the supported default)", r.Config.LLM.Provider)
	}
	if r.Origins.Provider != OriginDefault {
		t.Errorf("provider origin = %q, want %q", r.Origins.Provider, OriginDefault)
	}
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
	if err != nil {
		t.Fatal(err)
	}

	key, err := r.APIKeyValue()
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-gui" {
		t.Errorf("key = %q, want from-gui", key)
	}
	if r.Origins.APIKey != OriginGUI {
		t.Errorf("key origin = %q, want %q", r.Origins.APIKey, OriginGUI)
	}

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	r, err = ResolveFrom(cfgPath, setPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err = r.APIKeyValue()
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-env" {
		t.Errorf("key = %q, want from-env", key)
	}
	if r.Origins.APIKey != OriginEnv {
		t.Errorf("key origin = %q, want %q", r.Origins.APIKey, OriginEnv)
	}
}

func TestAPIKeyValueIsReadLazily(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	keyFile := writeAt(t, "key.txt", "old\n")
	cfgPath := writeAt(t, "config.toml", "[llm]\napi_key_file = \""+keyFile+"\"\n")

	r, err := ResolveFrom(cfgPath, filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if key, _ := r.APIKeyValue(); key != "old" {
		t.Fatalf("key = %q, want old", key)
	}

	if err := os.WriteFile(keyFile, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := r.APIKeyValue()
	if err != nil {
		t.Fatal(err)
	}
	if key != "new" {
		t.Errorf("key = %q, want new (the file must be re-read on every call)", key)
	}
	if r.Origins.APIKey != OriginKeyFile {
		t.Errorf("key origin = %q, want %q", r.Origins.APIKey, OriginKeyFile)
	}
}

func TestSaveOverridesRoundTripsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	o := Overrides{LLM: LLMOverrides{Model: ptr("qwen3"), APIKey: "secret"}}
	if err := SaveOverrides(path, o); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600 (the file holds an API key)", perm)
	}

	got, err := LoadOverrides(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AIEnabled != nil || got.LLM.Provider != nil || got.LLM.BaseURL != nil ||
		got.LLM.Model == nil || *got.LLM.Model != "qwen3" || got.LLM.APIKey != "secret" {
		t.Errorf("round trip = %+v, want %+v", got, o)
	}
}

func TestSaveOverridesOmitsUnsetFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := SaveOverrides(path, Overrides{LLM: LLMOverrides{Model: ptr("qwen3")}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "provider") {
		t.Errorf("file mentions an unset field:\n%s", raw)
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
				if err := os.WriteFile(setPath, []byte(tt.settings), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			r, err := ResolveFrom(cfgPath, setPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.AIEnabled(); got != tt.want {
				t.Errorf("AIEnabled() = %v, want %v", got, tt.want)
			}
			if r.Origins.AIEnabled != tt.wantOrigin {
				t.Errorf("origin = %q, want %q", r.Origins.AIEnabled, tt.wantOrigin)
			}
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
	if err == nil {
		t.Fatal("resolve succeeded, want an error naming the variable")
	}
	if !strings.Contains(err.Error(), aiEnv) {
		t.Errorf("error %q does not name %s", err, aiEnv)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if r.AIEnabled() {
		t.Fatal("AIEnabled() = true, want false")
	}
	if r.Config.LLM.Provider != ProviderOpenAICompat {
		t.Errorf("provider = %q, want it preserved while AI is off", r.Config.LLM.Provider)
	}
	if r.Config.LLM.Model != "qwen2.5:3b" {
		t.Errorf("model = %q, want it preserved while AI is off", r.Config.LLM.Model)
	}
	if r.Config.LLM.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("base URL = %q, want it preserved while AI is off", r.Config.LLM.BaseURL)
	}
}

func TestNoProviderIgnoresEnvAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	t.Setenv("OPENAI_API_KEY", "")

	cfgPath := writeAt(t, "config.toml", "")
	setPath := filepath.Join(filepath.Dir(cfgPath), "settings.toml")

	r, err := ResolveFrom(cfgPath, setPath)
	if err != nil {
		t.Fatal(err)
	}

	if r.Config.LLM.Provider != "" {
		t.Errorf("provider = %q, want empty (AI off)", r.Config.LLM.Provider)
	}
	if r.Origins.APIKey == OriginEnv {
		t.Errorf("key origin = OriginEnv, must not be env-locked when provider is empty")
	}
	if r.APIKeySet() {
		t.Errorf("APIKeySet = true, want false (no provider means no key is set)")
	}
	if key, _ := r.APIKeyValue(); key != "" {
		t.Errorf("key value = %q, want empty when provider is empty", key)
	}
}
