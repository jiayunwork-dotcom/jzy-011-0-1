package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"ackermann/internal/geometry"
	"ackermann/internal/store"
	"ackermann/internal/validate"
)

func testService() *Service {
	return New(store.NewMemory(), Config{ToleranceMM: 1e-6})
}

func mustBody(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSingleCalc(t *testing.T) {
	s := testService()
	resp := s.Calc(context.Background(),
		mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30}),
		"calculate", false)
	if !resp.OK {
		t.Fatalf("calc failed: %+v", resp.Error)
	}
	if resp.Result.OuterAngle >= 30 || resp.Result.OuterAngle < 20 {
		t.Fatalf("outer angle %.4f not clearly below 30", resp.Result.OuterAngle)
	}
	if resp.Result.TurnRadius == nil || *resp.Result.TurnRadius <= 0 {
		t.Fatalf("missing finite turn radius: %+v", resp.Result.TurnRadius)
	}
	if resp.Result.ArcRatio == nil || resp.Result.ArcRatio.Rear <= 1 {
		t.Fatalf("bad arc ratio: %+v", resp.Result.ArcRatio)
	}
}

func TestStraightNoFakeRadius(t *testing.T) {
	s := testService()
	resp := s.Calc(context.Background(),
		mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 0}),
		"calculate", false)
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	if !resp.Result.Straight || resp.Result.TurnRadius != nil ||
		resp.Result.InstantCenter != nil || resp.Result.ArcRatio != nil {
		t.Fatalf("straight case fabricated finite geometry: %+v", resp.Result)
	}
}

func TestCalcPersistsRequestAndResult(t *testing.T) {
	s := testService()
	body := mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30})
	resp := s.Calc(context.Background(), body, "calculate", false)
	if !resp.OK || resp.RecordID == "" {
		t.Fatalf("missing record id: %+v", resp.Error)
	}
	rec, ok, err := s.GetRecord(context.Background(), resp.RecordID)
	if err != nil || !ok {
		t.Fatalf("record not persisted: ok=%v err=%v", ok, err)
	}
	if rec.Status != store.StatusOK || rec.Kind != store.KindSingle || rec.Source != "calculate" {
		t.Fatalf("wrong record metadata: %+v", rec)
	}
	if rec.Direction != "left" || rec.Radius == nil {
		t.Fatalf("wrong indexed fields: dir=%s radius=%v", rec.Direction, rec.Radius)
	}
	if !strings.Contains(string(rec.Request), "2.7") || len(rec.Result) == 0 {
		t.Fatalf("request/result bodies not stored: req=%s res=%s", rec.Request, rec.Result)
	}
}

func TestRejectedCalcPersistsError(t *testing.T) {
	s := testService()
	body := mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 90})
	resp := s.Calc(context.Background(), body, "calculate", false)
	if resp.OK {
		t.Fatal("expected rejection")
	}
	out, _ := s.History(context.Background(), HistoryQuery{Status: store.StatusError, Limit: 10})
	if out.Total != 1 {
		t.Fatalf("expected 1 error record, got %d", out.Total)
	}
	if !strings.Contains(out.Items[0].Error, "90") {
		t.Fatalf("persisted error not readable: %q", out.Items[0].Error)
	}
}

