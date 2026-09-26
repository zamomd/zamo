package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func TestExpandPromptIncludesTopicAndHint(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "focus on cancellation")
	require.NoError(t, err)
	assert.Contains(t, got, "Go contexts", "prompt is missing the topic")
	assert.Contains(t, got, "focus on cancellation", "prompt is missing the hint")
	assert.Contains(t, got, "children", "rendered prompt does not describe the JSON tree shape")
}

func TestExpandPromptOmitsEmptyHint(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "")
	require.NoError(t, err)
	assert.NotContains(t, got, "{{", "empty hint leaked a placeholder into the prompt")
	assert.NotContains(t, strings.ToLower(got), "hint:", "empty hint leaked a label into the prompt")
}

func TestEditPromptCarriesScopeAndInstruction(t *testing.T) {
	scope := []knowledge.Block{
		{ID: "01A", Content: "context.Context carries deadlines"},
		{ID: "01B", ParentID: "01A", Content: "WithCancel returns a cancel func"},
	}
	got, err := EditPrompt(scope, "split the second block into more detail")
	require.NoError(t, err)
	assert.Contains(t, got, "split the second block into more detail", "prompt is missing the instruction")
	for _, b := range scope {
		assert.Contains(t, got, string(b.ID))
		assert.Contains(t, got, b.Content)
	}
}

func TestEditPromptDescribesOpShape(t *testing.T) {
	got, err := EditPrompt([]knowledge.Block{{ID: "01A", Content: "x"}}, "do a thing")
	require.NoError(t, err)
	for _, want := range []string{"add", "edit", "move", "delete", "block_id"} {
		assert.Contains(t, got, want, "prompt does not describe the edit op shape")
	}
}

func TestEditPromptDoesNotLeakMaintainerDoc(t *testing.T) {
	got, err := EditPrompt([]knowledge.Block{{ID: "01A", Content: "x"}}, "do a thing")
	require.NoError(t, err)
	for _, leak := range []string{"<!--", "text/template"} {
		assert.NotContains(t, got, leak, "prompt leaked maintainer doc to the model")
	}
}

func TestExpandPromptDoesNotLeakMaintainerDoc(t *testing.T) {
	got, err := ExpandPrompt("Go contexts", "")
	require.NoError(t, err)
	for _, leak := range []string{"<!--", "LLMClient", "text/template"} {
		assert.NotContains(t, got, leak, "prompt leaked maintainer doc to the model")
	}
}

func TestExpandDocumentPromptIncludesTopicAndHint(t *testing.T) {
	got, err := ExpandDocumentPrompt("Go contexts", "focus on cancellation")
	require.NoError(t, err)
	assert.Contains(t, got, "Go contexts", "prompt is missing the topic")
	assert.Contains(t, got, "focus on cancellation", "prompt is missing the hint")
}

func TestExpandDocumentPromptOmitsEmptyHint(t *testing.T) {
	got, err := ExpandDocumentPrompt("Go contexts", "")
	require.NoError(t, err)
	assert.NotContains(t, got, "{{", "empty hint leaked a placeholder into the prompt")
	assert.NotContains(t, strings.ToLower(got), "focus on:", "empty hint leaked a label into the prompt")
}

func TestEditDocumentPromptCarriesDocumentAndInstruction(t *testing.T) {
	doc := knowledge.Block{ID: "01A", Content: "# Go contexts\n\nBody text."}
	got, err := EditDocumentPrompt(doc, "rewrite the body")
	require.NoError(t, err)
	assert.Contains(t, got, "rewrite the body", "prompt is missing the instruction")
	assert.Contains(t, got, doc.Content, "prompt is missing the document content")
}

func TestDocumentPromptsDoNotLeakMaintainerDoc(t *testing.T) {
	expand, err := ExpandDocumentPrompt("Go contexts", "")
	require.NoError(t, err)
	edit, err := EditDocumentPrompt(knowledge.Block{Content: "x"}, "do a thing")
	require.NoError(t, err)
	for _, leak := range []string{"<!--", "LLMClient", "text/template"} {
		assert.NotContains(t, expand, leak, "expand document prompt leaked maintainer doc")
		assert.NotContains(t, edit, leak, "edit document prompt leaked maintainer doc")
	}
}
