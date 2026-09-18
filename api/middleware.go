package api

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// logging emits one structured-ish line per request.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.RequestURI(), sw.status, time.Since(start))
	})
}

// recoverer converts an unexpected panic into a JSON 500 so a bad input can
// never take the process down.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v\n%s", rec, debug.Stack())
				e := fieldErr(http.StatusInternalServerError, "internal_error",
					"unexpected internal error; request rejected without result", "", "", nil)
				writeJSON(w, http.StatusInternalServerError, e)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
