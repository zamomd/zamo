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
	if !strings.Contains(got, "children") {
		t.Errorf("rendered prompt does not describe the JSON tree shape:\n%s", got)
	}
}

func TestExpandPromptOmitsEmptyHint(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "")
	if err != nil {
		t.Fatalf("ExpandPrompt: %v", err)
	}
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
	for _, leak := range []string{"<!--", "LLMClient", "text/template"} {
		if strings.Contains(got, leak) {
			t.Errorf("prompt leaked maintainer doc %q to the model:\n%s", leak, got)
		}
	}
}

func TestExpandDocumentPromptIncludesTopicAndHint(t *testing.T) {
	got, err := ExpandDocumentPrompt("Go contexts", "focus on cancellation")
	if err != nil {
		t.Fatalf("ExpandDocumentPrompt: %v", err)
	}
	if !strings.Contains(got, "Go contexts") {
		t.Errorf("rendered prompt is missing the topic:\n%s", got)
	}
	if !strings.Contains(got, "focus on cancellation") {
		t.Errorf("rendered prompt is missing the hint:\n%s", got)
	}
}

func TestExpandDocumentPromptOmitsEmptyHint(t *testing.T) {
	got, err := ExpandDocumentPrompt("Go contexts", "")
	if err != nil {
		t.Fatalf("ExpandDocumentPrompt: %v", err)
	}
	if strings.Contains(got, "{{") || strings.Contains(strings.ToLower(got), "focus on:") {
		t.Errorf("empty hint leaked a label or placeholder into the prompt:\n%s", got)
	}
}

func TestEditDocumentPromptCarriesDocumentAndInstruction(t *testing.T) {
	doc := knowledge.Block{ID: "01A", Content: "# Go contexts\n\nBody text."}
	got, err := EditDocumentPrompt(doc, "rewrite the body")
	if err != nil {
		t.Fatalf("EditDocumentPrompt: %v", err)
	}
	if !strings.Contains(got, "rewrite the body") {
		t.Errorf("prompt is missing the instruction:\n%s", got)
	}
	if !strings.Contains(got, doc.Content) {
		t.Errorf("prompt is missing the document content:\n%s", got)
	}
}

func TestDocumentPromptsDoNotLeakMaintainerDoc(t *testing.T) {
	expand, err := ExpandDocumentPrompt("Go contexts", "")
	if err != nil {
		t.Fatalf("ExpandDocumentPrompt: %v", err)
	}
	edit, err := EditDocumentPrompt(knowledge.Block{Content: "x"}, "do a thing")
	if err != nil {
		t.Fatalf("EditDocumentPrompt: %v", err)
	}
	for _, leak := range []string{"<!--", "LLMClient", "text/template"} {
		if strings.Contains(expand, leak) {
			t.Errorf("expand document prompt leaked maintainer doc %q:\n%s", leak, expand)
		}
		if strings.Contains(edit, leak) {
			t.Errorf("edit document prompt leaked maintainer doc %q:\n%s", leak, edit)
		}
	}
}
