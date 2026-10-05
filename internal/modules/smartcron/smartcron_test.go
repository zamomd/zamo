package smartcron

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// fakeScheduler records what the module registers and saves.
type fakeScheduler struct {
	handler host.ScheduleHandler
	opts    host.ScheduleOptions
	records map[string]host.Schedule
	putErr  error
	ran     []string
	runErr  error
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{records: make(map[string]host.Schedule)}
}

func (f *fakeScheduler) Handle(h host.ScheduleHandler, opts host.ScheduleOptions) {
	f.handler, f.opts = h, opts
}

func (f *fakeScheduler) Put(
	_ context.Context, key, spec string, enabled bool, payload jsontext.Value,
) (host.Schedule, error) {
	if f.putErr != nil {
		return host.Schedule{}, f.putErr
	}
	s := host.Schedule{Key: key, Spec: spec, Enabled: enabled, Payload: payload, NextRun: time.Unix(1, 0)}
	f.records[key] = s
	return s, nil
}

func (f *fakeScheduler) Get(_ context.Context, key string) (host.Schedule, error) {
	s, ok := f.records[key]
	if !ok {
		return host.Schedule{}, host.ErrScheduleNotFound
	}
	return s, nil
}

func (f *fakeScheduler) Delete(_ context.Context, key string) error {
	delete(f.records, key)
	return nil
}

func (f *fakeScheduler) List(context.Context) ([]host.Schedule, error) { return nil, nil }

func (f *fakeScheduler) RunNow(_ context.Context, key string) error {
	f.ran = append(f.ran, key)
	return f.runErr
}

func (f *fakeScheduler) Cancel(context.Context, string) bool { return true }

func (f *fakeScheduler) Preview(spec string, n int) ([]time.Time, error) {
	if spec == "bad" {
		return nil, errors.Join(host.ErrInvalidSpec, errors.New("cron expression \"bad\" is not valid"))
	}
	out := make([]time.Time, n)
	for i := range n {
		out[i] = time.Unix(int64(i+1), 0)
	}
	return out, nil
}

type fakeAgent struct {
	got host.AgentRun
	res host.AgentResult
	err error
}

func (f *fakeAgent) Run(_ context.Context, run host.AgentRun) (host.AgentResult, error) {
	f.got = run
	return f.res, f.err
}

type fakeBase struct {
	blocks map[knowledge.ID]knowledge.Block
}

func (f fakeBase) TopicMap(context.Context) ([]knowledge.Block, error) { return nil, nil }

func (f fakeBase) GetSubtree(_ context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	b, ok := f.blocks[root]
	if !ok {
		return nil, knowledge.ErrNotFound
	}
	return []knowledge.Block{b}, nil
}

func fixture() (*Module, *fakeScheduler, *fakeAgent) {
	sched := newFakeScheduler()
	editor := &fakeAgent{res: host.AgentResult{Applied: knowledge.Applied{GroupID: "g1"}}}
	base := fakeBase{blocks: map[knowledge.ID]knowledge.Block{"blk": {ID: "blk", Content: "Go releases"}}}
	return New(sched, editor, base), sched, editor
}

func TestNewRegistersOneRunAtATime(t *testing.T) {
	_, sched, _ := fixture()
	assert.NotNil(t, sched.handler)
	assert.Equal(t, 1, sched.opts.MaxConcurrent)
}

func TestSaveStoresTheInstructionAsVersionedPayload(t *testing.T) {
	m, sched, _ := fixture()

	view, err := m.Save(t.Context(), "blk", "  refresh the release list ", "0 9 * * 1", true)
	require.NoError(t, err)
	assert.JSONEq(t, `{"v":1,"instruction":"refresh the release list"}`, string(sched.records["blk"].Payload))
	assert.True(t, view.Exists)
	assert.Equal(t, "refresh the release list", view.Instruction)
	assert.Equal(t, "0 9 * * 1", view.Spec)
	assert.True(t, view.Enabled)
}

func TestSaveRefusesAnEmptyInstruction(t *testing.T) {
	m, _, _ := fixture()

	_, err := m.Save(t.Context(), "blk", "   ", "0 9 * * 1", true)

	var coded host.Coded
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "schedule_empty_instruction", coded.Code())
}

func TestSaveRefusesAMissingBlock(t *testing.T) {
	m, _, _ := fixture()

	_, err := m.Save(t.Context(), "nope", "refresh", "0 9 * * 1", true)

	assert.ErrorIs(t, err, knowledge.ErrNotFound)
}

func TestInvalidSpecPassesThroughWithACode(t *testing.T) {
	m, sched, _ := fixture()
	sched.putErr = errors.Join(host.ErrInvalidSpec, errors.New("cron expression \"bad\" is not valid"))

	_, saveErr := m.Save(t.Context(), "blk", "refresh", "bad", true)
	_, previewErr := m.Preview(t.Context(), "bad")

	for _, err := range []error{saveErr, previewErr} {
		var coded host.Coded
		if assert.ErrorAs(t, err, &coded) {
			assert.Equal(t, "schedule_invalid_spec", coded.Code())
			assert.ErrorContains(t, err, "not valid", "want the parser's message")
		}
	}
}

