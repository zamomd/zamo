package sqlite

import (
	"context"
	"encoding/json/jsontext"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/scheduler"
)

// Two Zamo processes on one base: each has its own scheduler over the same
// database file, and RunNow from both must run the record once.
func TestTwoSchedulersOnOneDatabaseRunARecordOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	started := make(chan string, 2)
	release := make(chan struct{})
	open := func(name string) *scheduler.Module {
		db, err := Open(t.Context(), path)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })
		ids := 0
		newID := func() string { ids++; return name + "-" + string(rune('0'+ids)) }
		reg := jobs.New(t.Context(), newID, time.Now)
		s, err := scheduler.New(NewSchedules(db, time.Now), reg, time.Now, newID, scheduler.Options{})
		require.NoError(t, err)
		s.Start(t.Context())
		m := s.For("m")
		m.Handle(func(ctx context.Context, key string, _ jsontext.Value) scheduler.RunResult {
			started <- name
			<-release
			return scheduler.RunResult{Outcome: scheduler.OutcomeOK}
		}, scheduler.HandlerOptions{})
		return m
	}
	a, b := open("a"), open("b")
	_, err := a.Put(t.Context(), "k", "0 9 * * *", true, nil)
	require.NoError(t, err)

	errA := a.RunNow(t.Context(), "k")
	errB := b.RunNow(t.Context(), "k")
	close(release)

	require.NotEqual(t, errA == nil, errB == nil,
		"RunNow from both processes = %v / %v, want exactly one to run", errA, errB)
	for _, err := range []error{errA, errB} {
		if err != nil {
			require.ErrorIs(t, err, scheduler.ErrBusy, "the refused RunNow")
		}
	}
	require.NotEmpty(t, <-started, "no run started")
	select {
	case second := <-started:
		assert.Failf(t, "second run", "a second run started in process %s", second)
	case <-time.After(200 * time.Millisecond):
	}
}
