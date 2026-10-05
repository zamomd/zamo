// Package smartcron keeps a block fresh on a schedule: the owner saves an AI
// instruction and a cron expression on a block, and the module runs that
// instruction as an agent run over that block when the time comes. Every
// result is an AI-authored change awaiting review; the module never writes
// on its own authority.
package smartcron

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// Name is the module's name with the scheduler.
const Name = "smartcron"

// Source marks the revisions a scheduled run writes.
const Source knowledge.Source = "smart_cron"

const payloadVersion = 1

// payload is what the module keeps in its schedule records. The version lets
// a later build read what an earlier one wrote, and an earlier build refuse
// what it cannot.
type payload struct {
	V           int    `json:"v"`
	Instruction string `json:"instruction"`
}

// coded is a sentinel with a wire code (see host.Coded).
type coded struct{ code, msg string }

func (e coded) Error() string { return e.msg }
func (e coded) Code() string  { return e.code }

// The module's own failures, each with the wire code the frontend shows a
// sentence for.
var (
	ErrEmptyInstruction error = coded{"schedule_empty_instruction", "the instruction is empty"}
	ErrBusy             error = coded{"schedule_busy", "a scheduled run is already in progress"}
	ErrNoSchedule       error = coded{"schedule_missing", "this block has no schedule"}
)

