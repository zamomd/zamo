package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadWithoutProviderLeavesAIOff(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	require.NoError(t, err, "missing file must yield defaults")
	assert.Empty(t, cfg.LLM.Provider, "no provider is a supported state")
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
	require.NoError(t, err)
	assert.Equal(t, "/tmp/kb.db", cfg.Database)
	assert.Equal(t, ProviderOpenAICompat, cfg.LLM.Provider)
	assert.Equal(t, "qwen3", cfg.LLM.Model)
}

func TestLoadRejectsUnknownProvider(t *testing.T) {
	_, err := Load(write(t, "[llm]\nprovider = \"gemini\"\n"))
	require.Error(t, err, "unknown provider must be rejected")
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	_, err := Load(write(t, "databse = \"x\"\n"))
	require.Error(t, err, "unknown key must be rejected")
}

func TestDatabasePathPrecedence(t *testing.T) {
	t.Setenv("ZAMO_DB", "/tmp/env.db")
	r, err := ResolveWith(Config{Database: "/tmp/file.db"}, Overrides{})
	require.NoError(t, err)
	p, err := r.Config.DatabasePath()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/env.db", p, "env must win")

	t.Setenv("ZAMO_DB", "")
	r, err = ResolveWith(Config{Database: "/tmp/file.db"}, Overrides{})
	require.NoError(t, err)
	p, _ = r.Config.DatabasePath()
	assert.Equal(t, "/tmp/file.db", p, "config field must win over default")
}

func TestAPIKeyPrecedence(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(keyFile, []byte("from-file\n"), 0o600))
	llm := LLM{Provider: ProviderAnthropic, APIKey: "inline", APIKeyFile: keyFile}

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	r, err := ResolveWith(Config{LLM: llm}, Overrides{})
	require.NoError(t, err)
	k, _ := r.APIKeyValue()
	assert.Equal(t, "from-env", k, "env must win")

	t.Setenv("ANTHROPIC_API_KEY", "")
	k, _ = r.APIKeyValue()
	assert.Equal(t, "from-file", k, "key file must win over inline (and be trimmed)")

	llm.APIKeyFile = ""
	r.Config.LLM = llm
	k, _ = r.APIKeyValue()
	assert.Equal(t, "inline", k, "inline key is the fallback")
}

func TestAPIKeyEnvPerProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")

	anthropic, err := ResolveWith(Config{LLM: LLM{Provider: ProviderAnthropic}}, Overrides{})
	require.NoError(t, err)
	k, _ := anthropic.APIKeyValue()
	assert.Equal(t, "anthropic-key", k)
	openAI, err := ResolveWith(Config{LLM: LLM{Provider: ProviderOpenAICompat}}, Overrides{})
	require.NoError(t, err)
	k, _ = openAI.APIKeyValue()
	assert.Equal(t, "openai-key", k)
}
