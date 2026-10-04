// Package scheduler runs the timed work of feature modules. It owns the
// schedule records: a module registers a handler and describes what to run
// through an opaque payload; the scheduler decides when, claims the run in
// the database so two processes on one base never run the same record, and
// records how it went. It knows nothing about blocks or AI.
package scheduler

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/zamomd/zamo/internal/jobs"
)

// Outcome is how a run ended.
type Outcome string

// The outcomes a handler can report.
const (
	OutcomeOK       Outcome = "ok"
	OutcomeNoChange Outcome = "no_change"
	OutcomeSkipped  Outcome = "skipped"
	OutcomeFailed   Outcome = "failed"
)

// RunResult is a handler's report: the outcome and, for anything but ok, why.
type RunResult struct {
	Outcome Outcome
	Detail  string
}

// Handler runs one record. It is called with the record's key and payload and
// must return once ctx ends; what it did before that is its own business, so
// a cancelled run records no outcome.
type Handler func(ctx context.Context, key string, payload jsontext.Value) RunResult

// HandlerOptions is the policy a module registers its handler with.
type HandlerOptions struct {
	// MaxConcurrent caps the module's runs in flight; zero means one.
	MaxConcurrent int
}

// Schedule is a module's view of one of its records.
type Schedule struct {
	Key     string
	Spec    string
	Enabled bool
	Running bool
	Payload jsontext.Value
	NextRun time.Time
	LastRun time.Time
	Outcome Outcome
	Detail  string
}

// Entry is one record of any module, for the application's list of all
// scheduled work. Available is false when no module claims the record: it
// then never runs and is shown as such.
type Entry struct {
	Module    string
	Available bool
	Schedule
}

// Event announces a run starting (Running true) or finishing.
type Event struct {
	Module  string
	Key     string
	Running bool
	Outcome Outcome
	Detail  string
}

// Options tune one scheduler. Zero values take the defaults below.
type Options struct {
	// Lease is how long a claim stays valid. A process that dies mid-run
	// frees its record after this. Must exceed RunTimeout.
	Lease time.Duration
	// RunTimeout bounds one run.
	RunTimeout time.Duration
	// Heartbeat is how often the loop compares the wall clock with the
	// records, whatever its timers say: a laptop asleep stops the monotonic
	// clock timers run on, and the first beat after waking finds what is due.
	Heartbeat time.Duration
	// OnRun, when set, is told about every run starting and finishing.
	OnRun func(Event)
}

const (
	defaultLease      = 15 * time.Minute
	defaultRunTimeout = 10 * time.Minute
	defaultHeartbeat  = 45 * time.Second
)

// Errors a module can act on.
var (
	ErrInvalidSpec = errors.New("invalid cron expression")
	ErrBusy        = errors.New("a run is already in progress")
	ErrNoHandler   = errors.New("no handler registered for the module")
)

type registration struct {
	handler Handler
	slots   chan struct{}
}

// Scheduler is the scheduler of one process.
type Scheduler struct {
	store    Store
	jobs     *jobs.Registry
	now      func() time.Time
	newID    func() string
	instance string
	opts     Options

	mu       sync.Mutex
	handlers map[string]*registration
	running  map[string]context.CancelCauseFunc // by record id
	root     context.Context
	wake     chan struct{}
	wg       sync.WaitGroup
}

// The causes a run's context ends with.
var (
	errStopped = errors.New("run cancelled")
	errTimeout = errors.New("run timed out")
)

// New returns a scheduler over store. Runs are started under reg so they end
// with the process. now and newID are injected for tests.
func New(store Store, reg *jobs.Registry, now func() time.Time, newID func() string, opts Options) (*Scheduler, error) {
	if opts.Lease == 0 {
		opts.Lease = defaultLease
	}
	if opts.RunTimeout == 0 {
		opts.RunTimeout = defaultRunTimeout
	}
	if opts.Heartbeat == 0 {
		opts.Heartbeat = defaultHeartbeat
	}
	if opts.Lease <= opts.RunTimeout {
		return nil, fmt.Errorf("scheduler: lease %v must exceed the run timeout %v", opts.Lease, opts.RunTimeout)
	}
	return &Scheduler{
		store: store, jobs: reg, now: now, newID: newID, instance: newID(), opts: opts,
		handlers: make(map[string]*registration),
		running:  make(map[string]context.CancelCauseFunc),
		wake:     make(chan struct{}, 1),
	}, nil
}

// Start runs the loop until ctx ends. Runs in flight are cancelled with it.
// Slots that passed while no process was running are dropped first: a closed
// app runs nothing late.
func (s *Scheduler) Start(ctx context.Context) {
	s.begin(ctx)
	s.skipMissed(ctx)
	go s.loop(ctx)
}

func (s *Scheduler) begin(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root = ctx
}

// skipMissed moves every overdue record on to its next slot without running it.
func (s *Scheduler) skipMissed(ctx context.Context) {
	all, err := s.store.List(ctx, "")
	if err != nil {
		return
	}
	now := s.now()
	for _, rec := range all {
		s.skipSlot(ctx, rec, now)
	}
}

