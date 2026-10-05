package llm

import (
	"context"
	"encoding/json/jsontext"
)

// Role says who a message in a conversation is from.
type Role string

// The roles of a conversation.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool is the result of a tool call the assistant asked for.
	RoleTool Role = "tool"
)

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	// ID ties the call to the message carrying its result.
	ID   string
	Name string
	// Args is the JSON object of arguments, as the model wrote it.
	Args jsontext.Value
}

// Message is one turn of a conversation. A user turn carries Text; an
// assistant turn carries Text, Calls, or both; a tool turn carries the result
// of one call in Text and names that call in CallID.
type Message struct {
	Role   Role
	Text   string
	Calls  []ToolCall
	CallID string
	// Failed marks a tool result that reports an error rather than data.
	Failed bool
}

// ToolDef describes one tool the model may call.
type ToolDef struct {
	Name        string
	Description string
	// Schema is the JSON Schema of the tool's arguments object.
	Schema jsontext.Value
}

// Request is one chat round: everything the model sees.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolDef
}

// Reply is the model's turn: text, tool calls, or both. A reply with calls
// expects their results before the conversation continues.
type Reply struct {
	Text  string
	Calls []ToolCall
}

// Client is the boundary with an LLM provider.
type Client interface {
	// Chat runs one round and returns the model's turn.
	Chat(ctx context.Context, r Request) (Reply, error)
	// Ping reports whether the provider is reachable and the credentials are
	// accepted, without exercising the model.
	Ping(ctx context.Context) error
}
