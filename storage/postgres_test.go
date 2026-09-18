package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPostgresRoundTrip runs only when ACKERMANN_TEST_DSN is set, e.g.
//
//	ACKERMANN_TEST_DSN=postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable
//
// docker compose up -d db provides exactly that database.
func TestPostgresRoundTrip(t *testing.T) {
	dsn := os.Getenv("ACKERMANN_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACKERMANN_TEST_DSN to run the Postgres integration test")
	}
	ctx := context.Background()
	st, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer st.Close(ctx)

	wb, tr, ia := 2.7, 1.55, 30.0
	rec, err := st.Insert(ctx, &Record{
		Kind: "single", AngleUnit: "deg",
		Wheelbase: &wb, Track: &tr, InsideAngle: &ia,
		Success: true,
		Result: map[string]interface{}{
			"direction": "left", "outside_angle_deg": 23.44,
		},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if rec.ID == 0 {
		t.Fatal("id not assigned")
	}

	// Failure record with error metadata.
	batchID := "it-batch"
	idx := 0
	if _, err := st.Insert(ctx, &Record{
		Kind: "batch_item", BatchID: &batchID, BatchIndex: &idx,
		Success: false, ErrorCode: "angle_out_of_range",
		ErrorMessage: "item 1: out of range",
	}); err != nil {
		t.Fatalf("insert failure: %v", err)
	}

	ok := true
	rows, err := st.Query(ctx, Filter{Success: &ok})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no success rows returned")
	}
	var found bool
	for _, r := range rows {
		if r.ID == rec.ID {
			found = true
			if r.Wheelbase == nil || *r.Wheelbase != 2.7 {
				t.Errorf("wheelbase not persisted: %+v", r.Wheelbase)
			}
			m := r.Result.(map[string]interface{})
			if m["direction"] != "left" {
				t.Errorf("result json not persisted: %v", m)
			}
		}
	}
	if !found {
		t.Fatal("inserted record not found")
	}

	// Generated direction column filter.
	left, err := st.Query(ctx, Filter{Direction: "left"})
	if err != nil {
		t.Fatalf("direction filter: %v", err)
	}
	if len(left) == 0 {
		t.Fatal("direction=left returned no rows")
	}

	// Batch id filter + time range.
	since := time.Now().Add(-time.Minute)
	batch, err := st.Query(ctx, Filter{BatchID: batchID, Since: &since})
	if err != nil {
		t.Fatalf("batch filter: %v", err)
	}
	if len(batch) != 1 || batch[0].ErrorMessage == "" {
		t.Fatalf("batch record wrong: %+v", batch)
	}

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
