package agent_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
)

const testSource knowledge.Source = "test_agent"

// ask is an assistant run with nothing to write and no history.
func ask(instruction string) agent.Run {
	return agent.Run{Persona: llm.PersonaAssistant, Instruction: instruction, Source: testSource}
}

func withHistory(run agent.Run, history []llm.Message) agent.Run {
	run.History = history
	return run
}

func allowed(run agent.Run, policy agent.WritePolicy) agent.Run {
	run.Write = policy
	return run
}

func TestRunReturnsTheModelsTextOnTheFirstRound(t *testing.T) {
	model := &script{replies: []llm.Reply{text("nothing in the base about that")}}
	r := newRunner(t, model, openService(t))

	res, err := r.Run(t.Context(), ask("what is X?"))

	require.NoError(t, err)
	assert.Equal(t, "nothing in the base about that", res.Text)
	assert.False(t, res.Exhausted)
	assert.Empty(t, res.Applied.GroupID)
	req := model.request(0)
	assert.Contains(t, req.System, "cannot change the base")
	require.Len(t, req.Messages, 1)
	assert.Equal(t, llm.Message{Role: llm.RoleUser, Text: "what is X?"}, req.Messages[0])
	require.Len(t, res.Messages, 2, "the run's turns come back for a follow-up")
	assert.Equal(t, llm.RoleAssistant, res.Messages[1].Role)
}

func TestRunPutsHistoryBeforeTheInstruction(t *testing.T) {
	model := &script{replies: []llm.Reply{text("and so on")}}
	r := newRunner(t, model, openService(t))
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "what is X?"},
		{Role: llm.RoleAssistant, Text: "X is a thing"},
	}

	res, err := r.Run(t.Context(), withHistory(ask("more"), history))

	require.NoError(t, err)
	req := model.request(0)
	require.Len(t, req.Messages, 3)
	assert.Equal(t, history, req.Messages[:2])
	assert.Equal(t, "more", req.Messages[2].Text)
	assert.Len(t, res.Messages, 4)
	assert.Len(t, history, 2, "the caller's history is not modified")
}

func TestRunFeedsToolResultsBackInOrder(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "a deadline cancels the request")
	model := &script{replies: []llm.Reply{
		{Text: "looking", Calls: []llm.ToolCall{
			{ID: "c1", Name: "search", Args: []byte(`{"query":"deadline"}`)},
			{ID: "c2", Name: "topic_map"},
		}},
		text("it is under Go contexts"),
	}}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), ask("where are deadlines?"))

	require.NoError(t, err)
	assert.Equal(t, "it is under Go contexts", res.Text)
	require.Equal(t, 2, model.calls())
	msgs := model.request(1).Messages
	require.Len(t, msgs, 4)
	assert.Equal(t, llm.RoleAssistant, msgs[1].Role)
	assert.Len(t, msgs[1].Calls, 2)
	assert.Equal(t, llm.RoleTool, msgs[2].Role)
	assert.Equal(t, "c1", msgs[2].CallID)
	assert.Contains(t, msgs[2].Text, string(ids[1]), "the search result names the matching block")
	assert.False(t, msgs[2].Failed)
	assert.Equal(t, "c2", msgs[3].CallID)
	assert.Contains(t, msgs[3].Text, "Go contexts")
}

func TestRunStopsAtTheRoundLimit(t *testing.T) {
	model := &script{replies: []llm.Reply{toolCall("c", "topic_map", `{}`)}}
	r := newRunner(t, model, openService(t))

	res, err := r.Run(t.Context(), ask("loop"))

	require.NoError(t, err)
	assert.True(t, res.Exhausted)
	assert.Empty(t, res.Text)
	assert.Equal(t, agent.MaxRounds, model.calls())
}

func TestRunReturnsAToolFailureToTheModel(t *testing.T) {
	model := &script{replies: []llm.Reply{
		toolCall("c1", "get_subtree", `{"root":"01NOSUCHBLOCK"}`),
		text("no such block"),
	}}
	r := newRunner(t, model, openService(t))

	res, err := r.Run(t.Context(), ask("read it"))

	require.NoError(t, err)
	assert.Equal(t, "no such block", res.Text)
	result := model.request(1).Messages[2]
	assert.True(t, result.Failed)
	assert.Contains(t, result.Text, "not found")
}

