// Package store persists steering calculation requests and results.
//
// Two backends are provided: an in-memory store used by default and in tests,
// and a PostgreSQL store selected in production. Both implement the same
// interface so HTTP/concurrency behavior is identical across them.
package store

import (
	"context"
	"time"
)

// Status values for persisted records.
const (
	StatusOK    = "ok"
	StatusError = "error"

	KindSingle = "single"
	KindBatch  = "batch"
)

// Record is one persisted calculation request/result pair. Batch requests
// persist one "batch" summary plus one child record per item.
type Record struct {
	ID        string
	BatchID   string // empty for single requests and batch summaries
	Index     int    // item index within a batch; -1 for non-item records
	Kind      string
	Source    string // "calculate" | "radii" | "batch"
	Status    string
	Wheelbase float64 // zero when validation failed before parsing
	Track     float64
	InnerDeg  float64
	AngleUnit string
	Direction string // "left" | "right" | "straight" | "invalid"
	Radius    *float64
	Request   []byte // canonical request JSON
	Result    []byte // result/error JSON as returned to the caller
	Error     string // human readable error when Status == error
	CreatedAt time.Time
}

// Filter narrows a history query. Zero-valued fields are ignored.
type Filter struct {
	Status    string
	Kind      string
	Source    string
	Direction string
	BatchID   string
	MinRadius float64
	MaxRadius float64
	Since     time.Time
	Until     time.Time
	Limit     int
	Offset    int
}

// Store is the persistence interface. All implementations must be safe for
// concurrent use: simultaneous requests must never interleave their records.
type Store interface {
	// Save persists one record and returns the stored copy (with ID and
	// CreatedAt populated).
	Save(ctx context.Context, r Record) (Record, error)
	// Get fetches one record by id.
	Get(ctx context.Context, id string) (Record, bool, error)
	// Query returns records matching f (newest first) and the total count
	// before limit/offset are applied.
	Query(ctx context.Context, f Filter) ([]Record, int, error)
	// Ping is used by the readiness probe.
	Ping(ctx context.Context) error
	// Close releases underlying resources.
	Close() error
}
