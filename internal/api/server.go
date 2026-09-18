// Package api exposes the steering geometry service over HTTP.
package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"ackermann/internal/service"
	"ackermann/internal/store"
	"ackermann/internal/validate"
)

// Server holds HTTP dependencies.
type Server struct {
	svc    *service.Service
	store  store.Store
	logger *log.Logger
}

// NewServer wires the service to its routes.
func NewServer(svc *service.Service, st store.Store, logger *log.Logger) http.Handler {
	s := &Server{svc: svc, store: st, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /api/v1/config", s.config)
	mux.HandleFunc("GET /api/v1/steering/preset", s.preset)
	mux.HandleFunc("POST /api/v1/steering/calculate", s.calculate)
	mux.HandleFunc("POST /api/v1/steering/radii", s.radii)
	mux.HandleFunc("POST /api/v1/steering/batch", s.batch)
	mux.HandleFunc("GET /api/v1/history", s.history)
	mux.HandleFunc("GET /api/v1/history/{id}", s.historyOne)

	return recoverer(mux, logger)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Config())
}

func (s *Server) preset(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.PresetExample(r.Context()))
}

func (s *Server) calculate(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	resp := s.svc.Calc(r.Context(), body, "calculate", false)
	status := http.StatusOK
	if !resp.OK {
		status = statusFor(resp.Error)
	}
	writeJSON(w, status, resp)
}

func (s *Server) radii(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	resp := s.svc.Calc(r.Context(), body, "radii", true)
	status := http.StatusOK
	if !resp.OK {
		status = statusFor(resp.Error)
	}
	writeJSON(w, status, resp)
}

func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	resp := s.svc.Batch(r.Context(), body)
	// A structurally invalid batch is a 400; a processed batch is always 200,
	// even when individual items failed.
	if !resp.OK {
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	q, bad := service.ParseHistoryQuery(r.URL.Query().Get, func(k string) bool {
		return r.URL.Query().Has(k)
	})
	if len(bad) > 0 {
		fields := make([]validate.FieldError, 0, len(bad))
		for _, b := range bad {
			fields = append(fields, validate.FieldError{
				Field:   b.Param,
				Message: "invalid query value \"" + b.Value + "\"",
			})
		}
		writeJSON(w, http.StatusBadRequest, validate.Error{
			Code: "invalid_query", Message: "one or more query parameters are invalid", Fields: fields})
		return
	}
	out, verr := s.svc.History(r.Context(), q)
	if verr != nil {
		writeJSON(w, http.StatusInternalServerError, verr)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) historyOne(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok, err := s.svc.GetRecord(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "record not found", "id": id})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// --- helpers -------------------------------------------------------------

// maxBodyBytes caps request bodies to keep the service bounded.
const maxBodyBytes = 1 << 20

func readBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable_body", "could not read request body: "+err.Error())
		return nil, false
	}
	if len(data) > maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB limit")
		return nil, false
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty_body", "request body is required")
		return nil, false
	}
	return json.RawMessage(data), true
}

// writeError always uses the same envelope shape as calculation errors:
// {"ok":false,"error":{"code":...,"message":...}}.
func writeError(w http.ResponseWriter, status int, code, msg string, fields ...validate.FieldError) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": validate.Error{Code: code, Message: msg, Fields: fields},
	})
}

func statusFor(e *validate.Error) int {
	switch e.Code {
	case "geometry_infeasible", "inner_angle_out_of_range", "concurrency_tolerance_exceeded":
		return http.StatusUnprocessableEntity
	case "persistence_failed", "history_query_failed":
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, `{"ok":false,"error":{"code":"encoding","message":"response encoding failed"}}`,
			http.StatusInternalServerError)
	}
}

// recoverer turns unexpected panics into 500s instead of crashing the process;
// validation guarantees mean this should never fire for bad input.
func recoverer(h http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Printf("panic serving %s: %v", r.URL.Path, rec)
				}
				writeJSON(w, http.StatusInternalServerError, validate.Error{
					Code: "internal_error", Message: "internal server error"})
			}
		}()
		h.ServeHTTP(w, r)
	})
}
