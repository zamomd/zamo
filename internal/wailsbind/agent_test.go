package wailsbind

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
	"github.com/zamomd/zamo/internal/sqlite"
	"github.com/zamomd/zamo/internal/ulid"
)

func openService(t *testing.T) *knowledge.Service {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "zamo.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	return knowledge.NewService(sqlite.NewRepo(db, time.Now, newID), newID)
}

type recordingAgent struct {
	got agent.Run
	res agent.Result
	err error
}

func (r *recordingAgent) Run(_ context.Context, run agent.Run) (agent.Result, error) {
	r.got = run
	return r.res, r.err
}

func TestExpandTopicByIDRunsAnEditorOverTheTopic(t *testing.T) {
	svc := openService(t)
	id, err := svc.CreateTopic(t.Context(), "Go contexts", knowledge.ModeOutline)
	require.NoError(t, err)
	runner := &recordingAgent{res: agent.Result{Applied: knowledge.Applied{GroupID: "g1"}}}
	app := New(svc, nil, nil)
	SetAgent(app, runner)

	applied, err := app.ExpandTopicByID(t.Context(), id)

	require.NoError(t, err)
	assert.Equal(t, knowledge.ID("g1"), applied.GroupID)
	assert.Equal(t, llm.PersonaEditor, runner.got.Persona)
	assert.Equal(t, agent.WritePolicy{Scope: id}, runner.got.Write)
	assert.Equal(t, knowledge.SourceGUIExpand, runner.got.Source)
	assert.False(t, runner.got.Unattended)
	assert.Contains(t, runner.got.Instruction, "Go contexts")
}

func TestExpandTopicByIDThatWroteNothingIsABadResponse(t *testing.T) {
	svc := openService(t)
	id, err := svc.CreateTopic(t.Context(), "Go contexts", knowledge.ModeOutline)
	require.NoError(t, err)
	app := New(svc, nil, nil)
	SetAgent(app, &recordingAgent{res: agent.Result{Text: "I would rather not"}})

	_, err = app.ExpandTopicByID(t.Context(), id)

	assert.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
}

func TestExpandTopicByIDOfAMissingTopicIsNotFound(t *testing.T) {
	app := New(openService(t), nil, nil)
	runner := &recordingAgent{}
	SetAgent(app, runner)

	_, err := app.ExpandTopicByID(t.Context(), "01GONE")

	require.ErrorIs(t, err, knowledge.ErrNotFound)
	assert.Empty(t, runner.got.Instruction, "the agent was not run")
}

// hangingModel behaves like a provider that never answers: a chat waits for
// its context to end and reports the transport failure a real provider wraps.
type hangingModel struct{ started chan struct{} }

func (h hangingModel) Chat(ctx context.Context, _ llm.Request) (llm.Reply, error) {
	close(h.started)
	<-ctx.Done()
	return llm.Reply{}, context.Cause(ctx)
}

func (h hangingModel) Ping(context.Context) error { return nil }

func TestCancelAgentInterruptsARunningExpansion(t *testing.T) {
	svc := openService(t)
	id, err := svc.CreateTopic(t.Context(), "Go contexts", knowledge.ModeOutline)
	require.NoError(t, err)
	reg := jobs.New(t.Context(), ulid.New, time.Now)
	model := hangingModel{started: make(chan struct{})}
	app := New(svc, nil, reg)
	SetAgent(app, agent.New(model, svc, func() bool { return true }, reg))

	result := make(chan error, 1)
	go func() {
		_, err := app.ExpandTopicByID(t.Context(), id)
		result <- err
	}()

	<-model.started
	require.Equal(t, 1, app.CancelAgent(t.Context()))

	select {
	case err := <-result:
		require.Equal(t, "cancelled", unmarshalPayload(t, MarshalError(err)).Code, "error: %v", err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the expansion did not return after cancellation")
	}
	assert.Zero(t, app.CancelAgent(t.Context()), "job still registered after it returned")
}

func TestCancelAgentWithNothingInFlight(t *testing.T) {
	app := New(nil, nil, jobs.New(t.Context(), func() string { return "job" }, time.Now))

	assert.Zero(t, app.CancelAgent(t.Context()))
}

// oneTool plays a run of one search and an answer.
type oneTool struct{}

func (oneTool) Chat(_ context.Context, r llm.Request) (llm.Reply, error) {
	if r.Messages[len(r.Messages)-1].Role == llm.RoleUser {
		return llm.Reply{Calls: []llm.ToolCall{{ID: "c1", Name: "search", Args: []byte(`{"query":"go"}`)}}}, nil
	}
	return llm.Reply{Text: "done"}, nil
}

func (oneTool) Ping(context.Context) error { return nil }

func TestAnnounceForwardsARunsStepsAsEvents(t *testing.T) {
	svc := openService(t)
	reg := jobs.New(t.Context(), ulid.New, time.Now)
	fe := &fakeEmitter{}
	runner := Announce(agent.New(oneTool{}, svc, func() bool { return true }, reg), func() Emitter { return fe })

	_, err := runner.Run(t.Context(), agent.Run{
		Persona: llm.PersonaAssistant, Instruction: "find go", Source: "test",
	})

	require.NoError(t, err)
	var steps []AgentStep
	for _, e := range fe.events {
		assert.Equal(t, AgentStepEvent, e.name)
		require.Len(t, e.data, 1)
		step, ok := e.data[0].(AgentStep)
		require.True(t, ok, "payload type = %T, want AgentStep", e.data[0])
		steps = append(steps, step)
	}
	assert.Equal(t, []AgentStep{
		{Kind: "thinking"},
		{Kind: "tool", Tool: "search", Detail: "go"},
		{Kind: "thinking"},
	}, steps)
}

func TestAnnounceWithoutAnEmitterStillRuns(t *testing.T) {
	inner := &recordingAgent{res: agent.Result{Text: "ok"}}
	runner := Announce(inner, func() Emitter { return nil })

	res, err := runner.Run(t.Context(), agent.Run{Instruction: "go"})

	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text)
}
