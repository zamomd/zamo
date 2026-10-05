package wailsbind

import (
	"context"
	"fmt"

	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
)

// AgentStepEvent is the name of the Wails event carrying the live steps of
// an agent run to the frontend. Its payload is an AgentStep.
const AgentStepEvent = "agent:step"

// AgentStep is one step of a run as the frontend reads it.
type AgentStep struct {
	Kind   string `json:"kind"`
	Tool   string `json:"tool,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// AgentRunner runs the agent; *agent.Runner in production.
type AgentRunner interface {
	Run(ctx context.Context, run agent.Run) (agent.Result, error)
}

type announcing struct {
	inner AgentRunner
	emit  func() Emitter
}

// Announce wraps a runner so that every run through it reports its steps to
// the frontend as AgentStepEvent events. The emitter is looked up per run:
// the Wails application that provides it is built after its services.
func Announce(inner AgentRunner, emit func() Emitter) AgentRunner {
	return announcing{inner: inner, emit: emit}
}

func (a announcing) Run(ctx context.Context, run agent.Run) (agent.Result, error) {
	if em := a.emit(); em != nil {
		ctx = agent.WithSteps(ctx, func(s agent.Step) {
			em.Emit(AgentStepEvent, AgentStep{Kind: string(s.Kind), Tool: s.Tool, Detail: s.Detail})
		})
	}
	return a.inner.Run(ctx, run)
}

// SetAgent wires the agent the app's own AI gestures run on. A package
// function for the reason SetEmitter is one.
func SetAgent(a *App, r AgentRunner) { a.agent = r }

// CancelAgent cancels every agent run started on the user's behalf and
// returns how many there were. A cancelled call fails with the "cancelled"
// code.
func (a *App) CancelAgent(_ context.Context) int {
	return a.jobs.CancelKind(jobs.KindAgent)
}

// ExpandTopicByID has the AI write an existing topic in the shape it has: an
// outline grows a block tree, a document is written whole. The result awaits
// review. A run that writes nothing fails as a bad response.
func (a *App) ExpandTopicByID(ctx context.Context, id knowledge.ID) (knowledge.Applied, error) {
	blocks, err := a.svc.GetSubtree(ctx, id)
	if err != nil {
		return knowledge.Applied{}, err
	}
	name := knowledge.TopicName(blocks[0].Content)
	instruction, err := llm.ExpandInstruction(name)
	if err != nil {
		return knowledge.Applied{}, err
	}
	res, err := a.agent.Run(ctx, agent.Run{
		Persona:     llm.PersonaEditor,
		Instruction: instruction,
		Write:       agent.WritePolicy{Scope: id},
		Source:      knowledge.SourceGUIExpand,
	})
	if err != nil {
		return res.Applied, err
	}
	if res.Applied.GroupID == "" {
		return knowledge.Applied{}, fmt.Errorf("expand %s: the model wrote nothing: %w", id, knowledge.ErrLLMBadResponse)
	}
	return res.Applied, nil
}
