package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"ackermann-service/ackermann"
	"ackermann-service/storage"
)

// Config holds runtime geometry/service settings echoed by GET /config.
type Config struct {
	AngleUnit            string   `json:"angle_unit"`
	AngleUnitsAccepted   []string `json:"angle_units_accepted"`
	GeometryAbsTolerance float64  `json:"geometry_abs_tolerance"`
	GeometryRelTolerance float64  `json:"geometry_rel_tolerance"`
}

// Server wires the geometry engine and persistence to HTTP handlers.
type Server struct {
	store storage.Store
	cfg   Config
	tol   ackermann.Tol
}

func NewServer(store storage.Store, tol ackermann.Tol) *Server {
	return &Server{
		store: store,
		cfg: Config{
			AngleUnit:            "deg",
			AngleUnitsAccepted:   []string{"deg", "rad"},
			GeometryAbsTolerance: tol.Abs,
			GeometryRelTolerance: tol.Rel,
		},
		tol: tol,
	}
}

// Routes returns the mounted mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/steering/calc", s.handleCalc)
	mux.HandleFunc("POST /api/v1/steering/wheels", s.handleWheels)
	mux.HandleFunc("POST /api/v1/steering/batch", s.handleBatch)
	mux.HandleFunc("GET /api/v1/steering/history", s.handleHistory)
	mux.HandleFunc("GET /api/v1/steering/config", s.handleConfig)
	mux.HandleFunc("GET /api/v1/steering/preset", s.handlePreset)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	return logging(recoverer(mux))
}

type itemResult struct {
	Index   int         `json:"index"`
	Success bool        `json:"success"`
	Error   *apiError   `json:"error,omitempty"`
	Result  interface{} `json:"result,omitempty"`
}

// MarshalJSON flattens the inner error envelope so a failed batch item carries
// {"error":{"code","message","details"}} rather than an extra nesting level.
func (ir itemResult) MarshalJSON() ([]byte, error) {
	type alias struct {
		Index   int         `json:"index"`
		Success bool        `json:"success"`
		Error   interface{} `json:"error,omitempty"`
		Result  interface{} `json:"result,omitempty"`
	}
	var errPayload interface{}
	if ir.Error != nil {
		errPayload = ir.Error.Error
	}
	return json.Marshal(alias{ir.Index, ir.Success, errPayload, ir.Result})
}

func (s *Server) handleCalc(w http.ResponseWriter, r *http.Request) {
	fields, e := decodeObject(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, e := parseSingle(fields, nil)
	if e != nil {
		s.persistFailure(r, fields, p, "single", nil, nil, e)
		writeError(w, e)
		return
	}
	res, err := ackermann.Compute(p.Input, s.tol)
	if err != nil {
		e2 := mapGeomError(err, nil)
		s.persistFailure(r, fields, p, "single", nil, nil, e2)
		writeError(w, e2)
		return
	}
	rec := s.persistSuccess(r, p, "single", nil, nil, res)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"record_id": rec.ID, "result": res,
	})
}

func (s *Server) handleWheels(w http.ResponseWriter, r *http.Request) {
	fields, e := decodeObject(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, e := parseSingle(fields, nil)
	if e != nil {
		s.persistFailure(r, fields, p, "single", nil, nil, e)
		writeError(w, e)
		return
	}
	res, err := ackermann.Compute(p.Input, s.tol)
	if err != nil {
		e2 := mapGeomError(err, nil)
		s.persistFailure(r, fields, p, "single", nil, nil, e2)
		writeError(w, e2)
		return
	}
	rec := s.persistSuccess(r, p, "single", nil, nil, res)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"record_id": rec.ID,
		"result":    wheelsView(res),
	})
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, fieldErr(400, "invalid_json",
			"request body must be a JSON object with an \"items\" array: "+err.Error(),
			"items", "invalid_json", nil))
		return
	}
	if len(body.Items) == 0 {
		writeError(w, fieldErr(400, "empty_batch",
			"items must contain at least one entry", "items", "missing_field", nil))
		return
	}

	batchID := newBatchID()
	out := make([]itemResult, 0, len(body.Items))
	successCount, failCount := 0, 0

	for i, fields := range body.Items {
		idx := i
		p, e := parseSingle(fields, &idx)
		if e != nil {
			failCount++
			s.persistFailure(r, fields, p, "batch_item", &batchID, &idx, e)
			out = append(out, itemResult{Index: idx, Success: false, Error: e})
			continue
		}
		res, err := ackermann.Compute(p.Input, s.tol)
		if err != nil {
			e2 := mapGeomError(err, &idx)
			failCount++
			s.persistFailure(r, fields, p, "batch_item", &batchID, &idx, e2)
			out = append(out, itemResult{Index: idx, Success: false, Error: e2})
			continue
		}
		successCount++
		s.persistSuccess(r, p, "batch_item", &batchID, &idx, res)
		out = append(out, itemResult{Index: idx, Success: true, Result: res})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"batch_id":  batchID,
		"total":     len(out),
		"succeeded": successCount,
		"failed":    failCount,
		"items":     out,
	})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.Filter{
		Kind:      q.Get("kind"),
		BatchID:   q.Get("batch_id"),
		Direction: q.Get("direction"),
	}
	if v := q.Get("success"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, fieldErr(400, "invalid_query",
				"success must be true or false", "success", "invalid", nil))
			return
		}
		f.Success = &b
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, fieldErr(400, "invalid_query",
				"since must be RFC3339 timestamp", "since", "invalid", nil))
			return
		}
		f.Since = &t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, fieldErr(400, "invalid_query",
				"until must be RFC3339 timestamp", "until", "invalid", nil))
			return
		}
		f.Until = &t
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	f.Limit, f.Offset = storage.NormalizeLimit(limit, offset)

	recs, err := s.store.Query(r.Context(), f)
	if err != nil {
		writeError(w, fieldErr(500, "storage_error", "failed to query history: "+err.Error(), "", "", nil))
		return
	}
	total, _ := s.store.Count(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":    total,
		"returned": len(recs),
		"records":  recs,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg)
}

