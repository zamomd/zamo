// Package agent runs the one loop every AI feature goes through: the model
// is given the base's tools, asks for some, sees their results, and repeats
// until it answers in text. A run reads the whole base and writes only what
// its caller's policy allows.
package agent

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
	"github.com/zamomd/zamo/internal/tools"
)

// MaxRounds is the most model rounds in one run.
const MaxRounds = 8

// MaxHistory is the most characters of conversation a run hands back for the
// next one. It keeps a long conversation inside a model's context with room
// for the tool results of the run to come.
const MaxHistory = 120_000

// limits keeps one tool result within a model's context: a long document
// still arrives whole, a pathological block or a huge topic does not.
var limits = tools.Limits{MaxContent: 20_000, MaxBlocks: 300}

// Base is the access to the base a run needs.
type Base interface {
	tools.Reader
	tools.Writer
	CountUnreviewedUnder(ctx context.Context, root knowledge.ID) (int, error)
}

// Run is one request to the agent.
type Run struct {
	// Persona picks the system prompt; the run's write policy is rendered
	// into it, so the model knows what it may change.
	Persona llm.Persona
	// Instruction is what to do or answer, sent as the user's turn.
	Instruction string
	// History is the conversation so far, oldest first.
	History []llm.Message
	// Write is what the run may change.
	Write WritePolicy
	// Source is recorded on the batch the run writes.
	Source knowledge.Source
	// Unattended marks a run nobody is watching. It refuses to write over
	// content that still awaits review: the owner has not judged what is
	// there, and a second change on top would bury the first.
	Unattended bool
}

// Result is how a run ended.
type Result struct {
	// Text is the model's answer; empty when the run was exhausted.
	Text string
	// Applied is the write the run made, zero if it made none. It is set
	// even when the run then failed, because the write stays.
	Applied knowledge.Applied
	// WriteErr is why the run's last write was refused, when no write of the
	// run landed: a caller with nobody watching reads the reason from it.
	WriteErr error
	// Messages is History plus this run's turns, to pass as the next run's
	// History; the oldest exchanges are dropped past MaxHistory. A failed
	// run leaves it empty.
	Messages []llm.Message
	// Exhausted reports that the round limit ended the run before the model
	// answered.
	Exhausted bool
}

// Runner runs agent loops against one base and one model.
type Runner struct {
	model   llm.Client
	base    Base
	allowed func() bool
	jobs    *jobs.Registry
}

// New assembles a runner. allowed is the AI policy, read at the start of
// every run; reg makes each run cancellable and ends it with the process.
func New(model llm.Client, base Base, allowed func() bool, reg *jobs.Registry) *Runner {
	return &Runner{model: model, base: base, allowed: allowed, jobs: reg}
}

// Run carries out one run. A tool failure is the model's to recover from and
// is not an error here; a provider failure or a cancellation is.
func (r *Runner) Run(ctx context.Context, run Run) (Result, error) {
	if !r.allowed() {
		return Result{}, knowledge.ErrAIDisabled
	}
	if run.Source == "" {
		return Result{}, fmt.Errorf("agent run: no source: %w", knowledge.ErrInvalidOps)
	}
	ctx, done := r.jobs.Start(ctx, jobs.KindAgent)
	defer done()

	// Checked here as well as inside the write: a run refused up front costs
	// no model calls, and the one inside the write catches what lands while
	// the model is thinking.
	if run.Unattended && run.Write.Scope != "" {
		n, err := r.base.CountUnreviewedUnder(ctx, run.Write.Scope)
		if err != nil {
			return Result{}, err
		}
		if n > 0 {
			return Result{}, &knowledge.UnreviewedInScopeError{Scope: run.Write.Scope, Count: n}
		}
	}

	system, err := r.system(ctx, run)
	if err != nil {
		return Result{}, err
	}

	writer := &guard{base: r.base, policy: run.Write}
	set := tools.Read(r.base, limits)
	if run.Write.Permits() {
		set = append(set, tools.Write(writer, tools.Stamp{
			Origin: string(run.Source),
			Meta: func(title string) knowledge.WriteMeta {
				meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: run.Source, Title: title}
				if run.Unattended {
					meta.RefuseOverUnreviewedIn = run.Write.Scope
				}
				return meta
			},
		}))
	}
	defs := make([]llm.ToolDef, len(set))
	for i, t := range set {
		defs[i] = llm.ToolDef{Name: t.Name, Description: t.Description, Schema: t.Schema}
	}

	msgs := append(slices.Clone(run.History), llm.Message{Role: llm.RoleUser, Text: run.Instruction})
	for range MaxRounds {
		step(ctx, Step{Kind: StepThinking})
		reply, err := r.model.Chat(ctx, llm.Request{System: system, Messages: msgs, Tools: defs})
		if err != nil {
			return writer.result(Result{}), err
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: reply.Text, Calls: reply.Calls})
		if len(reply.Calls) == 0 {
			return writer.result(Result{Text: reply.Text, Messages: trim(msgs)}), nil
		}
		for _, call := range reply.Calls {
			if err := ctx.Err(); err != nil {
				return writer.result(Result{}), err
			}
			msgs = append(msgs, r.call(ctx, set, call, writer))
		}
	}
	return writer.result(Result{Messages: trim(msgs), Exhausted: true}), nil
}

