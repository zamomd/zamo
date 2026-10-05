package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

// scriptedClient answers every chat with one fixed reply and records the
// request it was given.
type scriptedClient struct {
	reply Reply
	err   error
	got   Request
}

func (c *scriptedClient) Chat(_ context.Context, r Request) (Reply, error) {
	c.got = r
	return c.reply, c.err
}

func (c *scriptedClient) Ping(context.Context) error { return nil }

func TestProbeToolsPassesWhenTheModelCallsTheOfferedTool(t *testing.T) {
	c := &scriptedClient{}
	c.reply = Reply{Calls: []ToolCall{{ID: "c1", Name: probeTool}}}

	require.NoError(t, ProbeTools(t.Context(), c))
	require.Len(t, c.got.Tools, 1, "the probe offers exactly one tool")
	assert.Equal(t, probeTool, c.got.Tools[0].Name)
}

func TestProbeToolsFailsWhenTheModelAnswersInText(t *testing.T) {
	c := &scriptedClient{reply: Reply{Text: "Sure! I have called the tool."}}

	assert.ErrorIs(t, ProbeTools(t.Context(), c), knowledge.ErrLLMNoToolSupport)
}

func TestProbeToolsFailsWhenTheModelCallsSomethingElse(t *testing.T) {
	c := &scriptedClient{reply: Reply{Calls: []ToolCall{{ID: "c1", Name: "search"}}}}

	assert.ErrorIs(t, ProbeTools(t.Context(), c), knowledge.ErrLLMNoToolSupport)
}

func TestProbeToolsPassesProviderFailureThrough(t *testing.T) {
	down := errors.Join(knowledge.ErrLLMUnavailable, errors.New("connection refused"))
	c := &scriptedClient{err: down}

	err := ProbeTools(t.Context(), c)

	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
	assert.NotErrorIs(t, err, knowledge.ErrLLMNoToolSupport)
}
