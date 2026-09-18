package service

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"ackermann/internal/store"
	"ackermann/internal/validate"
)

// HistoryQuery are the parsed query-string filters.
type HistoryQuery struct {
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

// History runs a filtered query against the store.
func (s *Service) History(ctx context.Context, q HistoryQuery) (HistoryResponse, *validate.Error) {
	recs, total, err := s.store.Query(ctx, store.Filter{
		Status:    q.Status,
		Kind:      q.Kind,
		Source:    q.Source,
		Direction: q.Direction,
		BatchID:   q.BatchID,
		MinRadius: q.MinRadius,
		MaxRadius: q.MaxRadius,
		Since:     q.Since,
		Until:     q.Until,
		Limit:     q.Limit,
		Offset:    q.Offset,
	})
	if err != nil {
		return HistoryResponse{}, toValidateError("history_query_failed", err.Error())
	}
	out := HistoryResponse{Total: total, Items: []RecordDTO{}}
	for _, r := range recs {
		out.Items = append(out.Items, toRecordDTO(r))
	}
	out.Count = len(out.Items)
	return out, nil
}

// GetRecord fetches one record by id.
func (s *Service) GetRecord(ctx context.Context, id string) (RecordDTO, bool, error) {
	r, ok, err := s.store.Get(ctx, id)
	if err != nil || !ok {
		return RecordDTO{}, ok, err
	}
	return toRecordDTO(r), true, nil
}

func toRecordDTO(r store.Record) RecordDTO {
	dto := RecordDTO{
		ID:        r.ID,
		BatchID:   r.BatchID,
		Index:     r.Index,
		Kind:      r.Kind,
		Source:    r.Source,
		Status:    r.Status,
		Request:   json.RawMessage(r.Request),
		Result:    json.RawMessage(r.Result),
		Error:     r.Error,
		Direction: r.Direction,
		Radius:    r.Radius,
		CreatedAt: r.CreatedAt,
	}
	if dto.Result == nil {
		dto.Result = json.RawMessage("null")
	}
	if dto.Request == nil {
		dto.Request = json.RawMessage("null")
	}
	return dto
}

// ParseHistoryQuery parses query parameters and reports the offending
// parameter name in a field error when conversion fails.
func ParseHistoryQuery(get func(string) string, has func(string) bool) (HistoryQuery, []FieldParam) {
	var q HistoryQuery
	var bad []FieldParam
	q.Status = normEnum(get("status"), map[string]string{
		"ok": store.StatusOK, "success": store.StatusOK,
		"error": store.StatusError, "failed": store.StatusError,
	})
	q.Kind = normEnum(get("kind"), map[string]string{
		"single": store.KindSingle, "batch": store.KindBatch, "item": store.KindBatch,
	})
	q.Source = get("source")
	q.Direction = normEnum(get("direction"), map[string]string{
		"left": "left", "right": "right", "straight": "straight", "invalid": "invalid",
	})
	q.BatchID = get("batch_id")

	if has("min_radius_m") {
		if v, ok := parseFloat(get("min_radius_m")); ok {
			q.MinRadius = v
		} else {
			bad = append(bad, FieldParam{Param: "min_radius_m", Value: get("min_radius_m")})
		}
	}
	if has("max_radius_m") {
		if v, ok := parseFloat(get("max_radius_m")); ok {
			q.MaxRadius = v
		} else {
			bad = append(bad, FieldParam{Param: "max_radius_m", Value: get("max_radius_m")})
		}
	}
	if has("since") {
		if v, ok := parseTime(get("since")); ok {
			q.Since = v
		} else {
			bad = append(bad, FieldParam{Param: "since", Value: get("since")})
		}
	}
	if has("until") {
		if v, ok := parseTime(get("until")); ok {
			q.Until = v
		} else {
			bad = append(bad, FieldParam{Param: "until", Value: get("until")})
		}
	}
	q.Limit = 50
	q.Offset = 0
	if has("limit") {
		if v, err := strconv.Atoi(get("limit")); err == nil && v >= 0 {
			q.Limit = v
		} else {
			bad = append(bad, FieldParam{Param: "limit", Value: get("limit")})
		}
	}
	if has("offset") {
		if v, err := strconv.Atoi(get("offset")); err == nil && v >= 0 {
			q.Offset = v
		} else {
			bad = append(bad, FieldParam{Param: "offset", Value: get("offset")})
		}
	}
	return q, bad
}

// FieldParam identifies a malformed query-string parameter.
type FieldParam struct {
	Param string
	Value string
}

func normEnum(v string, m map[string]string) string {
	if v == "" {
		return ""
	}
	if out, ok := m[v]; ok {
		return out
	}
	return v
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
