package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ackermann/internal/service"
	"ackermann/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *service.Service) {
	t.Helper()
	st := store.NewMemory()
	svc := service.New(st, service.Config{AngleUnit: "deg", OutputAngleUnit: "deg", ToleranceMM: 1e-6})
	h := NewServer(svc, st, log.New(io.Discard, "", 0))
	return httptest.NewServer(h), svc
}

func post(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case string:
		rdr = strings.NewReader(b)
	case []byte:
		rdr = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	resp, err := http.Post(url, "application/json; charset=utf-8", rdr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return resp.StatusCode, m
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return resp.StatusCode, m
}

func TestHealthAndReady(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	for _, path := range []string{"/healthz", "/readyz"} {
		if code, m := get(t, srv.URL+path); code != http.StatusOK || m["status"] == "" {
			t.Fatalf("%s -> %d %v", path, code, m)
		}
	}
}

func TestConfigEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	code, m := get(t, srv.URL+"/api/v1/config")
	if code != http.StatusOK {
		t.Fatalf("config status %d", code)
	}
	if m["angle_unit"] != "deg" || m["output_angle_unit"] != "deg" {
		t.Fatalf("config did not echo angle units: %v", m)
	}
	if tol, ok := m["icr_tolerance_mm"].(float64); !ok || tol <= 0 {
		t.Fatalf("config did not echo geometry tolerance: %v", m)
	}
}

func TestPresetEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	code, m := get(t, srv.URL+"/api/v1/steering/preset")
	if code != http.StatusOK {
		t.Fatalf("preset status %d body=%v", code, m)
	}
	resp := m["response"].(map[string]any)
	result := resp["result"].(map[string]any)
	outer := result["outer_angle_deg"].(float64)
	if outer <= 20 || outer >= 28 {
		t.Fatalf("preset outer angle not clearly < 30: %v", outer)
	}
	wheels := result["wheels"].([]any)
	if len(wheels) != 4 {
		t.Fatalf("preset must include 4 wheels, got %d", len(wheels))
	}
}

func TestCalculateEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	code, m := post(t, srv.URL+"/api/v1/steering/calculate",
		map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30})
	if code != http.StatusOK || m["ok"] != true {
		t.Fatalf("calculate failed: %d %v", code, m)
	}
	result := m["result"].(map[string]any)
	if result["turn_direction"] != "left" {
		t.Fatalf("direction: %v", result["turn_direction"])
	}
	if _, has := result["wheels"]; has {
		t.Fatal("calculate endpoint should not include wheel layout; use /radii")
	}
	if m["record_id"] == "" {
		t.Fatal("record_id missing")
	}
}

func TestCalculateRadiansUnit(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	code, m := post(t, srv.URL+"/api/v1/steering/radii",
		map[string]any{"wheelbase_m": 2.7, "track_m": 1.6,
			"inner_angle": 0.5235987755982988, "angle_unit": "rad"})
	if code != http.StatusOK {
		t.Fatalf("radii rad failed: %d %v", code, m)
	}
	result := m["result"].(map[string]any)
	if outer := result["outer_angle_deg"].(float64); outer < 23 || outer > 24 {
		t.Fatalf("radian input produced wrong outer angle: %v", outer)
	}
	wheels := result["wheels"].([]any)
	for _, w := range wheels {
		wm := w.(map[string]any)
		if mathAbs(wm["icr_residual_mm"].(float64)) > 1e-6 {
			t.Fatalf("wheel residual too large: %v", wm)
		}
	}
	ic := result["instant_center_xy_m"].([]any)
	if ic[0].(float64) != 0 || ic[1].(float64) <= 0 {
		t.Fatalf("bad instant center: %v", ic)
	}
}

func TestCalculateRejections(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	url := srv.URL + "/api/v1/steering/calculate"

	cases := []struct {
		name string
		body any
		want int
		code string
	}{
		{"missing", map[string]any{"track_m": 1.6, "inner_angle": 30}, http.StatusBadRequest, "invalid_parameters"},
		{"non numeric", map[string]any{"wheelbase_m": "x", "track_m": 1.6, "inner_angle": 30}, http.StatusBadRequest, "invalid_parameters"},
		{"non finite body", `{"wheelbase_m":1e9999,"track_m":1.6,"inner_angle":30}`, http.StatusBadRequest, "invalid_parameters"},
		{"zero wheelbase", map[string]any{"wheelbase_m": 0, "track_m": 1.6, "inner_angle": 30}, http.StatusBadRequest, "invalid_parameters"},
		{"inner 90", map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 90}, http.StatusUnprocessableEntity, "inner_angle_out_of_range"},
		{"unit mismatch", map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30, "angle_unit": "rad"}, http.StatusBadRequest, "angle_unit_mismatch"},
		{"empty", "", http.StatusBadRequest, "empty_body"},
		{"garbage", "nonsense", http.StatusBadRequest, "invalid_json"},
	}
	for _, tc := range cases {
		code, m := post(t, url, tc.body)
		if code != tc.want {
			t.Errorf("%s: status = %d, want %d body=%v", tc.name, code, tc.want, m)
		}
		errObj, ok := m["error"].(map[string]any)
		if !ok {
			t.Errorf("%s: no readable error object: %v", tc.name, m)
			continue
		}
		if errObj["code"] != tc.code {
			t.Errorf("%s: error code = %v, want %s", tc.name, errObj["code"], tc.code)
		}
	}
}

