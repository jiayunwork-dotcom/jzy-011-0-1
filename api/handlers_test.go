package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ackermann-service/ackermann"
	"ackermann-service/storage"
)

func newTestServer() (*httptest.Server, storage.Store) {
	st := storage.NewMemoryStore()
	srv := NewServer(st, ackermann.DefaultTol)
	return httptest.NewServer(srv.Routes()), st
}

func doJSON(t *testing.T, url, method, body string) (int, map[string]interface{}) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

const validBody = `{"wheelbase":2.7,"track":1.55,"inside_angle":30}`

func TestCalcSuccess(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	status, v := doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", validBody)
	if status != 200 {
		t.Fatalf("status %d body %v", status, v)
	}
	res := v["result"].(map[string]interface{})
	outside := res["outside_angle_deg"].(float64)
	if outside <= 20 || outside >= 25 {
		t.Errorf("outside angle ~23.4 expected, got %v", outside)
	}
	if outside >= 30 {
		t.Error("outside must be below inside 30")
	}
	radius := res["radius"].(float64)
	if radius <= 0 {
		t.Errorf("radius must be finite positive, got %v", radius)
	}
	if v["record_id"] == nil {
		t.Error("record_id must be returned")
	}
	if res["geometry_ok"] != true {
		t.Errorf("geometry_ok: %v", res["geometry_ok"])
	}
}

func TestCalcStraightHasNullRadii(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	status, v := doJSON(t, ts.URL+"/api/v1/steering/calc", "POST",
		`{"wheelbase":2.7,"track":1.55,"inside_angle":0}`)
	if status != 200 {
		t.Fatalf("status %d %v", status, v)
	}
	res := v["result"].(map[string]interface{})
	if res["radius"] != nil || res["center"] != nil {
		t.Errorf("straight driving radii/center must be null, got %v %v", res["radius"], res["center"])
	}
	if res["outside_angle_deg"].(float64) != 0 {
		t.Error("outside angle must be 0")
	}
}

func TestWheelsEndpoint(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	status, v := doJSON(t, ts.URL+"/api/v1/steering/wheels", "POST", validBody)
	if status != 200 {
		t.Fatalf("status %d %v", status, v)
	}
	res := v["result"].(map[string]interface{})
	center := res["center"].(map[string]interface{})
	if center["x"].(float64) != 0 || center["y"].(float64) <= 0 {
		t.Errorf("bad center %v", center)
	}
	wheels := res["wheels"].(map[string]interface{})
	if len(wheels) != 4 {
		t.Fatalf("want 4 wheels, got %d", len(wheels))
	}
	for _, name := range []string{"rear_inside", "rear_outside", "front_inside", "front_outside"} {
		if _, ok := wheels[name]; !ok {
			t.Errorf("wheel %s missing", name)
		}
	}
	ratios := res["arc_ratios"].(map[string]interface{})
	if ratios["rear_outer_to_rear_inner"].(float64) <= 1 {
		t.Error("outer arc must exceed inner arc")
	}
}

