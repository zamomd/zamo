package llm

import (
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
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

func TestEditPromptCarriesScopeAndInstruction(t *testing.T) {
	scope := []knowledge.Block{
		{ID: "01A", Content: "context.Context carries deadlines"},
		{ID: "01B", ParentID: "01A", Content: "WithCancel returns a cancel func"},
	}
	got, err := EditPrompt(scope, "split the second block into more detail")
	if err != nil {
		t.Fatalf("EditPrompt: %v", err)
	}
	// The instruction and every scope block (id + content) must reach the model,
	// so it can target existing blocks by id.
	if !strings.Contains(got, "split the second block into more detail") {
		t.Errorf("prompt is missing the instruction:\n%s", got)
	}
	for _, b := range scope {
		if !strings.Contains(got, string(b.ID)) {
			t.Errorf("prompt is missing block id %q:\n%s", b.ID, got)
		}
		if !strings.Contains(got, b.Content) {
			t.Errorf("prompt is missing block content %q:\n%s", b.Content, got)
		}
	}
}

func TestEditPromptDescribesOpShape(t *testing.T) {
	got, err := EditPrompt([]knowledge.Block{{ID: "01A", Content: "x"}}, "do a thing")
	if err != nil {
		t.Fatalf("EditPrompt: %v", err)
	}
	// The prompt must ask for the op array ParseEditOps consumes: the kinds and
	// the block_id reference field.
	for _, want := range []string{"add", "edit", "move", "delete", "block_id"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt does not describe the edit op shape (missing %q):\n%s", want, got)
		}
	}
}

func TestEditPromptDoesNotLeakMaintainerDoc(t *testing.T) {
	got, err := EditPrompt([]knowledge.Block{{ID: "01A", Content: "x"}}, "do a thing")
	if err != nil {
		t.Fatalf("EditPrompt: %v", err)
	}
	for _, leak := range []string{"<!--", "text/template"} {
		if strings.Contains(got, leak) {
			t.Errorf("prompt leaked maintainer doc %q to the model:\n%s", leak, got)
		}
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
