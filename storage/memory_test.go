package storage

import (
	"context"
	"testing"
	"time"
)

func TestMemoryInsertQueryAndCount(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	mk := func(ok bool, kind string) *Record {
		return &Record{Kind: kind, Success: ok, AngleUnit: "deg"}
	}
	r1, err := s.Insert(ctx, mk(true, "single"))
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := s.Insert(ctx, mk(false, "batch_item"))
	r3, _ := s.Insert(ctx, mk(true, "batch_item"))
	if r1.ID == 0 || r2.ID == r1.ID || r3.ID == r2.ID {
		t.Fatalf("ids must be unique sequential: %d %d %d", r1.ID, r2.ID, r3.ID)
	}

	n, _ := s.Count(ctx)
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}

	ok := true
	got, err := s.Query(ctx, Filter{Success: &ok})
	if err != nil || len(got) != 2 {
		t.Fatalf("success filter: %d err=%v", len(got), err)
	}
	got, _ = s.Query(ctx, Filter{Kind: "batch_item"})
	if len(got) != 2 {
		t.Fatalf("kind filter: %d", len(got))
	}

	// Limit/offset.
	page, _ := s.Query(ctx, Filter{Limit: 2, Offset: 1})
	if len(page) != 2 {
		t.Fatalf("paging got %d", len(page))
	}
	if page[0].ID != r2.ID {
		t.Errorf("offset should skip the first record")
	}
}

func TestMemoryTimestampAndBatchFilter(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	bid := "batch-abc"
	_, _ = s.Insert(ctx, &Record{Kind: "batch_item", Success: true, BatchID: &bid})
	_, _ = s.Insert(ctx, &Record{Kind: "single", Success: true})

	got, _ := s.Query(ctx, Filter{BatchID: bid})
	if len(got) != 1 || got[0].BatchID == nil || *got[0].BatchID != bid {
		t.Fatalf("batch filter returned %d", len(got))
	}
	if got[0].CreatedAt.IsZero() {
		t.Fatal("created_at must be populated")
	}

	past := time.Now().Add(time.Hour)
	got, _ = s.Query(ctx, Filter{Since: &past})
	if len(got) != 0 {
		t.Fatalf("since filter returned %d", len(got))
	}
}

// TestConcurrentInsertNoCrossTalk fires many goroutines and checks that no ID
// is ever reused and the total count is exact.
func TestConcurrentInsertNoCrossTalk(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	const n = 200
	done := make(chan *Record, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			r, err := s.Insert(ctx, &Record{Kind: "single", Success: i%2 == 0})
			if err != nil {
				t.Errorf("insert: %v", err)
			}
			done <- r
		}()
	}
	ids := map[int64]bool{}
	for i := 0; i < n; i++ {
		r := <-done
		if ids[r.ID] {
			t.Fatalf("duplicate id %d", r.ID)
		}
		ids[r.ID] = true
	}
	if cnt, _ := s.Count(ctx); cnt != n {
		t.Fatalf("count %d want %d", cnt, n)
	}
}
