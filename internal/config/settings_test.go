package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAt puts content at a named file inside a fresh temp dir.
func writeAt(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

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
	// An untouched field keeps the declared value and reports where it came from.
	if r.Config.LLM.Provider != ProviderAnthropic {
		t.Errorf("provider = %q, want anthropic", r.Config.LLM.Provider)
	}
	if r.Origins.Provider != OriginConfig {
		t.Errorf("provider origin = %q, want %q", r.Origins.Provider, OriginConfig)
	}
}

func TestResolveDeclarativeIgnoresOverrides(t *testing.T) {
	cfgPath := writeAt(t, "config.toml", `
declarative = true

[llm]
model = "claude-opus-4-5"
`)
	// A settings.toml left over from before the flag was set must not leak back
	// in: that is the ghost the two-layer split exists to prevent.
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

	// The GUI layer beats api_key_file and the inline config key.
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

	// The environment beats everything, and the field becomes read-only.
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

	// Regression for sops-nix rotation: the file is rewritten under a symlink
	// that flips on activation, so the value must be read per call.
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
	o := Overrides{LLM: LLMOverrides{Model: "qwen3", APIKey: "secret"}}
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
	if got != o {
		t.Errorf("round trip = %+v, want %+v", got, o)
	}
}

func TestSaveOverridesOmitsUnsetFields(t *testing.T) {
	// An absent field means "not overridden", which is what makes "reset to
	// config" expressible; an empty string would be a real override to "".
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := SaveOverrides(path, Overrides{LLM: LLMOverrides{Model: "qwen3"}}); err != nil {
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
