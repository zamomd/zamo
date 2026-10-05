package anthropic

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
)

func TestEncodeChatSpellsAToolConversation(t *testing.T) {
	c := New(staticKey("k"), "claude-test")

	body, err := c.EncodeChat(llm.Request{
		System: "answer from the base",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "what links A and B?"},
			{Role: llm.RoleAssistant, Text: "searching", Calls: []llm.ToolCall{
				{ID: "c1", Name: "search", Args: jsontext.Value(`{"query":"A"}`)},
				{ID: "c2", Name: "search", Args: jsontext.Value(`{"query":"B"}`)},
			}},
			{Role: llm.RoleTool, CallID: "c1", Text: `{"hits":[]}`},
			{Role: llm.RoleTool, CallID: "c2", Text: "no such block", Failed: true},
		},
		Tools: []llm.ToolDef{{
			Name: "search", Description: "full-text search",
			Schema: jsontext.Value(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		}},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "claude-test",
		"max_tokens": 4096,
		"system": "answer from the base",
		"messages": [
			{"role": "user", "content": "what links A and B?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "searching"},
				{"type": "tool_use", "id": "c1", "name": "search", "input": {"query": "A"}},
				{"type": "tool_use", "id": "c2", "name": "search", "input": {"query": "B"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "c1", "content": "{\"hits\":[]}"},
				{"type": "tool_result", "tool_use_id": "c2", "content": "no such block", "is_error": true}
			]}
		],
		"tools": [{
			"name": "search",
			"description": "full-text search",
			"input_schema": {"type": "object", "properties": {"query": {"type": "string"}}}
		}]
	}`, string(body))
}

func TestEncodeChatOmitsSystemAndToolsWhenAbsent(t *testing.T) {
	c := New(staticKey("k"), "claude-test")

	body, err := c.EncodeChat(llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "hi"}}})

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "claude-test",
		"max_tokens": 4096,
		"messages": [{"role": "user", "content": "hi"}]
	}`, string(body))
}

func TestEncodeChatSendsEmptyObjectForCallWithoutArgs(t *testing.T) {
	c := New(staticKey("k"), "claude-test")

	body, err := c.EncodeChat(llm.Request{Messages: []llm.Message{
		{Role: llm.RoleAssistant, Calls: []llm.ToolCall{{ID: "c1", Name: "topic_map"}}},
	}})

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "claude-test",
		"max_tokens": 4096,
		"messages": [{"role": "assistant", "content": [
			{"type": "tool_use", "id": "c1", "name": "topic_map", "input": {}}
		]}]
	}`, string(body))
}

func TestDecodeChatReadsTextAndToolCalls(t *testing.T) {
	c := New(staticKey("k"), "")

	reply, err := c.DecodeChat([]byte(`{"content":[
		{"type":"text","text":"let me "},
		{"type":"text","text":"look"},
		{"type":"tool_use","id":"c1","name":"search","input":{"query":"A"}}
	]}`))

	require.NoError(t, err)
	assert.Equal(t, "let me look", reply.Text)
	require.Len(t, reply.Calls, 1)
	assert.Equal(t, "c1", reply.Calls[0].ID)
	assert.Equal(t, "search", reply.Calls[0].Name)
	assert.JSONEq(t, `{"query":"A"}`, string(reply.Calls[0].Args))
}

func TestDecodeChatWithoutEnvelopeIsUnavailable(t *testing.T) {
	c := New(staticKey("k"), "")

	_, err := c.DecodeChat([]byte(`{"foo":"bar"}`))

	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}