func TestValidationErrors(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	cases := []struct {
		body      string
		wantCode  string
		wantField string
	}{
		{`{"track":1.55,"inside_angle":30}`, "missing_field", "wheelbase"},
		{`{"wheelbase":"2.7","track":1.55,"inside_angle":30}`, "invalid_number", "wheelbase"},
		{`{"wheelbase":0,"track":1.55,"inside_angle":30}`, "non_positive_dimension", "wheelbase"},
		{`{"wheelbase":2.7,"track":-1,"inside_angle":30}`, "non_positive_dimension", "track"},
		{`{"wheelbase":2.7,"track":1.55,"inside_angle":90}`, "angle_out_of_range", "inside_angle"},
		{`{"wheelbase":2.7,"track":1.55,"inside_angle":-91}`, "angle_out_of_range", "inside_angle"},
		{`{"wheelbase":2.7,"track":1.55,"inside_angle":400}`, "angle_unit_contradiction", "inside_angle"},
		{`{"wheelbase":2.7,"track":1.55,"inside_angle":10,"angle_unit":"rad"}`, "angle_unit_contradiction", "inside_angle"},
		{`{"wheelbase":2.7,"track":1.55,"inside_angle":30,"angle_unit":"turns"}`, "unknown_angle_unit", "angle_unit"},
		{`not json`, "invalid_json", ""},
		{`{}`, "empty_body", ""},
	}
	for _, c := range cases {
		status, v := doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", c.body)
		if status == 200 {
			t.Errorf("body %s: must be rejected", c.body)
			continue
		}
		env, ok := v["error"].(map[string]interface{})
		if !ok {
			t.Errorf("body %s: missing error envelope: %v", c.body, v)
			continue
		}
		if env["code"] != c.wantCode {
			t.Errorf("body %s: code = %v, want %s", c.body, env["code"], c.wantCode)
		}
		if c.wantField != "" {
			details := env["details"].(map[string]interface{})
			if details["field"] != c.wantField {
				t.Errorf("body %s: field = %v, want %s", c.body, details["field"], c.wantField)
			}
		}
		if env["message"] == nil || env["message"] == "" {
			t.Errorf("body %s: readable message required", c.body)
		}
	}
}

// Radians are accepted and converted.
func TestRadiansAccepted(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	// pi/6 rad = 30 deg, same geometry as the degree request.
	status, v := doJSON(t, ts.URL+"/api/v1/steering/calc", "POST",
		`{"wheelbase":2.7,"track":1.55,"inside_angle":0.5235987755982988,"angle_unit":"rad"}`)
	if status != 200 {
		t.Fatalf("status %d %v", status, v)
	}
	res := v["result"].(map[string]interface{})
	if d := res["inside_angle_deg"].(float64); d < 29.9999 || d > 30.0001 {
		t.Errorf("rad conversion: inside=%v", d)
	}
}

func TestBatchPartialFailure(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	body := `{"items":[
		{"wheelbase":2.7,"track":1.55,"inside_angle":30},
		{"wheelbase":-1,"track":1.55,"inside_angle":30},
		{"wheelbase":2.7,"track":1.55,"inside_angle":90},
		{"wheelbase":3.0,"track":1.6,"inside_angle":10}
	]}`
	status, v := doJSON(t, ts.URL+"/api/v1/steering/batch", "POST", body)
	if status != 200 {
		t.Fatalf("batch always 200 with per-item results, got %d %v", status, v)
	}
	if v["succeeded"].(float64) != 2 || v["failed"].(float64) != 2 {
		t.Fatalf("counts: %v", v)
	}
	items := v["items"].([]interface{})
	if len(items) != 4 {
		t.Fatalf("items %d", len(items))
	}
	second := items[1].(map[string]interface{})
	if second["success"] != false {
		t.Fatal("item 2 must fail")
	}
	errObj := second["error"].(map[string]interface{})
	details := errObj["details"].(map[string]interface{})
	if details["item_index"].(float64) != 1 {
		t.Errorf("must point at item index 1 (0-based), got %v", details["item_index"])
	}
	if details["field"] != "wheelbase" {
		t.Errorf("must name wheelbase, got %v", details["field"])
	}
	if !strings.Contains(errObj["message"].(string), "item 2") {
		t.Errorf("message must say which item (1-based): %v", errObj["message"])
	}
	third := items[2].(map[string]interface{})
	if third["error"].(map[string]interface{})["details"].(map[string]interface{})["field"] != "inside_angle" {
		t.Error("item 3 error must name inside_angle")
	}
	first := items[0].(map[string]interface{})
	fourth := items[3].(map[string]interface{})
	if first["success"] != true || fourth["success"] != true {
		t.Fatal("valid items must succeed despite other failures")
	}
}

func TestBatchEmptyRejected(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()
	status, v := doJSON(t, ts.URL+"/api/v1/steering/batch", "POST", `{"items":[]}`)
	if status != 400 {
		t.Fatalf("status %d %v", status, v)
	}
}

