package gitrepo

import (
	"fmt"

	"github.com/zamomd/zamo/internal/config"
)

// preferences is the git snapshot module's GUI-set state: which directory
// is managed, and what commit message template to use there.
type preferences struct {
	Dir            string
	CommitTemplate string
}

// loadPreferences reads the preferences from settings.toml. A missing file
// or an unset directory is not an error: it means nothing has been chosen
// yet.
func loadPreferences(settingsPath string) (preferences, error) {
	o, err := config.LoadOverrides(settingsPath)
	if err != nil {
		return preferences{}, fmt.Errorf("read git snapshot preferences: %w", err)
	}
	return preferences{Dir: o.GitSnapshot.Dir, CommitTemplate: o.GitSnapshot.CommitTemplate}, nil
}

// savePreferences writes p into settings.toml, preserving every other
// override already there (LLM, AIEnabled, and anything this build does not
// recognize).
func savePreferences(settingsPath string, p preferences) error {
	o, err := config.LoadOverrides(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings before saving git snapshot preferences: %w", err)
	}
	o.GitSnapshot = config.GitSnapshotOverrides{Dir: p.Dir, CommitTemplate: p.CommitTemplate}
	if err := config.SaveOverrides(settingsPath, o); err != nil {
		return fmt.Errorf("save git snapshot preferences: %w", err)
	}
	return nil
}
