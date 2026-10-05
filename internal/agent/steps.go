package agent

import "context"

// StepKind names what a run is doing at one moment.
type StepKind string

// The kinds of steps a run reports.
const (
	// StepThinking is a round sent to the model.
	StepThinking StepKind = "thinking"
	// StepTool is a tool the model asked for, about to run.
	StepTool StepKind = "tool"
	// StepWrote is a write that landed in the base.
	StepWrote StepKind = "wrote"
)

// Step is one progress report of a run, shown live so a run of several
// rounds does not look frozen.
type Step struct {
	Kind StepKind
	// Tool names the tool of a StepTool.
	Tool string
	// Detail is the tool's main argument: the query, the block, the title.
	Detail string
}

type stepsKey struct{}

// WithSteps returns a context under which a run reports its steps to report.
// It is called from the goroutine running the run, so it must not block.
func WithSteps(ctx context.Context, report func(Step)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, stepsKey{}, report)
}

func step(ctx context.Context, s Step) {
	if report, ok := ctx.Value(stepsKey{}).(func(Step)); ok {
		report(s)
	}
}
