package llm

import (
	"fmt"
	"strings"
	"text/template"
)

// ExpandPrompt renders the topic-expansion prompt for a topic and an optional
// hint. Providers share this so the expansion instructions live in one template
// (prompts/expand_topic.md), not duplicated per provider. An empty hint drops
// the focus line rather than rendering a blank one.
func ExpandPrompt(topic, hint string) (string, error) {
	tmpl, err := template.ParseFS(Prompts, "prompts/expand_topic.md")
	if err != nil {
		return "", fmt.Errorf("parse expand prompt: %w", err)
	}
	var out strings.Builder
	data := struct{ Topic, Hint string }{Topic: topic, Hint: hint}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render expand prompt: %w", err)
	}
	return out.String(), nil
}
