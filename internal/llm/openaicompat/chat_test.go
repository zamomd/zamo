package openaicompat

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
)

func TestEncodeChatSpellsAToolConversation(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	body, err := c.EncodeChat(llm.Request{
		System: "answer from the base",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "what links A and B?"},
			{Role: llm.RoleAssistant, Text: "searching", Calls: []llm.ToolCall{
				{ID: "c1", Name: "search", Args: jsontext.Value(`{"query":"A"}`)},
				{ID: "c2", Name: "topic_map"},
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
		"model": "llama3",
		"max_tokens": 4096,
		"messages": [
			{"role": "system", "content": "answer from the base"},
			{"role": "user", "content": "what links A and B?"},
			{"role": "assistant", "content": "searching", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "search", "arguments": "{\"query\":\"A\"}"}},
				{"id": "c2", "type": "function", "function": {"name": "topic_map", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c1", "content": "{\"hits\":[]}"},
			{"role": "tool", "tool_call_id": "c2", "content": "error: no such block"}
		],
		"tools": [{
			"type": "function",
			"function": {
				"name": "search",
				"description": "full-text search",
				"parameters": {"type": "object", "properties": {"query": {"type": "string"}}}
			}
		}]
	}`, string(body))
}

func TestEncodeChatOmitsSystemAndToolsWhenAbsent(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	body, err := c.EncodeChat(llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "hi"}}})

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "llama3",
		"max_tokens": 4096,
		"messages": [{"role": "user", "content": "hi"}]
	}`, string(body))
}

func TestDecodeChatReadsTextAndToolCalls(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	reply, err := c.DecodeChat([]byte(`{"choices":[{"message":{
		"role":"assistant",
		"content":"looking",
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{\"query\":\"A\"}"}}]
	}}]}`))

	require.NoError(t, err)
	assert.Equal(t, "looking", reply.Text)
	require.Len(t, reply.Calls, 1)
	assert.Equal(t, "c1", reply.Calls[0].ID)
	assert.Equal(t, "search", reply.Calls[0].Name)
	assert.JSONEq(t, `{"query":"A"}`, string(reply.Calls[0].Args))
}

func TestDecodeChatAcceptsNullContentBesideToolCalls(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	reply, err := c.DecodeChat([]byte(`{"choices":[{"message":{
		"role":"assistant",
		"content":null,
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"topic_map","arguments":""}}]
	}}]}`))

	require.NoError(t, err)
	assert.Empty(t, reply.Text)
	require.Len(t, reply.Calls, 1)
	assert.JSONEq(t, `{}`, string(reply.Calls[0].Args), "empty arguments read as an empty object")
}

func TestDecodeChatRefusesArgumentsThatAreNotJSON(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	_, err := c.DecodeChat([]byte(`{"choices":[{"message":{
		"role":"assistant",
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{query: A"}}]
	}}]}`))

	assert.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
}

func TestDecodeChatWithoutEnvelopeIsUnavailable(t *testing.T) {
	c := New("http://localhost/v1", staticKey(""), "llama3")

	_, err := c.DecodeChat([]byte(`{"foo":"bar"}`))

	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}