func TestHistoryPersistence(t *testing.T) {
	ts, st := newTestServer()
	defer ts.Close()

	doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", validBody)
	doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", `{"wheelbase":0,"track":1,"inside_angle":1}`)

	// Through the API.
	status, v := doJSON(t, ts.URL+"/api/v1/steering/history?limit=10", "GET", "")
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	if v["total"].(float64) != 2 {
		t.Fatalf("both success and failure must be persisted, total %v", v["total"])
	}
	recs := v["records"].([]interface{})
	if len(recs) != 2 {
		t.Fatalf("records %d", len(recs))
	}

	// Filters: only failures.
	_, v = doJSON(t, ts.URL+"/api/v1/steering/history?success=false", "GET", "")
	for _, r := range v["records"].([]interface{}) {
		if r.(map[string]interface{})["success"] != false {
			t.Fatal("filter success=false leaked a success row")
		}
	}

	// Directly from the store: inputs and error detail survive.
	all, _ := st.Query(nil, storage.Filter{})
	var failed storage.Record
	for _, r := range all {
		if !r.Success {
			failed = r
		}
	}
	if failed.ErrorCode == "" || failed.ErrorMessage == "" {
		t.Fatal("failure must persist error code and readable message")
	}
}

func TestHistoryDirectionFilter(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()
	doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", validBody)
	doJSON(t, ts.URL+"/api/v1/steering/calc", "POST",
		`{"wheelbase":2.7,"track":1.55,"inside_angle":-30}`)

	_, v := doJSON(t, ts.URL+"/api/v1/steering/history?direction=left", "GET", "")
	recs := v["records"].([]interface{})
	for _, r := range recs {
		res := r.(map[string]interface{})["result"].(map[string]interface{})
		if res != nil && res["direction"] != "left" {
			t.Fatalf("direction filter leaked %v", res["direction"])
		}
	}
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 left record, got %d", len(recs))
	}
}

func TestConfigAndHealth(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()

	status, v := doJSON(t, ts.URL+"/api/v1/steering/config", "GET", "")
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	if v["angle_unit"] != "deg" {
		t.Errorf("angle_unit echo: %v", v["angle_unit"])
	}
	if v["geometry_abs_tolerance"] == nil {
		t.Error("tolerance must be echoed")
	}

	status, v = doJSON(t, ts.URL+"/healthz", "GET", "")
	if status != 200 || v["status"] != "ok" {
		t.Fatalf("healthz %d %v", status, v)
	}
	status, v = doJSON(t, ts.URL+"/readyz", "GET", "")
	if status != 200 || v["status"] != "ready" {
		t.Fatalf("readyz %d %v", status, v)
	}
}

func TestPresetEndpoint(t *testing.T) {
	ts, _ := newTestServer()
	defer ts.Close()
	status, v := doJSON(t, ts.URL+"/api/v1/steering/preset", "GET", "")
	if status != 200 {
		t.Fatalf("status %d %v", status, v)
	}
	res := v["result"].(map[string]interface{})
	if res["outside_angle_deg"].(float64) >= 30 {
		t.Error("preset outside must be clearly below 30")
	}
}

// TestConcurrentRequestsNoCrossTalk fires many concurrent calculations with
// distinct wheelbases and verifies every response belongs to its own request;
// history must contain exactly that many records with no intermixing.
func TestConcurrentRequestsNoCrossTalk(t *testing.T) {
	ts, st := newTestServer()
	defer ts.Close()

	const n = 80
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			L := 2.0 + float64(i)*0.05 // unique per request
			body := fmt.Sprintf(`{"wheelbase":%g,"track":1.55,"inside_angle":30}`, L)
			status, v := doJSON(t, ts.URL+"/api/v1/steering/calc", "POST", body)
			if status != 200 {
				errs <- fmt.Errorf("req %d status %d", i, status)
				return
			}
			res := v["result"].(map[string]interface{})
			if got := res["wheelbase"].(float64); got != L {
				errs <- fmt.Errorf("cross-talk: req L=%g got wheelbase %g", L, got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	cnt, _ := st.Count(nil)
	if cnt != n {
		t.Fatalf("history count %d want %d", cnt, n)
	}
}
