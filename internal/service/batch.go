package service

import (
	"context"
	"encoding/json"

	"ackermann/internal/store"
	"ackermann/internal/validate"
)

// MaxBatchItems bounds a single batch submission.
const MaxBatchItems = 1000

// batchRequest is the envelope of POST /batch.
type batchRequest struct {
	Items []json.RawMessage `json:"items"`
}

// Batch validates and calculates every item independently. One bad item never
// prevents the others from being computed; each item reports its own index and
// the offending parameter through its error.fields entries.
//
// Every item (success or failure) is persisted as a child of one batch record;
// the batch summary is persisted first and last updated with its outcome.
func (s *Service) Batch(ctx context.Context, raw json.RawMessage) BatchResponse {
	batchID := store.NewID("bat")
	summary := store.Record{
		ID:        batchID,
		BatchID:   "",
		Index:     -1,
		Kind:      store.KindBatch,
		Source:    "batch",
		Status:    store.StatusOK,
		Direction: "batch",
		Request:   requestJSON(raw),
		CreatedAt: s.now(),
	}

	var req batchRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Items == nil {
		verr := validate.Error{Code: "invalid_json",
			Message: "batch body must be a JSON object with an \"items\" array"}
		body, _ := json.Marshal(BatchResponse{OK: false, BatchID: batchID})
		summary.Status = store.StatusError
		summary.Error = verr.Message
		summary.Result = body
		_ = s.saveSummary(ctx, summary)
		return BatchResponse{OK: false, BatchID: batchID,
			Items: []ItemResult{{Index: -1, OK: false, Error: &verr}}}
	}
	if len(req.Items) == 0 {
		verr := validate.Error{Code: "empty_batch", Message: "\"items\" must contain at least one entry"}
		s.failSummary(ctx, summary, verr.Message)
		return BatchResponse{OK: false, BatchID: batchID,
			Items: []ItemResult{{Index: -1, OK: false, Error: &verr}}}
	}
	if len(req.Items) > MaxBatchItems {
		verr := validate.Error{Code: "batch_too_large",
			Message: errTooMany(len(req.Items))}
		s.failSummary(ctx, summary, verr.Message)
		return BatchResponse{OK: false, BatchID: batchID,
			Items: []ItemResult{{Index: -1, OK: false, Error: &verr}}}
	}

	out := BatchResponse{OK: true, BatchID: batchID, Total: len(req.Items), Items: make([]ItemResult, 0, len(req.Items))}
	for i, itemRaw := range req.Items {
		out.Items = append(out.Items, s.calcBatchItem(ctx, itemRaw, i, batchID))
		if out.Items[i].OK {
			out.Succeeded++
		} else {
			out.Failed++
		}
	}
	if out.Failed > 0 {
		// The HTTP call itself succeeded; the summary records partial failure.
		summary.Status = store.StatusError
		summary.Error = partialMsg(out.Succeeded, out.Failed)
	}
	body, _ := json.Marshal(out)
	summary.Result = body
	_ = s.saveSummary(ctx, summary)
	return out
}

func (s *Service) calcBatchItem(ctx context.Context, raw json.RawMessage, index int, batchID string) ItemResult {
	checked, verr := validate.Item(raw)
	if verr != nil {
		s.persistError(ctx, raw, "batch", batchID, index, verr)
		return ItemResult{Index: index, OK: false, Error: verr}
	}
	res, gerr := s.compute(ctx, checked)
	if gerr != nil {
		v := toValidateError(gerr.Code, gerr.Message)
		s.persistError(ctx, raw, "batch", batchID, index, v)
		return ItemResult{Index: index, OK: false, Error: v}
	}
	dto := s.toDTO(checked, res, false)
	rec, perr := s.persistOK(ctx, raw, "batch", batchID, index, checked, res, dto)
	if perr != nil {
		v := toValidateError("persistence_failed", perr.Error())
		return ItemResult{Index: index, OK: false, Error: v}
	}
	return ItemResult{Index: index, OK: true, RecordID: rec.ID, Result: dto}
}

func (s *Service) saveSummary(ctx context.Context, rec store.Record) error {
	if len(rec.Result) == 0 {
		rec.Result = []byte("null")
	}
	_, err := s.store.Save(ctx, rec)
	return err
}

func (s *Service) failSummary(ctx context.Context, rec store.Record, msg string) {
	rec.Status = store.StatusError
	rec.Error = msg
	rec.Result = []byte("null")
	_ = s.saveSummary(ctx, rec)
}

func partialMsg(ok, bad int) string {
	return "batch completed with partial failures: " +
		itoa(ok) + " succeeded, " + itoa(bad) + " failed"
}

func errTooMany(n int) string {
	return "batch exceeds maximum of " + itoa(MaxBatchItems) + " items, got " + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