// Batch with one invalid item in the middle: the other items still succeed and
// the failure names both its index and the offending parameter.
func TestBatchPartialFailure(t *testing.T) {
	s := testService()
	items := []json.RawMessage{
		mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30}),
		mustBody(t, map[string]any{"wheelbase_m": 0, "track_m": 1.6, "inner_angle": 30}),
		mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": "banana"}),
		mustBody(t, map[string]any{"wheelbase_m": 2.5, "track_m": 1.5, "inner_angle": -20}),
	}
	raw, _ := json.Marshal(map[string]any{"items": items})
	resp := s.Batch(context.Background(), raw)
	if !resp.OK {
		t.Fatalf("batch envelope should be ok even with bad items: %+v", resp)
	}
	if resp.Total != 4 || resp.Succeeded != 2 || resp.Failed != 2 {
		t.Fatalf("counts wrong: %+v", resp)
	}
	if resp.Items[0].Index != 0 || !resp.Items[0].OK {
		t.Fatal("item 0 should succeed")
	}
	if resp.Items[1].OK || !mentions(resp.Items[1].Error, "wheelbase_m") {
		t.Fatalf("item 1 should fail on wheelbase_m: %+v", resp.Items[1].Error)
	}
	if resp.Items[2].OK || !mentions(resp.Items[2].Error, "inner_angle") {
		t.Fatalf("item 2 should fail on inner_angle: %+v", resp.Items[2].Error)
	}
	if !resp.Items[3].OK || resp.Items[3].Result.TurnDirection != "right" {
		t.Fatalf("item 3 should succeed as right turn: %+v", resp.Items[3])
	}

	// The four item records are persisted under the batch id; the batch
	// summary itself is stored separately and fetched by its own id.
	out, _ := s.History(context.Background(), HistoryQuery{BatchID: resp.BatchID, Limit: 50})
	if out.Total != 4 {
		t.Fatalf("expected 4 item records under batch_id, got %d", out.Total)
	}
	for _, it := range out.Items {
		if it.Kind != store.KindBatch {
			t.Fatalf("item kind wrong: %s", it.Kind)
		}
	}
	summary, ok, err := s.GetRecord(context.Background(), resp.BatchID)
	if err != nil || !ok {
		t.Fatalf("batch summary not persisted: ok=%v err=%v", ok, err)
	}
	if summary.Kind != store.KindBatch || summary.Status != store.StatusError {
		t.Fatalf("summary metadata wrong: %+v", summary)
	}

	// Kind filter returns all five batch-related rows (summary + items).
	batchKind, _ := s.History(context.Background(), HistoryQuery{Kind: store.KindBatch, Limit: 50})
	if batchKind.Total != 5 {
		t.Fatalf("expected 5 batch-kind rows, got %d", batchKind.Total)
	}
}

func TestBatchStructuralErrors(t *testing.T) {
	s := testService()
	for _, body := range []string{
		`{"items":[]}`, `{"items":"nope"}`, `{}`, `not json`,
	} {
		resp := s.Batch(context.Background(), json.RawMessage(body))
		if resp.OK {
			t.Fatalf("body %q should be rejected", body)
		}
	}
}

func TestHistoryFilters(t *testing.T) {
	s := testService()
	ctx := context.Background()
	s.Calc(ctx, mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30}), "calculate", false)
	s.Calc(ctx, mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": -30}), "radii", true)
	s.Calc(ctx, mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 90}), "calculate", false)

	ok, _ := s.History(ctx, HistoryQuery{Status: store.StatusOK, Limit: 50})
	if ok.Total != 2 {
		t.Fatalf("want 2 ok records, got %d", ok.Total)
	}
	left, _ := s.History(ctx, HistoryQuery{Direction: "left", Limit: 50})
	if left.Total != 1 {
		t.Fatalf("want 1 left, got %d", left.Total)
	}
	fromRadii, _ := s.History(ctx, HistoryQuery{Source: "radii", Limit: 50})
	if fromRadii.Total != 1 || fromRadii.Items[0].Result == nil {
		t.Fatalf("source filter wrong: %+v", fromRadii)
	}
	// Radius range filter.
	big, _ := s.History(ctx, HistoryQuery{MinRadius: 999, Limit: 50})
	if big.Total != 0 {
		t.Fatalf("min radius filter failed: %d", big.Total)
	}
}