func TestBatchEndpointPartialFailure(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	body := map[string]any{"items": []any{
		map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30},
		map[string]any{"wheelbase_m": -1, "track_m": 1.6, "inner_angle": 30},
		map[string]any{"wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 45},
	}}
	code, m := post(t, srv.URL+"/api/v1/steering/batch", body)
	if code != http.StatusOK {
		t.Fatalf("batch status %d", code)
	}
	if m["succeeded"].(float64) != 2 || m["failed"].(float64) != 1 {
		t.Fatalf("counts wrong: %v", m)
	}
	items := m["items"].([]any)
	bad := items[1].(map[string]any)
	if bad["ok"] != false {
		t.Fatal("item 1 should fail")
	}
	errObj := bad["error"].(map[string]any)
	fields := errObj["fields"].([]any)
	field0 := fields[0].(map[string]any)
	if field0["field"] != "wheelbase_m" {
		t.Fatalf("error should identify wheelbase_m on item 1: %v", fields)
	}
	idx := bad["index"].(float64)
	if idx != 1 {
		t.Fatalf("error should carry index 1, got %v", idx)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	srv, svc := newTestServer(t)
	defer srv.Close()
	ctx := context.Background()
	svc.Calc(ctx, []byte(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30}`), "calculate", false)

	code, m := get(t, srv.URL+"/api/v1/history?status=ok&limit=10")
	if code != http.StatusOK || m["total"].(float64) < 1 {
		t.Fatalf("history query failed: %d %v", code, m)
	}
	items := m["items"].([]any)
	first := items[0].(map[string]any)
	id := first["id"].(string)

	code, m = get(t, srv.URL+"/api/v1/history/"+id)
	if code != http.StatusOK || m["id"] != id {
		t.Fatalf("get one record failed: %d %v", code, m)
	}

	code, m = get(t, srv.URL+"/api/v1/history/does-not-exist")
	if code != http.StatusNotFound {
		t.Fatalf("missing record should be 404, got %d", code)
	}

	code, m = get(t, srv.URL+"/api/v1/history?limit=notanumber")
	if code != http.StatusBadRequest {
		t.Fatalf("bad query param should be 400, got %d: %v", code, m)
	}
}

func TestHTTPConcurrencyIsolation(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	const n = 60
	var wg sync.WaitGroup
	var failures sync.Map
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inner := 10.0 + float64(i%70)
			if i%2 == 0 {
				inner = -inner
			}
			code, m := post(t, srv.URL+"/api/v1/steering/radii", map[string]any{
				"wheelbase_m": 2.0 + float64(i)*0.01,
				"track_m":     1.0 + float64(i%4)*0.2,
				"inner_angle": inner,
			})
			if code != http.StatusOK || m["ok"] != true {
				failures.Store(i, m)
				return
			}
			result := m["result"].(map[string]any)
			wantDir := "left"
			if inner < 0 {
				wantDir = "right"
			}
			if result["turn_direction"] != wantDir {
				failures.Store(i, "direction cross-talk: "+result["turn_direction"].(string))
			}
		}(i)
	}
	wg.Wait()
	failures.Range(func(k, v any) bool {
		t.Errorf("request %v failed: %v", k, v)
		return true
	})
	code, m := get(t, srv.URL+"/api/v1/history?limit=1000")
	if code != http.StatusOK || int(m["total"].(float64)) != n {
		t.Fatalf("expected %d records, got %v", n, m["total"])
	}
}

func TestNoPanicOnWeirdBodies(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()
	for _, p := range []string{
		"/api/v1/steering/calculate",
		"/api/v1/steering/radii",
		"/api/v1/steering/batch",
	} {
		for _, body := range []string{`[]`, `null`, `{"wheelbase_m":{},"track_m":{},"inner_angle":{}}`, `123`} {
			resp, err := http.Post(srv.URL+p, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode == http.StatusInternalServerError {
				t.Fatalf("body %q to %s caused 500", body, p)
			}
			resp.Body.Close()
		}
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
