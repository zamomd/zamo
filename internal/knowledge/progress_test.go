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
	Report(ctx, Progress{Phase: PhaseParsing})

	if len(got) != 2 {
		t.Fatalf("reporter received %d reports, want 2", len(got))
	}
	if got[0].Phase != PhaseRequesting {
		t.Errorf("first phase = %q, want %q", got[0].Phase, PhaseRequesting)
	}
	if got[1].Phase != PhaseParsing {
		t.Errorf("second phase = %q, want %q", got[1].Phase, PhaseParsing)
	}
}

func TestReportWithoutReporterIsNoOp(t *testing.T) {
	Report(context.Background(), Progress{Phase: PhaseAwaiting})
}

func TestWithProgressNilReporterIsNoOp(t *testing.T) {
	ctx := WithProgress(context.Background(), nil)
	Report(ctx, Progress{Phase: PhaseAwaiting})
}
