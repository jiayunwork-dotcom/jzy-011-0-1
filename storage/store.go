// Package storage persists steering calculation requests and their results so
// they can later be queried. Two implementations are provided:
//
//	MemoryStore  - zero dependency, used by tests and standalone runs
//	PostgresStore - durable, used when DATABASE_URL / DSN is configured
package storage

import (
	"context"
	"time"
)

// Record is one persisted calculation request with its outcome.
type Record struct {
	ID           int64       `json:"id"`
	BatchID      *string     `json:"batch_id,omitempty"`
	BatchIndex   *int        `json:"batch_index,omitempty"`
	Kind         string      `json:"kind"` // "single" | "batch_item"
	CreatedAt    time.Time   `json:"created_at"`
	AngleUnit    string      `json:"angle_unit"`
	Wheelbase    *float64    `json:"wheelbase,omitempty"`
	Track        *float64    `json:"track,omitempty"`
	InsideAngle  *float64    `json:"inside_angle_deg,omitempty"`
	Success      bool        `json:"success"`
	ErrorCode    string      `json:"error_code,omitempty"`
	ErrorMessage string      `json:"error_message,omitempty"`
	Result       interface{} `json:"result,omitempty"`
}

// Filter narrows a history query. Zero values mean "no constraint".
type Filter struct {
	Success   *bool
	Kind      string
	Direction string
	BatchID   string
	Since     *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
}

// Store is the persistence contract.
type Store interface {
	Insert(ctx context.Context, r *Record) (*Record, error)
	Query(ctx context.Context, f Filter) ([]Record, error)
	Count(ctx context.Context) (int64, error)
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// NormalizeLimit applies sane bounds for list queries.
func NormalizeLimit(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
