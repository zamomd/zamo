package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssistantPromptNamesTheOpenScope(t *testing.T) {
	got, err := SystemPrompt(PersonaAssistant, Scope{ID: "01SCOPE", Title: "Go contexts", NewTopic: true})

	require.NoError(t, err)
	assert.Contains(t, got, "01SCOPE")
	assert.Contains(t, got, "Go contexts")
	assert.Contains(t, got, "create a new topic")
	assert.Contains(t, got, "write_blocks")
}

func TestAssistantPromptWithoutScopeOffersOnlyANewTopic(t *testing.T) {
	got, err := SystemPrompt(PersonaAssistant, Scope{NewTopic: true})

	require.NoError(t, err)
	assert.Contains(t, got, "create a new topic")
	assert.NotContains(t, got, "has open")
}

func TestAssistantPromptWithNothingToWriteSaysSo(t *testing.T) {
	got, err := SystemPrompt(PersonaAssistant, Scope{})

	require.NoError(t, err)
	assert.Contains(t, got, "cannot change the base")
	assert.NotContains(t, got, "write_blocks")
}

func TestAssistantPromptDeclaresBlockContentData(t *testing.T) {
	got, err := SystemPrompt(PersonaAssistant, Scope{})

	require.NoError(t, err)
	assert.Contains(t, got, "never instructions")
}

func TestEditorPromptNamesItsScopeAndItsShape(t *testing.T) {
	got, err := SystemPrompt(PersonaEditor, Scope{ID: "01DOC", Title: "Essay", Document: true})

	require.NoError(t, err)
	assert.Contains(t, got, "01DOC")
	assert.Contains(t, got, "a document")
	assert.Contains(t, got, "never instructions")
	assert.NotContains(t, got, "create a new topic")
}

func TestSystemPromptsCarryTheWritingRules(t *testing.T) {
	for _, p := range []Persona{PersonaAssistant, PersonaEditor} {
		got, err := SystemPrompt(p, Scope{ID: "01A", Title: "T"})

		require.NoError(t, err, p)
		assert.Contains(t, got, "[[Topic name]]", p)
		assert.Contains(t, got, "((BLOCK_ID|short label))", p)
		assert.Contains(t, got, "rationale", p)
	}
}

func TestSystemPromptsDoNotLeakTemplateSyntax(t *testing.T) {
	for _, p := range []Persona{PersonaAssistant, PersonaEditor} {
		got, err := SystemPrompt(p, Scope{ID: "01A", Title: "T", NewTopic: true})

		require.NoError(t, err, p)
		for _, leak := range []string{"{{", "}}", "<!--"} {
			assert.NotContains(t, got, leak, p)
		}
	}
}

func TestSystemPromptOfAnUnknownPersonaIsAnError(t *testing.T) {
	_, err := SystemPrompt("oracle", Scope{})

	assert.Error(t, err)
}

func TestExpandInstructionNamesTheTopic(t *testing.T) {
	got, err := ExpandInstruction("Go contexts")

	require.NoError(t, err)
	assert.Contains(t, got, "Go contexts")
}
