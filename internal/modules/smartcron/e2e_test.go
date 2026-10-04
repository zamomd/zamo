package smartcron_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/smartcron"
	"github.com/zamomd/zamo/internal/scheduler"
	"github.com/zamomd/zamo/internal/sqlite"
)

// scriptedLLM answers every edit with the content it is told to.
type scriptedLLM struct {
	mu      sync.Mutex
	content string
	calls   int
}

func (l *scriptedLLM) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func (l *scriptedLLM) GenerateTree(context.Context, string, string) ([]knowledge.Op, error) {
	return nil, errors.New("not scripted")
}

func (l *scriptedLLM) GenerateEdit(_ context.Context, scope []knowledge.Block, _ string) ([]knowledge.Op, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return []knowledge.Op{{Kind: knowledge.OpEdit, BlockID: scope[0].ID, Content: l.content}}, nil
}

func (l *scriptedLLM) GenerateDocument(context.Context, string, string) (string, error) {
	return "", errors.New("not scripted")
}

func (l *scriptedLLM) GenerateDocumentEdit(context.Context, knowledge.Block, string) (string, error) {
	return "", errors.New("not scripted")
}

func (l *scriptedLLM) Ping(context.Context) error { return nil }

// The whole path on a real database: a schedule on a block, a run that writes
// an unreviewed revision, a second run refused over it, and a run that finds
// nothing to change once the first is accepted.
func TestScheduledRefreshWritesOneReviewableChange(t *testing.T) {
	ctx := t.Context()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "base.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ids := 0
	var idMu sync.Mutex
	newID := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		ids++
		return fmt.Sprintf("id-%03d", ids)
	}
	llm := &scriptedLLM{content: "Go 1.27 is out"}
	svc := knowledge.NewService(llm, sqlite.NewRepo(db, time.Now, func() knowledge.ID { return knowledge.ID(newID()) }),
		func() knowledge.ID { return knowledge.ID(newID()) }, knowledge.WithAIPolicy(func() bool { return true }))
	sched, err := scheduler.New(sqlite.NewSchedules(db, time.Now), jobs.New(ctx, newID, time.Now), time.Now, newID,
		scheduler.Options{})
	require.NoError(t, err)
	sched.Start(ctx)
	module := smartcron.New(sched.For(smartcron.Name), svc, svc)

	topic, err := svc.CreateTopic(ctx, "Go releases", knowledge.ModeOutline)
	require.NoError(t, err)
	_, err = module.Save(ctx, topic, "refresh the release list", "0 9 * * *", true)
	require.NoError(t, err)

	runAndWait := func() smartcron.View {
		t.Helper()
		require.NoError(t, module.RunNow(ctx, topic))
		var view smartcron.View
		for range 200 {
			view, err = module.Get(ctx, topic)
			require.NoError(t, err)
			if !view.Running && !view.LastRun.IsZero() {
				return view
			}
			time.Sleep(10 * time.Millisecond)
		}
		require.FailNow(t, "the run did not finish", "%+v", view)
		return view
	}
	settled := func(attempt int) smartcron.View {
		t.Helper()
		view := runAndWait()
		for range 100 {
			if llm.count() >= attempt || view.Outcome == string(host.OutcomeSkipped) {
				return view
			}
			time.Sleep(10 * time.Millisecond)
			view, _ = module.Get(ctx, topic)
		}
		return view
	}

	first := settled(1)
	require.Equal(t, string(host.OutcomeOK), first.Outcome, "first run: %+v", first)
	pending, err := svc.Unreviewed(ctx)
	require.NoError(t, err)
	require.Len(t, pending.Blocks, 1, "want the one scheduled change")
	assert.Equal(t, "Go 1.27 is out", pending.Blocks[0].Head.Content)
	history, _ := svc.BlockHistory(ctx, topic)
	assert.Equal(t, smartcron.Source, history[0].Source)
	assert.Equal(t, knowledge.AuthorAI, history[0].Author)

	second := settled(1)
	assert.Equal(t, string(host.OutcomeSkipped), second.Outcome, "second run must skip over the pending change")
	assert.Equal(t, "1 unreviewed changes in scope", second.Detail)
	assert.Equal(t, 1, llm.count(), "the refused run must not spend a call")

	require.NoError(t, svc.AcceptGroup(ctx, pending.Blocks[0].GroupID))
	third := settled(2)
	assert.Equal(t, string(host.OutcomeNoChange), third.Outcome, "the model answered with what is there")
	history, _ = svc.BlockHistory(ctx, topic)
	assert.Len(t, history, 2, "the no-change run wrote nothing")
}
