package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPostgresBackend runs only when ACKERMANN_TEST_DSN points at a reachable
// PostgreSQL database (e.g. the docker-compose db service). It exercises the
// same behavioral surface as the memory backend, ensuring the SQL layer
// persists and filters records correctly.
//
//	DSN=postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable \
//	  go test ./internal/store -run TestPostgresBackend
func TestPostgresBackend(t *testing.T) {
	dsn := os.Getenv("ACKERMANN_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACKERMANN_TEST_DSN to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pg, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pg.Close()

	if err := pg.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Baseline count so the test is repeatable against the same database.
	_, before, err := pg.Query(ctx, Filter{})
	if err != nil {
		t.Fatalf("baseline query: %v", err)
	}

	R1, R2 := 5.5, 9.0
	rec1, err := pg.Save(ctx, Record{
		BatchID: "", Index: -1, Kind: KindSingle, Source: "calculate",
		Status: StatusOK, Direction: "left", Radius: &R1,
		Request: []byte(`{"wheelbase_m":2.7}`), Result: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	batchID := NewID("bat")
	if _, err := pg.Save(ctx, Record{
		ID: batchID, BatchID: "", Index: -1, Kind: KindBatch, Source: "batch",
		Status: StatusOK, Direction: "batch",
		Request: []byte(`{"items":[]}`), Result: []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	if _, err := pg.Save(ctx, Record{
		BatchID: batchID, Index: 0, Kind: KindBatch, Source: "batch",
		Status: StatusOK, Direction: "right", Radius: &R2,
		Request: []byte(`{"inner_angle":-30}`), Result: []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	got, ok, err := pg.Get(ctx, rec1.ID)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Radius == nil || *got.Radius != R1 || got.Direction != "left" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if string(got.Request) != `{"wheelbase_m":2.7}` {
		t.Fatalf("request JSON not preserved: %s", got.Request)
	}

	// Filtering and batch linkage.
	recs, total, err := pg.Query(ctx, Filter{BatchID: batchID})
	if err != nil {
		t.Fatalf("batch query: %v", err)
	}
	if total != 1 || len(recs) != 1 || recs[0].Index != 0 {
		t.Fatalf("batch filter wrong: total=%d len=%d", total, len(recs))
	}

	_, totalOK, err := pg.Query(ctx, Filter{Status: StatusOK})
	if err != nil {
		t.Fatalf("status query: %v", err)
	}
	if totalOK < before+3 {
		t.Fatalf("expected at least %d ok rows, got %d", before+3, totalOK)
	}

	_, totalBig, err := pg.Query(ctx, Filter{MinRadius: 8})
	if err != nil {
		t.Fatalf("radius query: %v", err)
	}
	if totalBig < 1 {
		t.Fatalf("min radius filter returned %d", totalBig)
	}

	// Limit/offset sanity without caring about unrelated rows.
	page1, _, err := pg.Query(ctx, Filter{BatchID: batchID, Limit: 1})
	if err != nil || len(page1) != 1 {
		t.Fatalf("limit query: %v len=%d", err, len(page1))
	}
}
