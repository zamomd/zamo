package llm

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

// Prompts are template files: the person responsible for answer quality
// iterates on them often, without digging through the code.
//
//go:embed prompts/*.md
var Prompts embed.FS

// Persona is who the model is asked to be in an agent run.
type Persona string

// The personas, each with its own system prompt template.
const (
	// PersonaAssistant converses with the owner: it answers from the base
	// and changes it when asked.
	PersonaAssistant Persona = "assistant"
	// PersonaEditor carries out one instruction with nobody to talk to.
	PersonaEditor Persona = "editor"
)

// Scope is what a system prompt tells the model about where it may write.
// The zero Scope is a run that may not write.
type Scope struct {
	// ID and Title name the block the run may change; empty when it has none.
	ID, Title string
	// Document reports that the scope is a document topic.
	Document bool
	// NewTopic reports that the run may create topics.
	NewTopic bool
}

// CanWrite reports whether the run may change anything.
func (s Scope) CanWrite() bool { return s.ID != "" || s.NewTopic }

// SystemPrompt renders the system prompt of a persona for a run with the
// given scope (prompts/assistant.md, prompts/editor.md, with the rules both
// share in prompts/writing.md).
func SystemPrompt(p Persona, s Scope) (string, error) {
	return render(string(p)+".md", s)
}

// ExpandInstruction renders the instruction that has an editor run write an
// existing, empty topic (prompts/expand.md).
func ExpandInstruction(topic string) (string, error) {
	return render("expand.md", struct{ Topic string }{topic})
}

func render(file string, data any) (string, error) {
	tmpl, err := template.ParseFS(Prompts, "prompts/"+file, "prompts/writing.md")
	if err != nil {
		return "", fmt.Errorf("parse prompt %s: %w", file, err)
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, file, data); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", file, err)
	}
	return strings.TrimSpace(out.String()), nil
}