// Concurrency: many goroutines with distinct inputs must never see another
// request's data, and every request must land in history exactly once.
func TestConcurrentRequestsIsolated(t *testing.T) {
	s := testService()
	const n = 80
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inner := 5.0 + float64(i%80) // 5..84 degrees, all valid
			if i%2 == 1 {
				inner = -inner
			}
			body := mustBody(t, map[string]any{
				"wheelbase_m": 2.0 + float64(i)*0.01,
				"track_m":     1.0 + float64(i%5)*0.1,
				"inner_angle": inner,
			})
			resp := s.Calc(context.Background(), body, "calculate", true)
			if !resp.OK {
				errCh <- errWrap(i, resp.Error)
				return
			}
			// The response must correspond exactly to this request's sign.
			if inner > 0 && resp.Result.TurnDirection != "left" {
				errCh <- errWrap(i, errSimple("expected left"))
			}
			if inner < 0 && resp.Result.TurnDirection != "right" {
				errCh <- errWrap(i, errSimple("expected right"))
			}
			// Wheels and residuals must be consistent and never cross-talk.
			if resp.Result.MaxResidualMM > 1e-6 {
				errCh <- errWrap(i, errSimple("residual too large"))
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Error(e)
	}
	out, _ := s.History(context.Background(), HistoryQuery{Status: store.StatusOK, Limit: 1000})
	if out.Total != n {
		t.Fatalf("expected %d persisted records, got %d", n, out.Total)
	}
}

// Same-input concurrency still yields independent, correctly formed responses.
func TestConcurrentIdenticalRequests(t *testing.T) {
	s := testService()
	const n = 50
	var wg sync.WaitGroup
	ids := sync.Map{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30})
			resp := s.Calc(context.Background(), body, "radii", true)
			if !resp.OK {
				t.Errorf("calc: %+v", resp.Error)
				return
			}
			if resp.Result.OuterAngle < 23 || resp.Result.OuterAngle > 24 {
				t.Errorf("outer angle drifted: %v", resp.Result.OuterAngle)
			}
			if len(resp.Result.Wheels) != 4 {
				t.Errorf("wheels missing: %d", len(resp.Result.Wheels))
			}
			ids.Store(resp.RecordID, struct{}{})
		}()
	}
	wg.Wait()
	count := 0
	ids.Range(func(_, _ any) bool { count++; return true })
	if count != n {
		t.Fatalf("expected %d unique record ids, got %d", n, count)
	}
}

func TestPresetExample(t *testing.T) {
	s := testService()
	p := s.PresetExample(context.Background())
	if !p.Response.OK {
		t.Fatal("preset must be valid")
	}
	r := p.Response.Result
	if r.InnerAngle != 30 || r.OuterAngle >= 28 || r.OuterAngle <= 20 {
		t.Fatalf("preset outer angle should be clearly < 30, got %v", r.OuterAngle)
	}
	if len(r.Wheels) != 4 || r.MaxResidualMM > 1e-6 {
		t.Fatalf("preset wheel layout inconsistent: %+v", r.Wheels)
	}
}

// End-to-end geometry identity through the service layer: cot difference.
func TestServiceCotIdentity(t *testing.T) {
	s := testService()
	resp := s.Calc(context.Background(),
		mustBody(t, map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30}),
		"calculate", false)
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	diff := geometry.CotCheck(resp.Result.InnerAngle, resp.Result.OuterAngle)
	if d := diff - 1.6/2.7; d > 1e-9 || d < -1e-9 {
		t.Fatalf("cot diff %.10f != T/L %.10f", diff, 1.6/2.7)
	}
}

func mentions(e *validate.Error, field string) bool {
	if e == nil {
		return false
	}
	for _, f := range e.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}

type idxErr struct {
	idx int
	msg string
}

func (e idxErr) Error() string { return e.msg }

func errWrap(i int, e error) error { return idxErr{idx: i, msg: e.Error()} }
func errSimple(m string) error     { return &simpleErr{m} }

type simpleErr struct{ m string }

func (e *simpleErr) Error() string { return e.m }