// View is a block's schedule as the panel shows it. Exists is false when the
// block has none; the other fields are then empty.
type View struct {
	Exists      bool      `json:"exists"`
	Instruction string    `json:"instruction"`
	Spec        string    `json:"spec"`
	Enabled     bool      `json:"enabled"`
	Running     bool      `json:"running"`
	NextRun     time.Time `json:"nextRun,omitzero"`
	LastRun     time.Time `json:"lastRun,omitzero"`
	Outcome     string    `json:"outcome,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

// Preview is what the form shows before saving: the next firing times of an
// expression and the zone they are in.
type Preview struct {
	Next []time.Time `json:"next"`
	Zone string      `json:"zone"`
}

// Module is the feature. Its exported methods are bound into the desktop
// frontend as the schedule service.
type Module struct {
	sched host.Scheduler
	agent host.Agent
	base  host.BaseReader
}

// New builds the module and registers its handler. Runs go one at a time:
// each is an LLM call, and a burst after a long absence must not be one.
func New(sched host.Scheduler, agent host.Agent, base host.BaseReader) *Module {
	m := &Module{sched: sched, agent: agent, base: base}
	sched.Handle(m.run, host.ScheduleOptions{MaxConcurrent: 1})
	return m
}

// run carries out one scheduled refresh and translates how it went into an
// outcome the list can show.
func (m *Module) run(ctx context.Context, key string, raw jsontext.Value) host.RunResult {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return failed("unreadable payload: " + err.Error())
	}
	if p.V > payloadVersion {
		return failed(fmt.Sprintf("payload version %d is newer than this build understands", p.V))
	}
	res, err := m.agent.Run(ctx, host.AgentRun{
		Persona:     host.PersonaEditor,
		Instruction: p.Instruction,
		Write:       host.WritePolicy{Scope: knowledge.ID(key)},
		Source:      Source,
		Unattended:  true,
	})
	// A write that landed is the outcome, whatever became of the run after it.
	if res.Applied.GroupID != "" {
		return host.RunResult{Outcome: host.OutcomeOK}
	}
	if pending, ok := errors.AsType[*knowledge.UnreviewedInScopeError](cmp.Or(err, res.WriteErr)); ok {
		return skipped(fmt.Sprintf("%d unreviewed changes in scope", pending.Count))
	}
	switch {
	case err == nil && res.Exhausted:
		return failed("the model did not finish within its rounds")
	case err == nil:
		return host.RunResult{Outcome: host.OutcomeNoChange}
	case errors.Is(err, knowledge.ErrNotFound):
		return skipped("target missing")
	case errors.Is(err, knowledge.ErrAIDisabled):
		return skipped("AI is off")
	default:
		return failed(err.Error())
	}
}

func failed(detail string) host.RunResult {
	return host.RunResult{Outcome: host.OutcomeFailed, Detail: detail}
}

func skipped(detail string) host.RunResult {
	return host.RunResult{Outcome: host.OutcomeSkipped, Detail: detail}
}

// Get returns the block's schedule, or a View with Exists false.
func (m *Module) Get(ctx context.Context, blockID knowledge.ID) (View, error) {
	s, err := m.sched.Get(ctx, string(blockID))
	if errors.Is(err, host.ErrScheduleNotFound) {
		return View{}, nil
	}
	if err != nil {
		return View{}, err
	}
	return view(s), nil
}

// Save creates or replaces the block's schedule. The block must exist; a
// document topic is scheduled as a whole, which is what its root block is.
func (m *Module) Save(
	ctx context.Context, blockID knowledge.ID, instruction, spec string, enabled bool,
) (View, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return View{}, ErrEmptyInstruction
	}
	if _, err := m.base.GetSubtree(ctx, blockID); err != nil {
		return View{}, err
	}
	raw, err := json.Marshal(payload{V: payloadVersion, Instruction: instruction})
	if err != nil {
		return View{}, err
	}
	s, err := m.sched.Put(ctx, string(blockID), spec, enabled, raw)
	if err != nil {
		return View{}, translate(err)
	}
	return view(s), nil
}

// SetEnabled switches the block's schedule on or off, keeping the rest.
func (m *Module) SetEnabled(ctx context.Context, blockID knowledge.ID, enabled bool) (View, error) {
	s, err := m.sched.Get(ctx, string(blockID))
	if err != nil {
		return View{}, translate(err)
	}
	s, err = m.sched.Put(ctx, string(blockID), s.Spec, enabled, s.Payload)
	if err != nil {
		return View{}, translate(err)
	}
	return view(s), nil
}

// Remove deletes the block's schedule; a block without one is left as is.
func (m *Module) Remove(ctx context.Context, blockID knowledge.ID) error {
	return m.sched.Delete(ctx, string(blockID))
}

// RunNow runs the block's schedule at once.
func (m *Module) RunNow(ctx context.Context, blockID knowledge.ID) error {
	return translate(m.sched.RunNow(ctx, string(blockID)))
}

// Cancel stops the block's run in flight, if any.
func (m *Module) Cancel(ctx context.Context, blockID knowledge.ID) error {
	m.sched.Cancel(ctx, string(blockID))
	return nil
}

// Preview validates spec and returns its next three runs.
func (m *Module) Preview(_ context.Context, spec string) (Preview, error) {
	next, err := m.sched.Preview(spec, 3)
	if err != nil {
		return Preview{}, translate(err)
	}
	return Preview{Next: next, Zone: zoneName(time.Now())}, nil
}

// zoneName is the machine's zone as the form labels it: "CEST (UTC+02:00)".
func zoneName(at time.Time) string {
	abbr, offset := at.Zone()
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	return fmt.Sprintf("%s (UTC%s%02d:%02d)", abbr, sign, offset/3600, offset%3600/60)
}

// translate gives the scheduler's errors the module's wire codes, keeping the
// parser's message on an invalid expression since the form shows it.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, host.ErrInvalidSpec):
		return coded{"schedule_invalid_spec", strings.TrimPrefix(err.Error(), host.ErrInvalidSpec.Error()+": ")}
	case errors.Is(err, host.ErrScheduleBusy):
		return ErrBusy
	case errors.Is(err, host.ErrScheduleNotFound):
		return ErrNoSchedule
	default:
		return err
	}
}

func view(s host.Schedule) View {
	var p payload
	_ = json.Unmarshal(s.Payload, &p)
	return View{
		Exists: true, Instruction: p.Instruction, Spec: s.Spec, Enabled: s.Enabled, Running: s.Running,
		NextRun: s.NextRun, LastRun: s.LastRun, Outcome: string(s.Outcome), Detail: s.Detail,
	}
}