func TestRunTellsTheModelAboutAnUnknownTool(t *testing.T) {
	model := &script{replies: []llm.Reply{toolCall("c1", "delete_everything", `{}`), text("ok")}}
	r := newRunner(t, model, openService(t))

	_, err := r.Run(t.Context(), ask("go"))

	require.NoError(t, err)
	result := model.request(1).Messages[2]
	assert.True(t, result.Failed)
	assert.Contains(t, result.Text, "delete_everything")
}

func TestRunOffersWriteOnlyWhenThePolicyPermitsAWrite(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts")
	cases := []struct {
		name   string
		policy agent.WritePolicy
		want   bool
	}{
		{"no policy", agent.WritePolicy{}, false},
		{"scope", agent.WritePolicy{Scope: ids[0]}, true},
		{"new topic", agent.WritePolicy{AllowNewTopic: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &script{replies: []llm.Reply{text("ok")}}
			r := newRunner(t, model, svc)

			_, err := r.Run(t.Context(), allowed(ask("go"), tc.policy))

			require.NoError(t, err)
			names := toolNames(model.request(0))
			assert.Subset(t, names, []string{"search", "get_subtree", "topic_map", "get_backlinks"})
			assert.Equal(t, tc.want, slices.Contains(names, "write_blocks"))
		})
	}
}

func TestRunAppliesAWriteInScopeAwaitingReview(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "old claim")
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks",
			`{"title":"sharpen","ops":[{"op":"edit","block_id":"`+string(ids[1])+`","content":"new claim"}]}`),
		text("done"),
	}}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "sharpen it", Write: agent.WritePolicy{Scope: ids[0]}, Source: testSource,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, res.Applied.GroupID)
	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	require.Len(t, pending.Blocks, 1, "the write awaits review")
	history, err := svc.BlockHistory(t.Context(), ids[1])
	require.NoError(t, err)
	assert.Equal(t, string(testSource), history[0].Origin)
}

func TestRunRefusesAWriteOutsideScopeToTheModel(t *testing.T) {
	svc := openService(t)
	mine := seedTopic(t, svc, "Go contexts", "claim")
	other := seedTopic(t, svc, "Rust lifetimes", "untouchable")
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks",
			`{"title":"stray","ops":[{"op":"edit","block_id":"`+string(other[1])+`","content":"touched"}]}`),
		text("could not"),
	}}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "edit the other one", Write: agent.WritePolicy{Scope: mine[0]}, Source: testSource,
	})

	require.NoError(t, err)
	assert.Empty(t, res.Applied.GroupID)
	result := model.request(1).Messages[2]
	assert.True(t, result.Failed)
	assert.Contains(t, result.Text, string(other[1]), "the refusal names the block")
	blocks, err := svc.GetSubtree(t.Context(), other[0])
	require.NoError(t, err)
	assert.Equal(t, "untouchable", blocks[1].Content)
}

func TestRunAppliesAtMostOneWrite(t *testing.T) {
	svc := openService(t)
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks", `{"title":"one","ops":[{"op":"add","temp_id":"t","content":"First"}]}`),
		toolCall("c2", "write_blocks", `{"title":"two","ops":[{"op":"add","temp_id":"t","content":"Second"}]}`),
		text("done"),
	}}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "write twice", Write: agent.WritePolicy{AllowNewTopic: true}, Source: testSource,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, res.Applied.GroupID)
	require.NoError(t, res.WriteErr, "a refusal after a write that landed is not the run's write failure")
	second := model.request(2).Messages[4]
	assert.True(t, second.Failed)
	topics, err := svc.TopicMap(t.Context())
	require.NoError(t, err)
	require.Len(t, topics, 1)
	assert.Equal(t, "First", topics[0].Content)
}

func TestRunWithAIOffNeverCallsTheModel(t *testing.T) {
	model := &script{replies: []llm.Reply{text("ok")}}
	r := agent.New(model, openService(t), func() bool { return false }, newRegistry(t))

	_, err := r.Run(t.Context(), ask("go"))

	require.ErrorIs(t, err, knowledge.ErrAIDisabled)
	assert.Zero(t, model.calls())
}