// skipSlot gives up the record's slot if it has come, counting the next one
// from now.
func (s *Scheduler) skipSlot(ctx context.Context, rec Record, now time.Time) {
	if !rec.Enabled || rec.NextRun.IsZero() || rec.NextRun.After(now) {
		return
	}
	if sched, err := parse(rec.Spec); err == nil {
		_ = s.store.Reschedule(ctx, rec.ID, sched.next(now))
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	beat := time.NewTicker(s.opts.Heartbeat)
	defer beat.Stop()
	for {
		s.tick(ctx)
		timer := time.NewTimer(s.untilNext(ctx))
		select {
		case <-ctx.Done():
			timer.Stop()
			s.wg.Wait()
			return
		case <-timer.C:
		case <-beat.C:
			timer.Stop()
		case <-s.wake:
			timer.Stop()
		}
	}
}

// untilNext is how long the loop may sleep: until the earliest run still
// ahead, bounded by the heartbeat so a slept-through timer is noticed. A
// record already due is not waited for: the tick before this either started
// it or left it to the wake of the run it queues behind.
func (s *Scheduler) untilNext(ctx context.Context) time.Duration {
	wait := s.opts.Heartbeat
	all, err := s.store.List(ctx, "")
	if err != nil {
		return wait
	}
	now := s.now()
	for _, r := range all {
		if r.Enabled && r.NextRun.After(now) {
			wait = min(wait, r.NextRun.Sub(now))
		}
	}
	return wait
}

func (s *Scheduler) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// tick starts every due record that has a handler and a free slot. Waiting
// for a slot happens here, unclaimed, so a queued record does not spend its
// lease; the next tick tries again.
func (s *Scheduler) tick(ctx context.Context) {
	now := s.now()
	due, err := s.store.Due(ctx, now, s.opts.Lease)
	if err != nil {
		return
	}
	for _, rec := range due {
		s.mu.Lock()
		reg := s.handlers[rec.Module]
		_, busy := s.running[rec.ID]
		s.mu.Unlock()
		if reg == nil || busy || !reg.acquire() {
			continue
		}
		ok, err := s.store.Claim(ctx, rec.ID, s.instance, now, s.opts.Lease)
		if err != nil || !ok {
			reg.release()
			continue
		}
		s.launch(rec, reg)
	}
}

func (r *registration) acquire() bool {
	select {
	case r.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *registration) release() { <-r.slots }

// launch runs a claimed record in its own goroutine and records the result.
func (s *Scheduler) launch(rec Record, reg *registration) {
	s.mu.Lock()
	root := s.root
	s.mu.Unlock()
	jobCtx, done := s.jobs.Start(root, jobs.KindSchedule)
	ctx, cancel := context.WithCancelCause(jobCtx)
	ctx, stopTimeout := context.WithTimeoutCause(ctx, s.opts.RunTimeout, errTimeout)
	s.mu.Lock()
	s.running[rec.ID] = cancel
	s.mu.Unlock()

	s.wg.Go(func() {
		defer func() {
			stopTimeout()
			cancel(nil)
			done()
			reg.release()
			s.mu.Lock()
			delete(s.running, rec.ID)
			s.mu.Unlock()
			s.nudge()
		}()
		s.emit(Event{Module: rec.Module, Key: rec.Key, Running: true})
		res := call(ctx, reg.handler, rec)
		s.settle(ctx, rec, res)
	})
}

// call runs the handler, turning a panic into a failed result rather than a
// dead scheduler.
func call(ctx context.Context, h Handler, rec Record) (res RunResult) {
	defer func() {
		if p := recover(); p != nil {
			res = RunResult{Outcome: OutcomeFailed, Detail: fmt.Sprintf("panic: %v\n%s", p, debug.Stack())}
		}
	}()
	return h(ctx, rec.Key, rec.Payload)
}

// settle records the run unless it was cancelled. A cancelled run releases
// its claim, records no outcome and gives up the slot it ran for: left due,
// the record would start again on the next tick.
func (s *Scheduler) settle(ctx context.Context, rec Record, res RunResult) {
	bg := context.WithoutCancel(ctx)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, errTimeout) {
		_ = s.store.Release(bg, rec.ID, s.instance)
		if cur, err := s.store.Get(bg, rec.Module, rec.Key); err == nil {
			s.skipSlot(bg, cur, s.now())
		}
		s.emit(Event{Module: rec.Module, Key: rec.Key})
		return
	}
	if errors.Is(context.Cause(ctx), errTimeout) {
		res = RunResult{Outcome: OutcomeFailed, Detail: "run timed out"}
	}
	if res.Outcome == "" {
		res.Outcome = OutcomeFailed
		res.Detail = "handler reported no outcome"
	}
	now := s.now()
	done := Completion{At: now, Outcome: string(res.Outcome), Detail: res.Detail}
	// The spec may have been edited while the run was going: the next run
	// follows the record as it is now, not as it was claimed.
	if cur, err := s.store.Get(bg, rec.Module, rec.Key); err == nil && cur.Enabled {
		if sched, err := parse(cur.Spec); err == nil {
			done.NextRun = sched.next(now)
		}
	}
	_, _ = s.store.Finish(bg, rec.ID, s.instance, done)
	s.emit(Event{Module: rec.Module, Key: rec.Key, Outcome: res.Outcome, Detail: res.Detail})
}

func (s *Scheduler) emit(e Event) {
	if s.opts.OnRun != nil {
		s.opts.OnRun(e)
	}
}

// ListAll returns every module's records, for the application's overview.
func (s *Scheduler) ListAll(ctx context.Context) ([]Entry, error) {
	recs, err := s.store.List(ctx, "")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(recs))
	for _, r := range recs {
		_, available := s.handlers[r.Module]
		out = append(out, Entry{Module: r.Module, Available: available, Schedule: s.view(r)})
	}
	return out, nil
}

