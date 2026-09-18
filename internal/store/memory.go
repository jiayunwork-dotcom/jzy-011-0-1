package store

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Memory is a thread-safe in-memory Store. It is the default backend and keeps
// the service fully runnable without external dependencies; records survive
// until process exit.
type Memory struct {
	mu  sync.RWMutex
	rec []Record
	seq atomic.Int64
}

// NewMemory constructs an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{}
}

// Save appends a record under the write lock. The append + ID assignment are a
// single critical section so concurrent callers never observe reordered or
// shared records.
func (m *Memory) Save(_ context.Context, r Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = genID(m.seq.Add(1))
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	m.rec = append(m.rec, r)
	return r, nil
}

func (m *Memory) Get(_ context.Context, id string) (Record, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.rec {
		if m.rec[i].ID == id {
			return m.rec[i], true, nil
		}
	}
	return Record{}, false, nil
}

// Query scans under RLock and returns newest-first matches.
func (m *Memory) Query(_ context.Context, f Filter) ([]Record, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []Record
	for i := range m.rec {
		r := m.rec[i]
		if !matches(r, f) {
			continue
		}
		matched = append(matched, r)
	}
	// reverse -> newest first
	for l, rr := 0, len(matched)-1; l < rr; l, rr = l+1, rr-1 {
		matched[l], matched[rr] = matched[rr], matched[l]
	}
	total := len(matched)

	off := f.Offset
	if off < 0 {
		off = 0
	}
	if off > len(matched) {
		return []Record{}, total, nil
	}
	matched = matched[off:]
	if f.Limit > 0 && len(matched) > f.Limit {
		matched = matched[:f.Limit]
	}
	return matched, total, nil
}

func (m *Memory) Ping(_ context.Context) error { return nil }
func (m *Memory) Close() error                 { return nil }

func matches(r Record, f Filter) bool {
	if f.Status != "" && r.Status != f.Status {
		return false
	}
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.Source != "" && r.Source != f.Source {
		return false
	}
	if f.Direction != "" && r.Direction != f.Direction {
		return false
	}
	if f.BatchID != "" && r.BatchID != f.BatchID {
		return false
	}
	if (f.MinRadius != 0 || f.MaxRadius != 0) && r.Radius == nil {
		return false
	}
	if f.MinRadius != 0 && *r.Radius < f.MinRadius {
		return false
	}
	if f.MaxRadius != 0 && *r.Radius > f.MaxRadius {
		return false
	}
	if !f.Since.IsZero() && r.CreatedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && r.CreatedAt.After(f.Until) {
		return false
	}
	return true
}

// Len exposes the record count (used by tests).
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rec)
}
