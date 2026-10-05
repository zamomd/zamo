package llm

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

const probeTool = "ping"

// ProbeTools reports whether the model behind c calls a tool when asked to:
// one round offering a single tool with no arguments. A model that answers in
// text instead fails with ErrLLMNoToolSupport.
func ProbeTools(ctx context.Context, c Client) error {
	reply, err := c.Chat(ctx, Request{
		Messages: []Message{{Role: RoleUser, Text: "Call the " + probeTool + " tool now."}},
		Tools: []ToolDef{{
			Name:        probeTool,
			Description: "Takes no arguments and returns nothing.",
			Schema:      jsontext.Value(`{"type":"object","properties":{}}`),
		}},
	})
	if err != nil {
		return err
	}
	for _, call := range reply.Calls {
		if call.Name == probeTool {
			return nil
		}
	}
	return fmt.Errorf("%w: it answered without calling the offered tool", knowledge.ErrLLMNoToolSupport)
}