func TestRunWithoutASourceIsRefused(t *testing.T) {
	model := &script{replies: []llm.Reply{text("ok")}}
	r := newRunner(t, model, openService(t))

	_, err := r.Run(t.Context(), agent.Run{Persona: llm.PersonaAssistant, Instruction: "go"})

	require.ErrorIs(t, err, knowledge.ErrInvalidOps)
	assert.Zero(t, model.calls())
}

func aiEdit(t *testing.T, svc *knowledge.Service, id knowledge.ID, content string) {
	t.Helper()
	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "earlier"}
	_, err := svc.ApplyOps(context.WithoutCancel(t.Context()), meta,
		[]knowledge.Op{{Kind: knowledge.OpEdit, BlockID: id, Content: content}})
	require.NoError(t, err)
}

func TestUnattendedRunStopsBeforeTheModelWhenScopeAwaitsReview(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "claim")
	aiEdit(t, svc, ids[1], "unreviewed claim")
	model := &script{replies: []llm.Reply{text("ok")}}
	r := newRunner(t, model, svc)

	_, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "refresh", Write: agent.WritePolicy{Scope: ids[0]}, Source: testSource, Unattended: true,
	})

	pending, ok := errors.AsType[*knowledge.UnreviewedInScopeError](err)
	require.True(t, ok, "want UnreviewedInScopeError, got %v", err)
	assert.Equal(t, 1, pending.Count)
	assert.Zero(t, model.calls())
}

func TestUnattendedWriteIsRefusedOverContentThatBecameUnreviewed(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "claim")
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks",
			`{"title":"refresh","ops":[{"op":"edit","block_id":"`+string(ids[1])+`","content":"fresh"}]}`),
		text("could not"),
	}}
	model.before = func(_ context.Context, n int) error {
		if n == 0 {
			aiEdit(t, svc, ids[1], "landed while the model was thinking")
		}
		return nil
	}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "refresh", Write: agent.WritePolicy{Scope: ids[0]}, Source: testSource, Unattended: true,
	})

	require.NoError(t, err)
	assert.Empty(t, res.Applied.GroupID)
	assert.True(t, model.request(1).Messages[2].Failed)
	pending, ok := errors.AsType[*knowledge.UnreviewedInScopeError](res.WriteErr)
	require.True(t, ok, "the refusal is reported to the caller, got %v", res.WriteErr)
	assert.Equal(t, 1, pending.Count)
}

func TestAttendedWriteStacksOnUnreviewedContent(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "claim")
	aiEdit(t, svc, ids[1], "unreviewed claim")
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks",
			`{"title":"again","ops":[{"op":"edit","block_id":"`+string(ids[1])+`","content":"second"}]}`),
		text("done"),
	}}
	r := newRunner(t, model, svc)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "again", Write: agent.WritePolicy{Scope: ids[0]}, Source: testSource,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, res.Applied.GroupID)
}

