package knowledge

import "context"

// GenerationPhase names a coarse stage of an LLM generation, reported live so
// the GUI can show that work is happening rather than freezing on a spinner.
type GenerationPhase string

// The generation phases every provider can report.
const (
	PhaseRequesting GenerationPhase = "requesting" // rendering the prompt, building the request
	PhaseAwaiting   GenerationPhase = "awaiting"   // request sent, waiting on the model
	PhaseParsing    GenerationPhase = "parsing"    // reply received, parsing into ops
)

// Progress is a single generation-progress report.
type Progress struct {
	Phase GenerationPhase
}

// ProgressReporter receives progress reports during a generation. It is called
// from the goroutine running the provider request, so it must not block.
type ProgressReporter func(Progress)

type progressKey struct{}

// WithProgress returns a context carrying reporter, so a provider running under
// it can Report generation progress without the provider interface depending on
// the GUI. A nil reporter installs nothing.
func WithProgress(ctx context.Context, reporter ProgressReporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, reporter)
}

// Report delivers one report to the reporter installed by WithProgress. With no
// reporter in ctx it is a no-op, so providers can call it unconditionally.
func Report(ctx context.Context, p Progress) {
	if reporter, ok := ctx.Value(progressKey{}).(ProgressReporter); ok {
		reporter(p)
	}
}