func (s *Scheduler) view(r Record) Schedule {
	live := !r.ClaimedAt.IsZero() && r.ClaimedAt.After(s.now().Add(-s.opts.Lease))
	return Schedule{
		Key: r.Key, Spec: r.Spec, Enabled: r.Enabled, Running: live, Payload: r.Payload,
		NextRun: r.NextRun, LastRun: r.LastRun, Outcome: Outcome(r.Outcome), Detail: r.Detail,
	}
}

// For returns the scheduler as one module sees it: its own records only.
func (s *Scheduler) For(module string) *Module {
	return &Module{s: s, name: module}
}

// Module is a scheduler bound to one module's name.
type Module struct {
	s    *Scheduler
	name string
}

// Handle registers the module's handler. Records without one never run.
func (m *Module) Handle(h Handler, opts HandlerOptions) {
	n := max(opts.MaxConcurrent, 1)
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	m.s.handlers[m.name] = &registration{handler: h, slots: make(chan struct{}, n)}
}

// Preview validates spec and returns its next n firing times from now.
func (m *Module) Preview(spec string, n int) ([]time.Time, error) {
	sched, err := parse(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	out := make([]time.Time, 0, n)
	at := m.s.now()
	for range n {
		at = sched.next(at)
		out = append(out, at)
	}
	return out, nil
}

// Put creates or replaces the module's record for key. The next run counts
// from now, so saving never triggers a run and a pause is never caught up.
// Disabling a record cancels its run in flight.
func (m *Module) Put(ctx context.Context, key, spec string, enabled bool, payload jsontext.Value) (Schedule, error) {
	sched, err := parse(spec)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	if payload == nil {
		payload = jsontext.Value(`{}`)
	}
	rec := Record{ID: m.s.newID(), Module: m.name, Key: key, Spec: spec, Enabled: enabled, Payload: payload}
	if enabled {
		rec.NextRun = sched.next(m.s.now())
	}
	stored, err := m.s.store.Put(ctx, rec)
	if err != nil {
		return Schedule{}, err
	}
	if !enabled {
		m.s.stop(stored.ID)
	}
	m.s.nudge()
	return m.s.view(stored), nil
}

// Get returns the module's record for key.
func (m *Module) Get(ctx context.Context, key string) (Schedule, error) {
	rec, err := m.s.store.Get(ctx, m.name, key)
	if err != nil {
		return Schedule{}, err
	}
	return m.s.view(rec), nil
}

// Delete removes the record and cancels its run in flight.
func (m *Module) Delete(ctx context.Context, key string) error {
	rec, err := m.s.store.Get(ctx, m.name, key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	m.s.stop(rec.ID)
	return m.s.store.Delete(ctx, m.name, key)
}

// List returns the module's records.
func (m *Module) List(ctx context.Context) ([]Schedule, error) {
	recs, err := m.s.store.List(ctx, m.name)
	if err != nil {
		return nil, err
	}
	out := make([]Schedule, 0, len(recs))
	for _, r := range recs {
		out = append(out, m.s.view(r))
	}
	return out, nil
}

// RunNow runs the record at once, through the same claim as a timed run. It
// refuses with ErrBusy while the record runs or the module has no free slot.
func (m *Module) RunNow(ctx context.Context, key string) error {
	rec, err := m.s.store.Get(ctx, m.name, key)
	if err != nil {
		return err
	}
	m.s.mu.Lock()
	reg := m.s.handlers[m.name]
	_, busy := m.s.running[rec.ID]
	m.s.mu.Unlock()
	if reg == nil {
		return ErrNoHandler
	}
	if busy || !reg.acquire() {
		return ErrBusy
	}
	ok, err := m.s.store.Claim(ctx, rec.ID, m.s.instance, m.s.now(), m.s.opts.Lease)
	if err != nil || !ok {
		reg.release()
		if err != nil {
			return err
		}
		return ErrBusy
	}
	m.s.launch(rec, reg)
	return nil
}

// Cancel stops the record's run in flight and reports whether there was one.
func (m *Module) Cancel(ctx context.Context, key string) bool {
	rec, err := m.s.store.Get(ctx, m.name, key)
	if err != nil {
		return false
	}
	return m.s.stop(rec.ID)
}

func (s *Scheduler) stop(id string) bool {
	s.mu.Lock()
	cancel, ok := s.running[id]
	s.mu.Unlock()
	if ok {
		cancel(errStopped)
	}
	return ok
}
