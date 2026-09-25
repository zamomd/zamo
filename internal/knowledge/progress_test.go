package knowledge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportDeliversToInstalledReporter(t *testing.T) {
	var got []Progress
	ctx := WithProgress(context.Background(), func(p Progress) {
		got = append(got, p)
	})

	Report(ctx, Progress{Phase: PhaseRequesting})
	Report(ctx, Progress{Phase: PhaseParsing})

	require.Len(t, got, 2)
	assert.Equal(t, PhaseRequesting, got[0].Phase)
	assert.Equal(t, PhaseParsing, got[1].Phase)
}

func TestReportWithoutReporterIsNoOp(t *testing.T) {
	Report(context.Background(), Progress{Phase: PhaseAwaiting})
}

func TestWithProgressNilReporterIsNoOp(t *testing.T) {
	ctx := WithProgress(context.Background(), nil)
	Report(ctx, Progress{Phase: PhaseAwaiting})
}
