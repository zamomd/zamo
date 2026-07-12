package llm

import (
	"strings"
	"testing"
)

func TestExpandPromptIncludesTopicAndHint(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "focus on cancellation")
	if err != nil {
		t.Fatalf("ExpandPrompt: %v", err)
	}
	if !strings.Contains(got, "Go contexts") {
		t.Errorf("rendered prompt is missing the topic:\n%s", got)
	}
	if !strings.Contains(got, "focus on cancellation") {
		t.Errorf("rendered prompt is missing the hint:\n%s", got)
	}
	// The prompt must still ask for the JSON tree shape ParseOps consumes.
	if !strings.Contains(got, "children") {
		t.Errorf("rendered prompt does not describe the JSON tree shape:\n%s", got)
	}
}

func TestExpandPromptOmitsEmptyHint(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "")
	if err != nil {
		t.Fatalf("ExpandPrompt: %v", err)
	}
	// With no hint, the template must not leave a dangling "hint:" label or the
	// literal placeholder behind.
	if strings.Contains(got, "{{") || strings.Contains(strings.ToLower(got), "hint:") {
		t.Errorf("empty hint leaked a label or placeholder into the prompt:\n%s", got)
	}
}

func TestExpandPromptDoesNotLeakMaintainerDoc(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "")
	if err != nil {
		t.Fatalf("ExpandPrompt: %v", err)
	}
	// text/template does not strip HTML comments, so an <!-- --> block would be
	// sent to the model verbatim. Maintainer notes must stay out of the prompt.
	for _, leak := range []string{"<!--", "LLMClient", "text/template"} {
		if strings.Contains(got, leak) {
			t.Errorf("prompt leaked maintainer doc %q to the model:\n%s", leak, got)
		}
	}
}
