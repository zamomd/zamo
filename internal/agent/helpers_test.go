package agent_test

import (
	"context"
	"encoding/json/jsontext"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
	"github.com/zamomd/zamo/internal/sqlite"
	"github.com/zamomd/zamo/internal/ulid"
)

func openService(t *testing.T) *knowledge.Service {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "zamo.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	return knowledge.NewService(sqlite.NewRepo(db, time.Now, newID), newID)
}

// seedTopic writes an owner-authored topic with the given children and
// returns the topic id followed by the children's ids.
func seedTopic(t *testing.T, svc *knowledge.Service, name string, children ...string) []knowledge.ID {
	t.Helper()
	ops := []knowledge.Op{{Kind: knowledge.OpAdd, TempID: "root", Content: name}}
	for i, c := range children {
		ops = append(ops, knowledge.Op{
			Kind: knowledge.OpAdd, TempID: "c" + string(rune('0'+i)), Parent: "root", Content: c,
		})
	}
	meta := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "seed"}
	applied, err := svc.ApplyOps(t.Context(), meta, ops)
	require.NoError(t, err)
	ids := []knowledge.ID{applied.IDs["root"]}
	for i := range children {
		ids = append(ids, applied.IDs["c"+string(rune('0'+i))])
	}
	return ids
}

// script is a model that plays fixed replies in order and records every
// request. Once the replies run out it repeats the last one.
type script struct {
	mu       sync.Mutex
	replies  []llm.Reply
	requests []llm.Request
	// before runs at the start of the n-th Chat (from 0), outside the lock.
	before func(ctx context.Context, n int) error
}

func (s *script) Chat(ctx context.Context, r llm.Request) (llm.Reply, error) {
	s.mu.Lock()
	n := len(s.requests)
	s.requests = append(s.requests, r)
	reply := s.replies[min(n, len(s.replies)-1)]
	before := s.before
	s.mu.Unlock()
	if before != nil {
		if err := before(ctx, n); err != nil {
			return llm.Reply{}, err
		}
	}
	return reply, nil
}

func (s *script) Ping(context.Context) error { return nil }

func (s *script) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *script) request(n int) llm.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[n]
}

func text(t string) llm.Reply { return llm.Reply{Text: t} }

func toolCall(id, name, args string) llm.Reply {
	return llm.Reply{Calls: []llm.ToolCall{{ID: id, Name: name, Args: jsontext.Value(args)}}}
}

func newRegistry(t *testing.T) *jobs.Registry {
	t.Helper()
	return jobs.New(t.Context(), ulid.New, time.Now)
}

func newRunner(t *testing.T, model llm.Client, svc *knowledge.Service) *agent.Runner {
	t.Helper()
	return agent.New(model, svc, func() bool { return true }, newRegistry(t))
}

func toolNames(r llm.Request) []string {
	names := make([]string, len(r.Tools))
	for i, tool := range r.Tools {
		names[i] = tool.Name
	}
	return names
}
