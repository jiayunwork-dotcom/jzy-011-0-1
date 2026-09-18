package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// genID builds a time-sortable identifier from a millisecond timestamp and a
// random suffix. Sorting ids lexicographically roughly matches creation order.
func genID(seq int64) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("rec_%d_%s_%d", time.Now().UTC().UnixMilli(), hex.EncodeToString(b[:]), seq)
}

// NewID generates a random-backed identifier without a sequence number (used
// by the PostgreSQL backend, where a sequence column is unnecessary).
func NewID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixMilli(), hex.EncodeToString(b[:]))
}
