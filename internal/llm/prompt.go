package llm

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/zamomd/zamo/internal/knowledge"
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

// EditPrompt renders the restructuring prompt for a scope (a block or subtree)
// and a text command. Providers share it so the edit instructions live in one
// template (prompts/edit_structure.md). The scope blocks are listed with their
// ids so the model can target existing blocks; ParseEditOps consumes the op
// array the prompt asks for.
func EditPrompt(scope []knowledge.Block, instruction string) (string, error) {
	tmpl, err := template.ParseFS(Prompts, "prompts/edit_structure.md")
	if err != nil {
		return "", fmt.Errorf("parse edit prompt: %w", err)
	}
	var out strings.Builder
	data := struct {
		Blocks      []knowledge.Block
		Instruction string
	}{Blocks: scope, Instruction: instruction}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render edit prompt: %w", err)
	}
	return out.String(), nil
}

// ExpandDocumentPrompt renders the document-expansion prompt (prompts/
// expand_document.md), the document-shaped twin of ExpandPrompt.
func ExpandDocumentPrompt(topic, hint string) (string, error) {
	tmpl, err := template.ParseFS(Prompts, "prompts/expand_document.md")
	if err != nil {
		return "", fmt.Errorf("parse expand document prompt: %w", err)
	}
	var out strings.Builder
	data := struct{ Topic, Hint string }{Topic: topic, Hint: hint}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render expand document prompt: %w", err)
	}
	return out.String(), nil
}

// EditDocumentPrompt renders the document-edit prompt (prompts/
// edit_document.md) over a document and a text command.
func EditDocumentPrompt(doc knowledge.Block, instruction string) (string, error) {
	tmpl, err := template.ParseFS(Prompts, "prompts/edit_document.md")
	if err != nil {
		return "", fmt.Errorf("parse edit document prompt: %w", err)
	}
	var out strings.Builder
	data := struct{ Document, Instruction string }{Document: doc.Content, Instruction: instruction}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render edit document prompt: %w", err)
	}
	return out.String(), nil
}