func (s *Server) handlePreset(w http.ResponseWriter, _ *http.Request) {
	res, err := ackermann.Compute(ackermann.PresetInput, s.tol)
	if err != nil {
		writeError(w, fieldErr(500, "internal_error", err.Error(), "", "", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"description": "Passenger car: wheelbase 2.7 m, track 1.55 m, inside wheel 30 degrees",
		"request":     ackermann.PresetInput,
		"result":      res,
	})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, fieldErr(503, "not_ready", "storage unavailable: "+err.Error(), "", "", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// wheelsView narrows a full result to the four-wheel-radius interface payload.
func wheelsView(res *ackermann.Result) interface{} {
	return map[string]interface{}{
		"direction":              res.Direction,
		"straight":               res.Straight,
		"inside_angle_deg":       res.InsideAngleDeg,
		"outside_angle_deg":      res.OutsideAngleDeg,
		"radius":                 res.Radius,
		"center":                 res.Center,
		"rear_inside_radius":     res.RearInsideRadius,
		"rear_outside_radius":    res.RearOutsideRadius,
		"front_inside_radius":    res.FrontInsideRadius,
		"front_outside_radius":   res.FrontOutsideRadius,
		"wheels":                 res.Wheels,
		"arc_ratios":             res.Arc,
		"geometry_ok":            res.GeometryOK,
		"geometry_max_deviation": res.MaxDeviation,
	}
}

// ---- persistence helpers ----

func (s *Server) persistSuccess(r *http.Request, p parsedInput, kind string,
	batchID *string, idx *int, res *ackermann.Result) *storage.Record {
	rec := &storage.Record{
		BatchID:     batchID,
		BatchIndex:  idx,
		Kind:        kind,
		AngleUnit:   p.AngleUnit,
		Wheelbase:   &p.Wheelbase,
		Track:       &p.Track,
		InsideAngle: &p.InsideAngle,
		Success:     true,
		Result:      res,
	}
	saved, err := s.store.Insert(r.Context(), rec)
	if err != nil {
		// Persistence failure must not hide or corrupt the calculation;
		// the caller still returns the result.
		log.Printf("persist success record failed: %v", err)
		return rec
	}
	return saved
}

func (s *Server) persistFailure(r *http.Request, _ map[string]json.RawMessage,
	p parsedInput, kind string, batchID *string, idx *int, e *apiError) {
	rec := &storage.Record{
		BatchID:      batchID,
		BatchIndex:   idx,
		Kind:         kind,
		AngleUnit:    p.AngleUnit,
		Success:      false,
		ErrorCode:    e.Error.Code,
		ErrorMessage: e.Error.Message,
	}
	if p.Wheelbase != 0 {
		wb := p.Wheelbase
		rec.Wheelbase = &wb
	}
	if p.Track != 0 {
		tr := p.Track
		rec.Track = &tr
	}
	// Zero is a legal (straight) value, so presence can't be inferred from
	// "!= 0"; the parse layer records it whenever inside_angle decoded.
	if p.insidePresent {
		ia := p.InsideAngle
		rec.InsideAngle = &ia
	}
	if _, err := s.store.Insert(r.Context(), rec); err != nil {
		log.Printf("persist failure record failed: %v", err)
	}
}

// ---- small helpers ----

func decodeObject(r *http.Request) (map[string]json.RawMessage, *apiError) {
	fields := map[string]json.RawMessage{}
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		return nil, fieldErr(400, "invalid_json",
			"request body must be a JSON object: "+err.Error(), "", "invalid_json", nil)
	}
	if len(fields) == 0 {
		return nil, fieldErr(400, "empty_body", "request body must not be empty", "", "missing_field", nil)
	}
	return fields, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, e *apiError) {
	status := e.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, e)
}

func newBatchID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
