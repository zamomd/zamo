// Settings are the mutable configuration layer: the part the GUI may write.
// It is a separate file from config.toml on purpose. config.toml is the
// declarative artifact — under home-manager it is a read-only symlink into the
// nix store — so the application only ever reads it. Merging happens in memory
// at read time; nothing is written back.

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

// Overrides is the mutable layer. Only fields the user actually changed in the
// GUI are present: an absent field means "not overridden", which is what makes
// "reset to config" expressible at all.
type Overrides struct {
	LLM LLMOverrides `toml:"llm"`
}

// LLMOverrides mirrors the settable subset of LLM. Fields are omitted when
// empty so an untouched field never lands in the file.
type LLMOverrides struct {
	Provider string `toml:"provider,omitempty"`
	Model    string `toml:"model,omitempty"`
	BaseURL  string `toml:"base_url,omitempty"`
	// APIKey is written by the GUI. It is not put in config.toml: that file is
	// declarative and may be committed.
	APIKey string `toml:"api_key,omitempty"`
}

// Origins reports the origin of each settable field.
type Origins struct {
	Provider Origin
	Model    Origin
	BaseURL  Origin
	APIKey   Origin
}

// Resolved is the effective configuration with the origin of every settable
// field. Config carries the merged values except the API key: that one stays
// unresolved and is read through APIKeyValue on every use, never cached.
type Resolved struct {
	Config    Config
	Origins   Origins
	Overrides Overrides
}

// SettingsPath returns the mutable layer's location: <data dir>/settings.toml,
// next to the database. It is deliberately not next to config.toml — that
// directory may be read-only.
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

// SaveOverrides writes the mutable layer atomically with mode 0600 — it holds
// an API key. The temporary file is created in the target directory so the
// rename cannot cross a filesystem boundary.
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

// ResolveFrom merges the layers: env > settings.toml > config.toml > defaults.
// An empty configPath means the default location. When config.toml sets
// declarative, settings.toml is neither read nor consulted — a file left over
// from before the flag was set must not leak back in.
func ResolveFrom(configPath, settingsPath string) (Resolved, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return Resolved{}, err
	}

	r := Resolved{Config: cfg}
	r.Origins = Origins{
		Provider: declaredOrigin(cfg.LLM.Provider != ""),
		Model:    declaredOrigin(cfg.LLM.Model != ""),
		BaseURL:  declaredOrigin(cfg.LLM.BaseURL != ""),
	}
	if cfg.Declarative {
		r.Origins.APIKey = keyOrigin(cfg, Overrides{})
		return r, nil
	}

	o, err := LoadOverrides(settingsPath)
	if err != nil {
		return Resolved{}, err
	}
	r.Overrides = o

	if o.LLM.Provider != "" {
		r.Config.LLM.Provider = o.LLM.Provider
		r.Origins.Provider = OriginGUI
	}
	if o.LLM.Model != "" {
		r.Config.LLM.Model = o.LLM.Model
		r.Origins.Model = OriginGUI
	}
	if o.LLM.BaseURL != "" {
		r.Config.LLM.BaseURL = o.LLM.BaseURL
		r.Origins.BaseURL = OriginGUI
	}
	r.Origins.APIKey = keyOrigin(cfg, o)

	switch r.Config.LLM.Provider {
	case "", ProviderAnthropic, ProviderOpenAICompat:
	default:
		return Resolved{}, fmt.Errorf("settings %s: unknown llm.provider %q (available: %s, %s)",
			settingsPath, r.Config.LLM.Provider, ProviderAnthropic, ProviderOpenAICompat)
	}
	return r, nil
}

// declaredOrigin maps "config.toml set this field" to an Origin.
func declaredOrigin(set bool) Origin {
	if set {
		return OriginConfig
	}
	return OriginDefault
}

// keyOrigin reports where the API key would come from, without reading the key
// file: "configured" is a cheaper question than "readable", and Ping answers
// the latter. When provider is empty (AI off), no environment variable is
// consulted even if one is set.
func keyOrigin(cfg Config, o Overrides) Origin {
	provider := cfg.LLM.Provider
	if o.LLM.Provider != "" {
		provider = o.LLM.Provider
	}
	if envName := keyEnvName(provider); envName != "" && os.Getenv(envName) != "" {
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

// APIKeyValue resolves the key: env > settings.toml > api_key_file > api_key.
// It is called on every request, not cached — a key file managed by sops-nix
// is rewritten on each activation. When provider is empty (AI off), no
// environment variable is consulted.
func (r Resolved) APIKeyValue() (string, error) {
	if envName := keyEnvName(r.Config.LLM.Provider); envName != "" {
		if key := os.Getenv(envName); key != "" {
			return key, nil
		}
	}
	if r.Overrides.LLM.APIKey != "" {
		return r.Overrides.LLM.APIKey, nil
	}
	return r.Config.LLM.APIKeyValue()
}