// system renders the run's system prompt around what it may change. A scope
// that no longer exists ends the run here.
func (r *Runner) system(ctx context.Context, run Run) (string, error) {
	scope := llm.Scope{NewTopic: run.Write.AllowNewTopic}
	if run.Write.Scope != "" {
		blocks, err := r.base.GetSubtree(ctx, run.Write.Scope)
		if err != nil {
			return "", err
		}
		root := blocks[0]
		scope.ID = string(root.ID)
		scope.Title, _, _ = strings.Cut(strings.TrimSpace(root.Content), "\n")
		scope.Document = root.IsTopic() && root.Mode == knowledge.ModeDoc
	}
	return llm.SystemPrompt(run.Persona, scope)
}

// trim drops whole exchanges from the front until the conversation fits
// MaxHistory. An exchange starts at a user turn and runs to the next one, so
// a tool call is never separated from its result. The latest exchange always
// stays.
func trim(msgs []llm.Message) []llm.Message {
	size := 0
	for _, m := range msgs {
		size += weight(m)
	}
	for size > MaxHistory {
		next := slices.IndexFunc(msgs[1:], func(m llm.Message) bool { return m.Role == llm.RoleUser })
		if next < 0 {
			break
		}
		for _, m := range msgs[:next+1] {
			size -= weight(m)
		}
		msgs = msgs[next+1:]
	}
	return msgs
}

func weight(m llm.Message) int {
	n := len(m.Text)
	for _, c := range m.Calls {
		n += len(c.Args)
	}
	return n
}

// call runs one tool call and returns the message carrying its result. A
// failure becomes a failed result for the model to read.
func (r *Runner) call(ctx context.Context, set []tools.Tool, call llm.ToolCall, writer *guard) llm.Message {
	result := llm.Message{Role: llm.RoleTool, CallID: call.ID}
	i := slices.IndexFunc(set, func(t tools.Tool) bool { return t.Name == call.Name })
	if i < 0 {
		result.Failed = true
		result.Text = fmt.Sprintf("no tool named %q", call.Name)
		return result
	}
	step(ctx, Step{Kind: StepTool, Tool: call.Name, Detail: detail(call.Args)})
	wrote := writer.wrote
	out, err := set[i].Call(ctx, call.Args)
	if err == nil {
		var raw []byte
		if raw, err = json.Marshal(out); err == nil {
			result.Text = string(raw)
		}
	}
	if err != nil {
		result.Failed = true
		result.Text = err.Error()
		return result
	}
	if writer.wrote && !wrote {
		step(ctx, Step{Kind: StepWrote})
	}
	return result
}

// detail picks the argument worth showing beside a tool's name.
func detail(args jsontext.Value) string {
	var a struct {
		Query string `json:"query"`
		Title string `json:"title"`
		Root  string `json:"root"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(args, &a)
	return cmp.Or(a.Query, a.Title, a.Root, a.ID)
}

var errAlreadyWrote = errors.New("this run has already written once; a run writes one batch")

// guard is the writer a run's write tool reaches: it holds the batch to the
// run's policy, lets one batch through, and remembers what it applied.
type guard struct {
	base    Base
	policy  WritePolicy
	wrote   bool
	applied knowledge.Applied
	refused error
}

// result fills in what the run wrote, or why its write did not land.
func (g *guard) result(res Result) Result {
	res.Applied = g.applied
	if !g.wrote {
		res.WriteErr = g.refused
	}
	return res
}

func (g *guard) ApplyOps(
	ctx context.Context, meta knowledge.WriteMeta, ops []knowledge.Op,
) (knowledge.Applied, error) {
	if g.wrote {
		return knowledge.Applied{}, errAlreadyWrote
	}
	if err := g.policy.Check(ctx, g.base, ops); err != nil {
		g.refused = err
		return knowledge.Applied{}, err
	}
	applied, err := g.base.ApplyOps(ctx, meta, ops)
	if err != nil {
		g.refused = err
		return knowledge.Applied{}, err
	}
	g.wrote = true
	g.applied = applied
	return applied, nil
}
