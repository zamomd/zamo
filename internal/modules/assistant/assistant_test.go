package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// fakeAgent answers each run with the next scripted result and records what
// it was asked. A run blocks on hold when it is set.
type fakeAgent struct {
	runs    []host.AgentRun
	results []host.AgentResult
	err     error
	hold    chan struct{}
	entered chan struct{}
}

func (f *fakeAgent) Run(ctx context.Context, run host.AgentRun) (host.AgentResult, error) {
	n := len(f.runs)
	f.runs = append(f.runs, run)
	if f.hold != nil {
		close(f.entered)
		select {
		case <-f.hold:
		case <-ctx.Done():
			return host.AgentResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return host.AgentResult{}, f.err
	}
	return f.results[n], nil
}

type fakeWriter struct {
	meta knowledge.WriteMeta
	ops  []knowledge.Op
	err  error
}

func (f *fakeWriter) ApplyOps(
	_ context.Context, meta knowledge.WriteMeta, ops []knowledge.Op,
) (knowledge.Applied, error) {
	f.meta, f.ops = meta, ops
	if f.err != nil {
		return knowledge.Applied{}, f.err
	}
	return knowledge.Applied{GroupID: "g-save", IDs: map[string]knowledge.ID{ops[0].TempID: "blk-saved"}}, nil
}

func exchange(question, answer string) []host.Message {
	return []host.Message{{Role: "user", Text: question}, {Role: "assistant", Text: answer}}
}

func code(t *testing.T, err error) string {
	t.Helper()
	coded, ok := errors.AsType[host.Coded](err)
	require.True(t, ok, "want a coded error, got %v", err)
	return coded.Code()
}

func TestSendRunsTheAssistantOverTheOpenContext(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{
		{Text: "X is a thing", Messages: exchange("what is X?", "X is a thing")},
	}}
	m := New(agent, &fakeWriter{})

	answer, err := m.Send(t.Context(), "  what is X?  ", Context{ScopeID: "blk", TopicID: "topic"})

	require.NoError(t, err)
	assert.Equal(t, Answer{Turn: 0, Text: "X is a thing"}, answer)
	require.Len(t, agent.runs, 1)
	assert.Equal(t, host.AgentRun{
		Persona:     host.PersonaAssistant,
		Instruction: "what is X?",
		Write:       host.WritePolicy{Scope: "blk", AllowNewTopic: true},
		Source:      Source,
	}, agent.runs[0])
}

func TestSendScopesToTheTopicWhenNoBlockIsSelected(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{Text: "ok"}}}
	m := New(agent, &fakeWriter{})

	_, err := m.Send(t.Context(), "tidy up", Context{TopicID: "topic"})

	require.NoError(t, err)
	assert.Equal(t, host.WritePolicy{Scope: "topic", AllowNewTopic: true}, agent.runs[0].Write)
}

func TestSendWithoutContextMayOnlyCreateATopic(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{Text: "ok"}}}
	m := New(agent, &fakeWriter{})

	_, err := m.Send(t.Context(), "what do I know about Go?", Context{})

	require.NoError(t, err)
	assert.Equal(t, host.WritePolicy{AllowNewTopic: true}, agent.runs[0].Write)
}

func TestSendPassesTheConversationToTheNextRun(t *testing.T) {
	first := exchange("what is X?", "X is a thing")
	agent := &fakeAgent{results: []host.AgentResult{
		{Text: "X is a thing", Messages: first},
		{Text: "more", Messages: append(first, exchange("more?", "more")...)},
	}}
	m := New(agent, &fakeWriter{})

	_, err := m.Send(t.Context(), "what is X?", Context{})
	require.NoError(t, err)
	answer, err := m.Send(t.Context(), "more?", Context{})

	require.NoError(t, err)
	assert.Equal(t, 1, answer.Turn)
	assert.Empty(t, agent.runs[0].History)
	assert.Equal(t, first, agent.runs[1].History)
}

func TestSendReportsTheWriteTheRunMade(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{
		Text: "tightened it", Applied: knowledge.Applied{GroupID: "g1"},
	}}}
	m := New(agent, &fakeWriter{})

	answer, err := m.Send(t.Context(), "tighten this", Context{ScopeID: "blk"})

	require.NoError(t, err)
	assert.Equal(t, knowledge.ID("g1"), answer.GroupID)
}

func TestSendReportsARunThatRanOutOfRounds(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{Exhausted: true}}}
	m := New(agent, &fakeWriter{})

	answer, err := m.Send(t.Context(), "everything about everything", Context{})

	require.NoError(t, err)
	assert.True(t, answer.Exhausted)
	assert.Empty(t, answer.Text)
}