func TestCancelDuringARoundKeepsAnAppliedWrite(t *testing.T) {
	svc := openService(t)
	reg := newRegistry(t)
	model := &script{replies: []llm.Reply{
		toolCall("c1", "write_blocks", `{"title":"one","ops":[{"op":"add","temp_id":"t","content":"Kept"}]}`),
		text("never reached"),
	}}
	model.before = func(ctx context.Context, n int) error {
		if n == 1 {
			reg.CancelKind(jobs.KindAgent)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	r := agent.New(model, svc, func() bool { return true }, reg)

	res, err := r.Run(t.Context(), agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "write then think", Write: agent.WritePolicy{AllowNewTopic: true}, Source: testSource,
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.NotEmpty(t, res.Applied.GroupID, "the write that landed is still reported")
	assert.Empty(t, res.Messages, "a broken run contributes nothing to the conversation")
	topics, mapErr := svc.TopicMap(context.WithoutCancel(t.Context()))
	require.NoError(t, mapErr)
	assert.Len(t, topics, 1)
	assert.Empty(t, reg.Running(), "the job is released")
}

func TestRunReportsItsSteps(t *testing.T) {
	svc := openService(t)
	model := &script{replies: []llm.Reply{
		toolCall("c1", "search", `{"query":"deadline"}`),
		toolCall("c2", "write_blocks", `{"title":"note","ops":[{"op":"add","temp_id":"t","content":"Note"}]}`),
		text("done"),
	}}
	r := newRunner(t, model, svc)
	var steps []agent.Step
	ctx := agent.WithSteps(t.Context(), func(s agent.Step) { steps = append(steps, s) })

	_, err := r.Run(ctx, agent.Run{
		Persona:     llm.PersonaAssistant,
		Instruction: "note it", Write: agent.WritePolicy{AllowNewTopic: true}, Source: testSource,
	})

	require.NoError(t, err)
	assert.Equal(t, []agent.Step{
		{Kind: agent.StepThinking},
		{Kind: agent.StepTool, Tool: "search", Detail: "deadline"},
		{Kind: agent.StepThinking},
		{Kind: agent.StepTool, Tool: "write_blocks", Detail: "note"},
		{Kind: agent.StepWrote},
		{Kind: agent.StepThinking},
	}, steps)
}

func TestRunTellsTheModelWhatItMayChange(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "a claim worth sharpening\nwith a second line")
	model := &script{replies: []llm.Reply{text("ok")}}
	r := newRunner(t, model, svc)

	_, err := r.Run(t.Context(), agent.Run{
		Persona: llm.PersonaEditor, Instruction: "sharpen", Write: agent.WritePolicy{Scope: ids[1]}, Source: testSource,
	})

	require.NoError(t, err)
	system := model.request(0).System
	assert.Contains(t, system, string(ids[1]))
	assert.Contains(t, system, "a claim worth sharpening")
	assert.NotContains(t, system, "with a second line", "a block is named by its first line")
}

func TestRunOverAMissingScopeNeverCallsTheModel(t *testing.T) {
	model := &script{replies: []llm.Reply{text("ok")}}
	r := newRunner(t, model, openService(t))

	_, err := r.Run(t.Context(), agent.Run{
		Persona: llm.PersonaEditor, Instruction: "go", Write: agent.WritePolicy{Scope: "01GONE"}, Source: testSource,
	})

	require.ErrorIs(t, err, knowledge.ErrNotFound)
	assert.Zero(t, model.calls())
}

func TestRunWithoutAPersonaIsRefused(t *testing.T) {
	model := &script{replies: []llm.Reply{text("ok")}}
	r := newRunner(t, model, openService(t))

	_, err := r.Run(t.Context(), agent.Run{Instruction: "go", Source: testSource})

	require.Error(t, err)
	assert.Zero(t, model.calls())
}

func TestRunDropsTheOldestExchangesPastTheHistoryBudget(t *testing.T) {
	model := &script{replies: []llm.Reply{text("short answer")}}
	r := newRunner(t, model, openService(t))
	big := strings.Repeat("x", agent.MaxHistory/2)
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "first question"},
		{Role: llm.RoleAssistant, Calls: []llm.ToolCall{{ID: "c1", Name: "search", Args: []byte(`{"query":"q"}`)}}},
		{Role: llm.RoleTool, CallID: "c1", Text: big},
		{Role: llm.RoleAssistant, Text: "first answer"},
		{Role: llm.RoleUser, Text: "second question"},
		{Role: llm.RoleAssistant, Text: big},
	}

	res, err := r.Run(t.Context(), agent.Run{
		Persona: llm.PersonaAssistant, Instruction: "third question", History: history, Source: testSource,
	})

	require.NoError(t, err)
	require.Len(t, res.Messages, 4, "the first exchange is dropped whole, tool turns included")
	assert.Equal(t, "second question", res.Messages[0].Text)
	assert.Equal(t, "short answer", res.Messages[3].Text)
}

func TestRunKeepsTheLatestExchangeHoweverLarge(t *testing.T) {
	big := strings.Repeat("x", agent.MaxHistory*2)
	model := &script{replies: []llm.Reply{text(big)}}
	r := newRunner(t, model, openService(t))

	res, err := r.Run(t.Context(), agent.Run{
		Persona: llm.PersonaAssistant, Instruction: "tell me everything", Source: testSource,
		History: []llm.Message{{Role: llm.RoleUser, Text: "earlier"}, {Role: llm.RoleAssistant, Text: "reply"}},
	})

	require.NoError(t, err)
	require.Len(t, res.Messages, 2)
	assert.Equal(t, "tell me everything", res.Messages[0].Text)
}
