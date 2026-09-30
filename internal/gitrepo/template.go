package gitrepo

import (
	"fmt"
	"strings"
	"text/template"
	"time"
)

// DefaultCommitTemplate is used when no custom template has been set.
const DefaultCommitTemplate = `Zamo snapshot ({{.Time.Format "2006-01-02 15:04"}})`

type templateData struct{ Time time.Time }

// validateCommitTemplate parses and test-executes tmpl so a broken template
// is rejected before it is ever saved or used to commit.
func validateCommitTemplate(tmpl string) error {
	_, err := renderCommitMessage(tmpl, time.Now())
	return err
}

// renderCommitMessage fills tmpl with when. Option("missingkey=error") turns
// a typo'd field into a render error instead of a silently blank commit
// message.
func renderCommitMessage(tmpl string, when time.Time) (string, error) {
	t, err := template.New("commit").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse commit message template: %w", err)
	}
	var b strings.Builder
	if err := t.Execute(&b, templateData{Time: when}); err != nil {
		return "", fmt.Errorf("render commit message template: %w", err)
	}
	return b.String(), nil
}
