package knowledge

import (
	"context"
	"testing"
)

func TestReportDeliversToInstalledReporter(t *testing.T) {
	var got []Progress
	ctx := WithProgress(context.Background(), func(p Progress) {
		got = append(got, p)
	})

	Report(ctx, Progress{Phase: PhaseRequesting})
	Report(ctx, Progress{Phase: PhaseParsing, Detail: "12 blocks"})

	if len(got) != 2 {
		t.Fatalf("reporter received %d reports, want 2", len(got))
	}
	if got[0].Phase != PhaseRequesting {
		t.Errorf("first phase = %q, want %q", got[0].Phase, PhaseRequesting)
	}
	if got[1].Phase != PhaseParsing || got[1].Detail != "12 blocks" {
		t.Errorf("second report = %+v, want parsing/12 blocks", got[1])
	}
}

func TestReportWithoutReporterIsNoOp(t *testing.T) {
	// A provider calls Report unconditionally; on a context with no reporter it
	// must be a silent no-op, not a panic.
	Report(context.Background(), Progress{Phase: PhaseAwaiting})
}

func TestWithProgressNilReporterIsNoOp(t *testing.T) {
	// Passing a nil reporter installs nothing, so Report stays a no-op.
	ctx := WithProgress(context.Background(), nil)
	Report(ctx, Progress{Phase: PhaseAwaiting})
}
