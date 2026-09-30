package gitrepo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCommitTemplateRejectsWhatItCannotRender(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		wantErr bool
	}{
		{"default template", DefaultCommitTemplate, false},
		{"custom template using Time", "Snapshot at {{.Time.Format \"15:04\"}}", false},
		{"unparsable template", "{{.Time", true},
		{"unknown field", "{{.NotAField}}", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCommitTemplate(tc.tmpl)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRenderCommitMessageFormatsTheGivenTime(t *testing.T) {
	when := time.Date(2026, 9, 30, 14, 5, 0, 0, time.UTC)

	got, err := renderCommitMessage("Snapshot at {{.Time.Format \"2006-01-02 15:04\"}}", when)

	require.NoError(t, err)
	assert.Equal(t, "Snapshot at 2026-09-30 14:05", got)
}
