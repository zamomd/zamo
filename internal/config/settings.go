// Settings implements the mutable configuration layer stored in settings.toml.

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Origin says where an effective value came from. The settings screen shows it
// so a value overridden in the GUI is distinguishable from a declared one.
type Origin string

const (
	// OriginDefault means no layer set the field.
	OriginDefault Origin = "default"
	// OriginConfig means config.toml set it.
	OriginConfig Origin = "config"
	// OriginKeyFile means the key came from the api_key_file named in config.toml.
	OriginKeyFile Origin = "api_key_file"
	// OriginGUI means settings.toml set it.
	OriginGUI Origin = "gui"
	// OriginEnv means an environment variable set it; the GUI cannot override it.
	OriginEnv Origin = "env"
)

// Overrides contains values explicitly set in the mutable layer.
type Overrides struct {
	// AIEnabled is the GUI's copy of the global AI policy; nil means the user
	// never touched the switch, so config.toml keeps deciding.
	AIEnabled *bool `toml:"ai_enabled,omitempty"`

	LLM LLMOverrides `toml:"llm"`
}

// LLMOverrides mirrors the settable subset of LLM. Fields are omitted when
// empty so an untouched field never lands in the file.
type LLMOverrides struct {
	Provider *string `toml:"provider,omitempty"`
	Model    *string `toml:"model,omitempty"`
	BaseURL  *string `toml:"base_url,omitempty"`
	// APIKey is written by the GUI. It is not put in config.toml: that file is
	// declarative and may be committed.
	APIKey string `toml:"api_key,omitempty"`
}

// Origins reports the origin of each settable field.
type Origins struct {
	AIEnabled Origin
	Provider  Origin
	Model     Origin
	BaseURL   Origin
	APIKey    Origin
}

// Resolved contains effective values, their origins, and mutable overrides.
type Resolved struct {
	Config    Config
	Origins   Origins
	Overrides Overrides
}

// SettingsPath returns the settings.toml path in the application data directory.
func SettingsPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.toml"), nil
}

// LoadOverrides reads the mutable layer. A missing file is not an error: it
// means nothing has been overridden.
func LoadOverrides(path string) (Overrides, error) {
	var o Overrides
	meta, err := toml.DecodeFile(path, &o)
	switch {
	case os.IsNotExist(err):
		return o, nil
	case err != nil:
		return o, fmt.Errorf("read settings %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return o, fmt.Errorf("settings %s: unknown key %q", path, undecoded[0].String())
	}
	return o, nil
}

// SaveOverrides writes settings atomically with mode 0600.
func SaveOverrides(path string, o Overrides) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("settings directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.toml")
	if err != nil {
		return fmt.Errorf("create temp settings: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp settings: %w", err)
	}
	if err := toml.NewEncoder(tmp).Encode(o); err != nil {
		tmp.Close()
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace settings %s: %w", path, err)
	}
	return nil
}

// Resolve merges the layers at their default locations.
func Resolve() (Resolved, error) {
	settings, err := SettingsPath()
	if err != nil {
		return Resolved{}, err
	}
	return ResolveFrom("", settings)
}

// ResolveFrom merges env, mutable settings, declarative config, and defaults.
// Declarative config disables the mutable layer.
func ResolveFrom(configPath, settingsPath string) (Resolved, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return Resolved{}, err
	}

	if cfg.Declarative {
		// An empty overrides layer is exactly "settings.toml was never read".
		return ResolveWith(cfg, Overrides{})
	}

	o, err := LoadOverrides(settingsPath)
	if err != nil {
		return Resolved{}, err
	}
	r, err := ResolveWith(cfg, o)
	if err != nil {
		return Resolved{}, fmt.Errorf("settings %s: %w", settingsPath, err)
	}
	return r, nil
}

// ResolveWith resolves an in-memory mutable layer over cfg.
func ResolveWith(cfg Config, o Overrides) (Resolved, error) {
	r := Resolved{Config: cfg, Overrides: o}
	if err := settingsSchema.resolve(&r); err != nil {
		return Resolved{}, err
	}
	r.Origins.APIKey = keyOrigin(cfg, o)

	if err := ValidateProvider(r.Config.LLM.Provider); err != nil {
		return Resolved{}, err
	}
	return r, nil
}

// AIEnabled reports the effective AI policy. Unset in every layer means on:
// the switch exists to turn AI off deliberately, not to gate a fresh install.
func (r Resolved) AIEnabled() bool {
	return r.Config.AIEnabledRaw == nil || *r.Config.AIEnabledRaw
}

// keyOrigin reports the effective API key source without reading a key file.
func keyOrigin(cfg Config, o Overrides) Origin {
	provider := cfg.LLM.Provider
	if o.LLM.Provider != nil {
		provider = *o.LLM.Provider
	}
	if envName := KeyEnvName(provider); envName != "" && os.Getenv(envName) != "" {
		return OriginEnv
	}
	if o.LLM.APIKey != "" {
		return OriginGUI
	}
	if cfg.LLM.APIKeyFile != "" {
		return OriginKeyFile
	}
	if cfg.LLM.APIKey != "" {
		return OriginConfig
	}
	return OriginDefault
}

// APIKeySet reports whether a key is configured in any layer. It does not read
// the key file: an unreadable file is Ping's business, not the screen's.
func (r Resolved) APIKeySet() bool { return r.Origins.APIKey != OriginDefault }

// APIKeyValue resolves the current API key without caching file contents.
func (r Resolved) APIKeyValue() (string, error) {
	if envName := KeyEnvName(r.Config.LLM.Provider); envName != "" {
		if key := os.Getenv(envName); key != "" {
			return key, nil
		}
	}
	if r.Overrides.LLM.APIKey != "" {
		return r.Overrides.LLM.APIKey, nil
	}
	return declaredAPIKey(r.Config.LLM)
}
