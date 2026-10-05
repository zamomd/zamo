// Package jobs tracks the long-running operations of one process so they can
// be listed and cancelled: by the user, by whoever started them, and by the
// process shutting down.
package jobs

import (
	"context"
	"sync"
	"time"
)

// Kind classifies a job for cancellation and display.
type Kind string

// The kinds of jobs.
const (
	// KindAgent is an agent run started on the user's behalf.
	KindAgent Kind = "agent"
	// KindSchedule is a scheduled run, started by nobody in particular.
	KindSchedule Kind = "schedule"
)

// Job is the observable description of a running job.
type Job struct {
	ID      string
	Kind    Kind
	Started time.Time
}

type running struct {
	Job
	cancel context.CancelFunc
}

// Registry holds the jobs of one process. Every job's context ends with the
// root context, so shutting the process down cancels all of them.
type Registry struct {
	root  context.Context
	newID func() string
	now   func() time.Time

	mu   sync.Mutex
	jobs map[string]*running
}

// New returns a registry whose jobs live under root.
func New(root context.Context, newID func() string, now func() time.Time) *Registry {
	return &Registry{root: root, newID: newID, now: now, jobs: make(map[string]*running)}
}

// Start registers a job of kind and returns the context to run it under. The
// context ends when parent ends, when the root context ends, when the job is
// cancelled through the registry, or when done is called. done must be called
// once the work has finished; it removes the job.
func (r *Registry) Start(parent context.Context, kind Kind) (ctx context.Context, done func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(r.root, cancel)
	j := &running{ID: r.newID(), Kind: kind, Started: r.now(), cancel: cancel}

	r.mu.Lock()
	r.jobs[j.ID] = j
	r.mu.Unlock()

	return ctx, func() {
		stop()
		cancel()
		r.mu.Lock()
		delete(r.jobs, j.ID)
		r.mu.Unlock()
	}
}

// Cancel cancels the job with the given ID and reports whether it was running.
// The job stays listed until its owner calls done.
func (r *Registry) Cancel(id string) bool {
	r.mu.Lock()
	j, ok := r.jobs[id]
	r.mu.Unlock()
	if ok {
		j.cancel()
	}
	return ok
}

// CancelKind cancels every running job of kind and returns how many it found.
func (r *Registry) CancelKind(kind Kind) int {
	r.mu.Lock()
	var found []*running
	for _, j := range r.jobs {
		if j.Kind == kind {
			found = append(found, j)
		}
	}
	r.mu.Unlock()
	for _, j := range found {
		j.cancel()
	}
	return len(found)
}

// Running lists the jobs that have started and not yet called done, oldest
// first.
func (r *Registry) Running() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, j.Job)
	}
	sortByStart(out)
	return out
}

func sortByStart(jobs []Job) {
	for i := 1; i < len(jobs); i++ {
		for k := i; k > 0 && jobs[k].Started.Before(jobs[k-1].Started); k-- {
			jobs[k], jobs[k-1] = jobs[k-1], jobs[k]
		}
	}
}