func TestPreviewNamesTheZone(t *testing.T) {
	m, _, _ := fixture()

	got, err := m.Preview(t.Context(), "0 9 * * *")

	require.NoError(t, err)
	assert.Len(t, got.Next, 3)
	assert.NotEmpty(t, got.Zone)
}

func TestGetWithoutAScheduleSaysSo(t *testing.T) {
	m, _, _ := fixture()

	view, err := m.Get(t.Context(), "blk")

	require.NoError(t, err)
	assert.False(t, view.Exists)
}

func TestRemoveOfAMissingScheduleIsFine(t *testing.T) {
	m, _, _ := fixture()
	assert.NoError(t, m.Remove(t.Context(), "blk"))
}

func run(t *testing.T, sched *fakeScheduler, payload string) host.RunResult {
	t.Helper()
	return sched.handler(t.Context(), "blk", jsontext.Value(payload))
}

func TestRunOutcomes(t *testing.T) {
	wrote := host.AgentResult{Applied: knowledge.Applied{GroupID: "g"}}
	pending := &knowledge.UnreviewedInScopeError{Scope: "blk", Count: 3}
	cases := []struct {
		name   string
		res    host.AgentResult
		err    error
		want   host.Outcome
		detail string
	}{
		{"applied", wrote, nil, host.OutcomeOK, ""},
		{"nothing changed", host.AgentResult{}, nil, host.OutcomeNoChange, ""},
		{"pending review", host.AgentResult{}, pending, host.OutcomeSkipped, "3 unreviewed changes in scope"},
		{
			"review became pending during the run",
			host.AgentResult{WriteErr: pending},
			nil,
			host.OutcomeSkipped, "3 unreviewed changes in scope",
		},
		{"block gone", host.AgentResult{}, knowledge.ErrNotFound, host.OutcomeSkipped, "target missing"},
		{"AI off", host.AgentResult{}, knowledge.ErrAIDisabled, host.OutcomeSkipped, "AI is off"},
		{
			"provider down",
			host.AgentResult{},
			knowledge.ErrLLMUnavailable, host.OutcomeFailed,
			knowledge.ErrLLMUnavailable.Error(),
		},
		{"ran out of rounds", host.AgentResult{Exhausted: true}, nil, host.OutcomeFailed, "did not finish"},
		{"wrote, then ran out of rounds", host.AgentResult{Applied: wrote.Applied, Exhausted: true}, nil, host.OutcomeOK, ""},
		{"wrote, then the provider failed", wrote, knowledge.ErrLLMUnavailable, host.OutcomeOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, sched, editor := fixture()
			editor.res, editor.err = c.res, c.err

			got := run(t, sched, `{"v":1,"instruction":"refresh"}`)

			assert.Equal(t, c.want, got.Outcome)
			assert.Contains(t, got.Detail, c.detail)
			want := host.AgentRun{
				Persona:     host.PersonaEditor,
				Instruction: "refresh",
				Write:       host.WritePolicy{Scope: "blk"},
				Source:      Source,
				Unattended:  true,
			}
			assert.Equal(t, want, editor.got)
		})
	}
}

func TestRunRefusesANewerPayload(t *testing.T) {
	_, sched, editor := fixture()

	got := run(t, sched, `{"v":2,"instruction":"refresh"}`)

	assert.Equal(t, host.OutcomeFailed, got.Outcome)
	assert.Contains(t, got.Detail, "version 2")
	assert.Empty(t, editor.got.Instruction, "the model was called for a payload this build cannot read")
}

func TestRunNowAndCancelReachTheScheduler(t *testing.T) {
	m, sched, _ := fixture()
	_, err := m.Save(t.Context(), "blk", "refresh", "0 9 * * 1", true)
	require.NoError(t, err)

	assert.NoError(t, m.RunNow(t.Context(), "blk"))
	assert.Len(t, sched.ran, 1)
	assert.NoError(t, m.Cancel(t.Context(), "blk"))
}

func TestRunNowWhileBusyHasACode(t *testing.T) {
	m, sched, _ := fixture()
	sched.runErr = host.ErrScheduleBusy

	err := m.RunNow(t.Context(), "blk")

	var coded host.Coded
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "schedule_busy", coded.Code())
}

func TestSetEnabledKeepsTheInstructionAndSpec(t *testing.T) {
	m, sched, _ := fixture()
	_, err := m.Save(t.Context(), "blk", "refresh", "0 9 * * 1", true)
	require.NoError(t, err)

	view, err := m.SetEnabled(t.Context(), "blk", false)
	require.NoError(t, err)
	rec := sched.records["blk"]
	assert.False(t, rec.Enabled)
	assert.Equal(t, "0 9 * * 1", rec.Spec)
	assert.JSONEq(t, `{"v":1,"instruction":"refresh"}`, string(rec.Payload))
	assert.False(t, view.Enabled)
	assert.Equal(t, "refresh", view.Instruction)
}

func TestSetEnabledWithoutAScheduleHasACode(t *testing.T) {
	m, _, _ := fixture()

	_, err := m.SetEnabled(t.Context(), "blk", true)

	var coded host.Coded
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "schedule_missing", coded.Code())
}
