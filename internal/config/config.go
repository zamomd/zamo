// Package config loads the application configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Provider names accepted in the llm.provider field.
const (
	ProviderAnthropic    = "anthropic"
	ProviderOpenAICompat = "openai-compatible"
)

// Config is the root of config.toml.
type Config struct {
	// Database is the SQLite file path; empty means the platform data
	// directory. The COMIND_DB environment variable overrides both.
	Database string `toml:"database"`

	// Declarative turns off the mutable settings layer: settings.toml is
	// neither read nor written, and the GUI shows values read-only. It lives
	// here and not in settings.toml on purpose — otherwise the GUI could
	// unlock itself.
	Declarative bool `toml:"declarative"`

	LLM LLM `toml:"llm"`
}

// LLM selects and configures the model provider.
type LLM struct {
	// Provider is "anthropic" (default) or "openai-compatible"
	// (OpenAI, OpenRouter, DeepSeek, local Ollama / LM Studio).
	Provider string `toml:"provider"`
	// Model is the model name; empty selects the provider's default.
	Model string `toml:"model"`
	// BaseURL is the endpoint for openai-compatible providers,
	// e.g. http://localhost:11434/v1 for Ollama.
	BaseURL string `toml:"base_url"`
	// APIKey holds the key inline. Prefer APIKeyFile or the environment
	// (ANTHROPIC_API_KEY / OPENAI_API_KEY) — both override this field.
	APIKey string `toml:"api_key"`
	// APIKeyFile is a path to a file containing the key.
	APIKeyFile string `toml:"api_key_file"`
}

// DefaultPath returns the config file location: $COMIND_CONFIG if set,
// otherwise <user config dir>/comind/config.toml (respects
// XDG_CONFIG_HOME on Linux).
func DefaultPath() (string, error) {
	if p := os.Getenv("COMIND_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}
	return filepath.Join(dir, "comind", "config.toml"), nil
}

// Load reads the config file at path (empty means DefaultPath).
// A missing file is not an error: it yields the defaults.
func Load(path string) (Config, error) {
	var cfg Config

	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return cfg, err
		}
		path = p
	}

	meta, err := toml.DecodeFile(path, &cfg)
	switch {
	case os.IsNotExist(err):
		// No file — defaults apply.
	case err != nil:
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	default:
		if undecoded := meta.Undecoded(); len(undecoded) > 0 {
			return cfg, fmt.Errorf("config %s: unknown key %q", path, undecoded[0].String())
		}
	}

	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = ProviderAnthropic
	}
	switch cfg.LLM.Provider {
	case ProviderAnthropic, ProviderOpenAICompat:
	default:
		return cfg, fmt.Errorf("config %s: unknown llm.provider %q (available: %s, %s)",
			path, cfg.LLM.Provider, ProviderAnthropic, ProviderOpenAICompat)
	}
	return cfg, nil
}

// DatabasePath resolves the SQLite file location: $COMIND_DB, then the
// database field, then <data dir>/comind/comind.db. The directory is
// created if missing.
func (c Config) DatabasePath() (string, error) {
	if p := os.Getenv("COMIND_DB"); p != "" {
		return p, nil
	}
	if c.Database != "" {
		return c.Database, nil
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "comind.db"), nil
}

// APIKeyValue resolves the provider key: environment variable first
// (ANTHROPIC_API_KEY or OPENAI_API_KEY, per provider), then
// api_key_file, then the inline api_key. Empty is allowed — local
// providers need no key, and remote ones will fail with a clear error
// on first use.
func (l LLM) APIKeyValue() (string, error) {
	if key := os.Getenv(keyEnvName(l.Provider)); key != "" {
		return key, nil
	}
	if l.APIKeyFile != "" {
		raw, err := os.ReadFile(l.APIKeyFile)
		if err != nil {
			return "", fmt.Errorf("read api_key_file: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	return l.APIKey, nil
}

// keyEnvName is the environment variable holding the key for a provider.
func keyEnvName(provider string) string {
	if provider == ProviderOpenAICompat {
		return "OPENAI_API_KEY"
	}
	return "ANTHROPIC_API_KEY"
}

// dataDir returns the platform directory for application data. Data and
// config deliberately live apart on Linux (XDG_DATA_HOME vs
// XDG_CONFIG_HOME)
func dataDir() (string, error) {
	if runtime.GOOS == "linux" {
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "comind"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		return filepath.Join(home, ".local", "share", "comind"), nil
	}
	// macOS (~/Library/Application Support) and Windows (%AppData%)
	// use one directory for both config and data.
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}
	return filepath.Join(dir, "comind"), nil
}
