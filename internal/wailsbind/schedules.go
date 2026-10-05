package wailsbind

import (
	"context"
	"time"

	"github.com/zamomd/zamo/internal/scheduler"
)

// ScheduleRunEvent is the frontend event announcing a scheduled run starting
// or finishing. Its payload is a ScheduleRun.
const ScheduleRunEvent = "schedule:run"

// ScheduleRun is the JSON shape of a ScheduleRunEvent.
type ScheduleRun struct {
	Module  string `json:"module"`
	Key     string `json:"key"`
	Running bool   `json:"running"`
	Outcome string `json:"outcome,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// ScheduleEntry is one row of the application's list of scheduled work.
type ScheduleEntry struct {
	Module    string    `json:"module"`
	Available bool      `json:"available"`
	Key       string    `json:"key"`
	Spec      string    `json:"spec"`
	Enabled   bool      `json:"enabled"`
	Running   bool      `json:"running"`
	NextRun   time.Time `json:"nextRun,omitzero"`
	LastRun   time.Time `json:"lastRun,omitzero"`
	Outcome   string    `json:"outcome,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// ScheduleLister is the scheduler as the overview needs it.
type ScheduleLister interface {
	ListAll(ctx context.Context) ([]scheduler.Entry, error)
}

// SetScheduler wires the scheduler the overview lists. A package function for
// the reason SetEmitter is one.
func SetScheduler(a *App, s ScheduleLister) { a.schedules = s }

// ListSchedules returns every module's scheduled work, for the Changes
// screen's scheduled tab. Without a scheduler (headless) the list is empty.
func (a *App) ListSchedules(ctx context.Context) ([]ScheduleEntry, error) {
	if a.schedules == nil {
		return []ScheduleEntry{}, nil
	}
	entries, err := a.schedules.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, ScheduleEntry{
			Module: e.Module, Available: e.Available, Key: e.Key, Spec: e.Spec,
			Enabled: e.Enabled, Running: e.Running, NextRun: e.NextRun, LastRun: e.LastRun,
			Outcome: string(e.Outcome), Detail: e.Detail,
		})
	}
	return out, nil
}

// EmitScheduleRuns returns the scheduler's OnRun callback that forwards each
// run event to the frontend through emit.
func EmitScheduleRuns(emit func() Emitter) func(scheduler.Event) {
	return func(e scheduler.Event) {
		if em := emit(); em != nil {
			em.Emit(ScheduleRunEvent, ScheduleRun{
				Module: e.Module, Key: e.Key, Running: e.Running, Outcome: string(e.Outcome), Detail: e.Detail,
			})
		}
	}
}
