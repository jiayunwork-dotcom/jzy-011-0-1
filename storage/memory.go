package storage

import (
	"context"
	"sync"
	"time"

	"ackermann-service/ackermann"
)

// MemoryStore is an in-process Store. It is safe for concurrent use and keeps
// records ordered by insertion. It makes the service deployable standalone
// without a database; PostgresStore provides durable persistence.
type MemoryStore struct {
	mu     sync.RWMutex
	nextID int64
	recs   []Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1}
}

func (m *MemoryStore) Insert(_ context.Context, r *Record) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	cp.ID = m.nextID
	m.nextID++
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	m.recs = append(m.recs, cp)
	return recPtr(cp), nil
}

func (m *MemoryStore) Query(_ context.Context, f Filter) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []Record
	// Newest first, matching the PostgreSQL implementation (ORDER BY id DESC).
	for i := len(m.recs) - 1; i >= 0; i-- {
		if match(m.recs[i], f) {
			out = append(out, m.recs[i])
		}
	}

	// Apply offset/limit on the filtered, newest-first slice.
	if f.Offset >= len(out) {
		return []Record{}, nil
	}
	out = out[f.Offset:]
	if f.Limit > 0 && f.Limit < len(out) {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *MemoryStore) Count(_ context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.recs)), nil
}

func (m *MemoryStore) Ping(_ context.Context) error  { return nil }
func (m *MemoryStore) Close(_ context.Context) error { return nil }

func match(r Record, f Filter) bool {
	if f.Success != nil && r.Success != *f.Success {
		return false
	}
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.BatchID != "" && (r.BatchID == nil || *r.BatchID != f.BatchID) {
		return false
	}
	if f.Since != nil && r.CreatedAt.Before(*f.Since) {
		return false
	}
	if f.Until != nil && r.CreatedAt.After(*f.Until) {
		return false
	}
	if f.Direction != "" && recordDirection(r) != f.Direction {
		return false
	}
	return true
}

func recordDirection(r Record) string {
	switch v := r.Result.(type) {
	case *ackermann.Result:
		if v == nil {
			return ""
		}
		return v.Direction
	case map[string]interface{}:
		if s, ok := v["direction"].(string); ok {
			return s
		}
	}
	return ""
}

func recPtr(r Record) *Record { return &r }
