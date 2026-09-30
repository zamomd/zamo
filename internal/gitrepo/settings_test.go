package gitrepo

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/config"
)

func TestPreferencesRoundTripThroughSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")

	got, err := loadPreferences(path)
	require.NoError(t, err)
	assert.Equal(t, preferences{}, got, "a missing settings file means nothing was ever set")

	want := preferences{Dir: "/snapshots/base", CommitTemplate: "custom {{.Time}}"}
	require.NoError(t, savePreferences(path, want))

	got, err = loadPreferences(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSavePreferencesLeavesOtherOverridesUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	apiKey := "sk-test"
	require.NoError(t, config.SaveOverrides(path, config.Overrides{
		LLM: config.LLMOverrides{APIKey: apiKey},
	}))

	require.NoError(t, savePreferences(path, preferences{Dir: "/snapshots"}))

	o, err := config.LoadOverrides(path)
	require.NoError(t, err)
	assert.Equal(t, apiKey, o.LLM.APIKey, "saving the snapshot directory must not drop unrelated LLM overrides")
	assert.Equal(t, "/snapshots", o.GitSnapshot.Dir)
}
