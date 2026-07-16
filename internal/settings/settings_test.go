package settings

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/config"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
)

// stubClient stands in for the client the manager starts with.
type stubClient struct{}

func (stubClient) GenerateTree(_ context.Context, _, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) GenerateEdit(_ context.Context, _ []knowledge.Block, _ string) ([]knowledge.Op, error) {
	return nil, nil
}

func (stubClient) Ping(_ context.Context) error { return nil }

// newManager points a Manager at a temp config.toml and settings.toml. It
// clears both provider key env vars first: ambient ANTHROPIC_API_KEY or
// OPENAI_API_KEY has leaked into these tests before.
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
	// The swap is the point of the whole task, so check it directly: after Save,
	// the holder the service already holds must reach the new provider. A stub
	// server standing in for it is the observable proof.
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

	// The holder started with stubClient, whose Ping is a no-op that touches no
	// network. Reaching the server can only mean Save swapped the client in.
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
	// The View has no field for the key value at all; this test documents that
	// the screen learns only whether one is set.
}

func TestCheckDoesNotSave(t *testing.T) {
	m, _, setPath := newManager(t, "")
	// The check reaches a provider that is not there; the point is only that a
	// failed check leaves no file behind.
	_ = m.Check(t.Context(), In{
		Provider: config.ProviderOpenAICompat,
		BaseURL:  "http://127.0.0.1:1/v1",
	})
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("Check wrote settings.toml; it must not save")
	}
}
