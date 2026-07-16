package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadWithoutProviderLeavesAIOff(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("missing file must yield defaults, got: %v", err)
	}
	if cfg.LLM.Provider != "" {
		t.Errorf("provider = %q, want empty (no provider is a supported, AI-off state)", cfg.LLM.Provider)
	}
}

func TestLoadFile(t *testing.T) {
	path := write(t, `
database = "/tmp/kb.db"

[llm]
provider = "openai-compatible"
model = "qwen3"
base_url = "http://localhost:11434/v1"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "/tmp/kb.db" {
		t.Errorf("database = %q", cfg.Database)
	}
	if cfg.LLM.Provider != ProviderOpenAICompat || cfg.LLM.Model != "qwen3" {
		t.Errorf("llm = %+v", cfg.LLM)
	}
}

func TestLoadRejectsUnknownProvider(t *testing.T) {
	if _, err := Load(write(t, "[llm]\nprovider = \"gemini\"\n")); err == nil {
		t.Fatal("unknown provider must be rejected")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	// A typo in the config must fail loudly, not be ignored silently.
	if _, err := Load(write(t, "databse = \"x\"\n")); err == nil {
		t.Fatal("unknown key must be rejected")
	}
}

func TestDatabasePathPrecedence(t *testing.T) {
	t.Setenv("COMIND_DB", "/tmp/env.db")
	p, err := Config{Database: "/tmp/file.db"}.DatabasePath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/tmp/env.db" {
		t.Errorf("env must win: got %q", p)
	}

	t.Setenv("COMIND_DB", "")
	if p, _ = (Config{Database: "/tmp/file.db"}).DatabasePath(); p != "/tmp/file.db" {
		t.Errorf("config field must win over default: got %q", p)
	}
}

func TestAPIKeyPrecedence(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	llm := LLM{Provider: ProviderAnthropic, APIKey: "inline", APIKeyFile: keyFile}

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	if k, _ := llm.APIKeyValue(); k != "from-env" {
		t.Errorf("env must win: got %q", k)
	}

	t.Setenv("ANTHROPIC_API_KEY", "")
	if k, _ := llm.APIKeyValue(); k != "from-file" {
		t.Errorf("key file must win over inline (and be trimmed): got %q", k)
	}

	llm.APIKeyFile = ""
	if k, _ := llm.APIKeyValue(); k != "inline" {
		t.Errorf("inline key is the fallback: got %q", k)
	}
}

func TestAPIKeyEnvPerProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")

	if k, _ := (LLM{Provider: ProviderAnthropic}).APIKeyValue(); k != "anthropic-key" {
		t.Errorf("anthropic provider read %q", k)
	}
	if k, _ := (LLM{Provider: ProviderOpenAICompat}).APIKeyValue(); k != "openai-key" {
		t.Errorf("openai-compatible provider read %q", k)
	}
}