func TestSendRefusesAnEmptyMessage(t *testing.T) {
	agent := &fakeAgent{}
	m := New(agent, &fakeWriter{})

	_, err := m.Send(t.Context(), "   ", Context{})

	assert.Equal(t, "assistant_empty", code(t, err))
	assert.Empty(t, agent.runs)
}

func TestAFailedSendLeavesTheConversationAsItWas(t *testing.T) {
	first := exchange("what is X?", "X is a thing")
	agent := &fakeAgent{results: []host.AgentResult{{Text: "X is a thing", Messages: first}, {}, {Text: "ok"}}}
	m := New(agent, &fakeWriter{})
	_, err := m.Send(t.Context(), "what is X?", Context{})
	require.NoError(t, err)

	agent.err = knowledge.ErrLLMUnavailable
	_, err = m.Send(t.Context(), "more?", Context{})
	require.ErrorIs(t, err, knowledge.ErrLLMUnavailable)

	agent.err = nil
	answer, err := m.Send(t.Context(), "more?", Context{})
	require.NoError(t, err)
	assert.Equal(t, 1, answer.Turn, "the failed send took no turn")
	assert.Equal(t, first, agent.runs[2].History)
}

func TestSendDuringARunIsRefused(t *testing.T) {
	agent := &fakeAgent{
		results: []host.AgentResult{{Text: "slow"}},
		hold:    make(chan struct{}), entered: make(chan struct{}),
	}
	m := New(agent, &fakeWriter{})
	done := make(chan error, 1)
	go func() {
		_, err := m.Send(t.Context(), "first", Context{})
		done <- err
	}()
	<-agent.entered

	_, err := m.Send(t.Context(), "second", Context{})

	assert.Equal(t, "assistant_busy", code(t, err))
	assert.Equal(t, "assistant_busy", code(t, m.Reset(t.Context())), "a reset would orphan the run in flight")
	close(agent.hold)
	require.NoError(t, <-done)
}

func TestResetStartsANewConversation(t *testing.T) {
	first := exchange("what is X?", "X is a thing")
	agent := &fakeAgent{results: []host.AgentResult{{Text: "X is a thing", Messages: first}, {Text: "fresh"}}}
	m := New(agent, &fakeWriter{})
	_, err := m.Send(t.Context(), "what is X?", Context{})
	require.NoError(t, err)

	require.NoError(t, m.Reset(t.Context()))
	answer, err := m.Send(t.Context(), "hello", Context{})

	require.NoError(t, err)
	assert.Equal(t, 0, answer.Turn)
	assert.Empty(t, agent.runs[1].History)
}

func TestSaveAnswerWritesItUnderTheTopicAwaitingReview(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{Text: "X is a thing"}}}
	writer := &fakeWriter{}
	m := New(agent, writer)
	answer, err := m.Send(t.Context(), "what is X?", Context{})
	require.NoError(t, err)

	id, err := m.SaveAnswer(t.Context(), answer.Turn, "topic")

	require.NoError(t, err)
	assert.Equal(t, knowledge.ID("blk-saved"), id)
	assert.Equal(t, knowledge.AuthorAI, writer.meta.Author, "an AI-authored batch awaits review")
	assert.Equal(t, Source, writer.meta.Source)
	assert.NotEmpty(t, writer.meta.Title)
	require.Len(t, writer.ops, 1)
	assert.Equal(t, knowledge.OpAdd, writer.ops[0].Kind)
	assert.Equal(t, "topic", writer.ops[0].Parent)
	assert.Equal(t, "X is a thing", writer.ops[0].Content)
}

func TestSaveAnswerOfAnUnknownTurnHasACode(t *testing.T) {
	writer := &fakeWriter{}
	m := New(&fakeAgent{}, writer)

	for _, turn := range []int{-1, 0, 7} {
		_, err := m.SaveAnswer(t.Context(), turn, "topic")
		assert.Equal(t, "assistant_no_answer", code(t, err), "turn %d", turn)
	}
	assert.Empty(t, writer.ops)
}

func TestSaveAnswerOfAnExhaustedTurnHasNothingToSave(t *testing.T) {
	agent := &fakeAgent{results: []host.AgentResult{{Exhausted: true}}}
	m := New(agent, &fakeWriter{})
	answer, err := m.Send(t.Context(), "everything", Context{})
	require.NoError(t, err)

	_, err = m.SaveAnswer(t.Context(), answer.Turn, "topic")

	assert.Equal(t, "assistant_no_answer", code(t, err))
}
