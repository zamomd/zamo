package scheduler

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"
)

// memStore is Store in memory, with the same claim rules as the SQLite one.
type memStore struct {
	mu   sync.Mutex
	recs map[string]*Record // by module + "\x00" + key
}

func newMemStore() *memStore { return &memStore{recs: make(map[string]*Record)} }

func memKey(module, key string) string { return module + "\x00" + key }

func (m *memStore) byID(id string) *Record {
	for _, r := range m.recs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *memStore) Put(_ context.Context, rec Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.recs[memKey(rec.Module, rec.Key)]; ok {
		old.Spec, old.Enabled, old.Payload, old.NextRun = rec.Spec, rec.Enabled, rec.Payload, rec.NextRun
		return *old, nil
	}
	m.recs[memKey(rec.Module, rec.Key)] = &rec
	return rec, nil
}

func (m *memStore) Get(_ context.Context, module, key string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.recs[memKey(module, key)]; ok {
		return *r, nil
	}
	return Record{}, ErrNotFound
}

func (m *memStore) Delete(_ context.Context, module, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, memKey(module, key))
	return nil
}

func (m *memStore) List(_ context.Context, module string) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.recs {
		if module == "" || r.Module == module {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (m *memStore) Due(_ context.Context, now time.Time, lease time.Duration) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.recs {
		free := r.ClaimedAt.IsZero() || !r.ClaimedAt.After(now.Add(-lease))
		if r.Enabled && !r.NextRun.IsZero() && !r.NextRun.After(now) && free {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(a, b Record) int {
		return cmp.Or(a.NextRun.Compare(b.NextRun), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func (m *memStore) Claim(_ context.Context, id, instance string, now time.Time, lease time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byID(id)
	if r == nil || (!r.ClaimedAt.IsZero() && r.ClaimedAt.After(now.Add(-lease))) {
		return false, nil
	}
	r.ClaimedAt, r.ClaimedBy = now, instance
	return true, nil
}

func (m *memStore) Finish(_ context.Context, id, instance string, done Completion) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byID(id)
	if r == nil || r.ClaimedBy != instance {
		return false, nil
	}
	r.LastRun, r.Outcome, r.Detail = done.At, done.Outcome, done.Detail
	r.NextRun = time.Time{}
	if r.Enabled {
		r.NextRun = done.NextRun
	}
	r.ClaimedAt, r.ClaimedBy = time.Time{}, ""
	return true, nil
}

func (m *memStore) Release(_ context.Context, id, instance string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.byID(id); r != nil && r.ClaimedBy == instance {
		r.ClaimedAt, r.ClaimedBy = time.Time{}, ""
	}
	return nil
}

func (m *memStore) Reschedule(_ context.Context, id string, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.byID(id); r != nil && r.Enabled {
		r.NextRun = next
	}
	return nil
}
