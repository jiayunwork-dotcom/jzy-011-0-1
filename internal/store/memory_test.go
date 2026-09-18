package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestMemorySaveGetQuery(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	R := 5.5
	rec, err := m.Save(ctx, Record{Kind: KindSingle, Source: "calculate", Status: StatusOK, Radius: &R})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID == "" || rec.CreatedAt.IsZero() {
		t.Fatalf("record not populated: %+v", rec)
	}
	got, ok, err := m.Get(ctx, rec.ID)
	if err != nil || !ok || got.ID != rec.ID {
		t.Fatalf("get failed: ok=%v err=%v", ok, err)
	}
	recs, total, err := m.Query(ctx, Filter{Status: StatusOK})
	if err != nil || total != 1 || len(recs) != 1 {
		t.Fatalf("query failed: total=%d len=%d err=%v", total, len(recs), err)
	}
}

// Concurrent saves must not lose records, share ids or leave torn state.
func TestMemoryConcurrentSaves(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	const writers, perWriter = 20, 50
	var wg sync.WaitGroup
	ids := make(chan string, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				R := float64(w*1000 + i)
				rec, err := m.Save(ctx, Record{
					Kind: KindSingle, Source: "calculate", Status: StatusOK,
					Request: []byte(fmt.Sprintf(`{"w":%d,"i":%d}`, w, i)),
					Result:  []byte(`{"ok":true}`),
					Radius:  &R,
				})
				if err != nil {
					t.Errorf("save: %v", err)
					return
				}
				ids <- rec.ID
			}
		}(w)
	}
	// Concurrent readers while writes are in flight.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_, _, _ = m.Query(ctx, Filter{Limit: 10})
			}
		}
	}()
	wg.Wait()
	close(done)
	close(ids)

	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id under concurrency: %s", id)
		}
		seen[id] = true
	}
	if m.Len() != writers*perWriter {
		t.Fatalf("lost records: %d != %d", m.Len(), writers*perWriter)
	}
}

func TestMemoryQueryFiltersAndOrder(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		r := float64(i + 1)
		_, _ = m.Save(ctx, Record{Status: StatusOK, Direction: "left", Radius: &r})
	}
	_, _ = m.Save(ctx, Record{Status: StatusError, Direction: "invalid"})

	recs, total, err := m.Query(ctx, Filter{Status: StatusOK, MinRadius: 3})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 { // radii 3,4,5
		t.Fatalf("min radius filter total = %d", total)
	}
	// newest first
	if recs[0].Radius == nil || *recs[0].Radius != 5 {
		t.Fatalf("ordering wrong: %+v", recs[0])
	}

	recs, total, _ = m.Query(ctx, Filter{Status: StatusOK, Limit: 2, Offset: 2})
	if total != 5 || len(recs) != 2 || *recs[0].Radius != 3 {
		t.Fatalf("limit/offset wrong: total=%d len=%d first=%v", total, len(recs), recs[0].Radius)
	}
}
