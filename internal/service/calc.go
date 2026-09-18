package service

import (
	"context"
	"encoding/json"

	"ackermann/internal/geometry"
	"ackermann/internal/store"
	"ackermann/internal/validate"
)

// Calc runs one calculation from raw request JSON, persists request+result and
// returns the envelope. source distinguishes the /calculate and /radii entry
// points; includeWheels attaches the four-wheel concurrency layout.
//
// Persistence errors are returned as a CalcError only after the geometry
// succeeded; they never silently produce an "ok" body that was not stored.
func (s *Service) Calc(ctx context.Context, raw json.RawMessage, source string, includeWheels bool) Response {
	checked, verr := validate.Item(raw)
	if verr != nil {
		s.persistError(ctx, raw, source, "", -1, verr)
		return Response{OK: false, Error: verr}
	}

	res, gerr := s.compute(ctx, checked)
	if gerr != nil {
		verr := toValidateError(gerr.Code, gerr.Message)
		s.persistError(ctx, raw, source, "", -1, verr)
		return Response{OK: false, Error: verr}
	}

	dto := s.toDTO(checked, res, includeWheels)
	rec, perr := s.persistOK(ctx, raw, source, "", -1, checked, res, dto)
	if perr != nil {
		verr := toValidateError("persistence_failed", perr.Error())
		return Response{OK: false, Error: verr}
	}
	return Response{OK: true, RecordID: rec.ID, Result: dto}
}

// compute runs the pure geometry and enforces the configured concurrency
// tolerance: all four wheel radius lines must meet one instant center.
func (s *Service) compute(_ context.Context, c *validate.Checked) (*geometry.Result, *CalcError) {
	res, err := geometry.Compute(geometry.Input{
		Wheelbase: c.Wheelbase,
		Track:     c.Track,
		InnerDeg:  c.InnerDeg,
	})
	if err != nil {
		switch err {
		case geometry.ErrInnerTooSteep:
			return nil, &CalcError{Code: "inner_angle_out_of_range", Message: err.Error()}
		case geometry.ErrInvalidDimension:
			return nil, &CalcError{Code: "invalid_parameters", Message: err.Error()}
		default:
			return nil, &CalcError{Code: "geometry_infeasible", Message: err.Error()}
		}
	}
	if res.MaxResidualMM > s.cfg.ToleranceMM {
		return nil, &CalcError{
			Code:    "concurrency_tolerance_exceeded",
			Message: errConcurrency(res.MaxResidualMM, s.cfg.ToleranceMM),
		}
	}
	return res, nil
}

func errConcurrency(got, limit float64) string {
	return "four wheel radius lines do not concur at one instant center within tolerance: " +
		"residual " + ftoa(got) + " mm exceeds limit " + ftoa(limit) + " mm"
}

func (s *Service) toDTO(c *validate.Checked, r *geometry.Result, includeWheels bool) *ResultDTO {
	dto := &ResultDTO{
		Wheelbase:     c.Wheelbase,
		Track:         c.Track,
		InnerAngle:    r.InnerDeg,
		OuterAngle:    r.OuterDeg,
		BicycleAngle:  r.BikeDeg,
		TurnDirection: directionOf(r.Sign),
		Straight:      r.Sign == geometry.Straight,
	}
	if r.Sign == geometry.Straight {
		return dto
	}

	R := r.Radius
	ic := [2]float64{r.ICRX, r.ICRY}
	dto.TurnRadius = &R
	dto.InstantCenter = &ic
	dto.Radii = &RadiiDTO{
		RearInner:  r.Radii.RearInner,
		RearOuter:  r.Radii.RearOuter,
		FrontInner: r.Radii.FrontInner,
		FrontOuter: r.Radii.FrontOuter,
	}
	dto.ArcRatio = &ArcRatios{Rear: *r.RearArcRatio, Front: *r.FrontArcRatio}
	dto.MaxResidualMM = r.MaxResidualMM
	if includeWheels {
		dto.Wheels = make([]WheelDTO, 0, 4)
		for _, w := range r.Wheels {
			dto.Wheels = append(dto.Wheels, WheelDTO{
				Name:         w.Name,
				X:            w.X,
				Y:            w.Y,
				SteerAngle:   w.AngleDeg,
				SignedRadius: w.SignedR,
				Radius:       w.AbsR,
				ICRResidual:  w.ResidualMM,
			})
		}
	}
	return dto
}

func (s *Service) persistOK(ctx context.Context, raw json.RawMessage, source, batchID string,
	index int, c *validate.Checked, r *geometry.Result, dto *ResultDTO) (store.Record, error) {

	resultBody, _ := json.Marshal(Response{OK: true, Result: dto})
	reqBody := requestJSON(raw)
	rec := store.Record{
		BatchID:   batchID,
		Index:     index,
		Kind:      kindOf(batchID),
		Source:    source,
		Status:    store.StatusOK,
		Wheelbase: c.Wheelbase,
		Track:     c.Track,
		InnerDeg:  c.InnerDeg,
		AngleUnit: c.AngleUnit,
		Direction: directionOf(r.Sign),
		Radius:    nil,
		Request:   reqBody,
		Result:    resultBody,
		CreatedAt: s.now(),
	}
	if r.Sign != geometry.Straight {
		R := r.Radius
		rec.Radius = &R
	}
	saved, err := s.store.Save(ctx, rec)
	if err != nil {
		return store.Record{}, err
	}
	return saved, nil
}

func (s *Service) persistError(ctx context.Context, raw json.RawMessage, source, batchID string,
	index int, verr *validate.Error) {

	body, _ := json.Marshal(Response{OK: false, Error: verr})
	// Best-effort extraction of recognizable numeric fields so the history
	// row keeps context even when the request was rejected.
	var wb, tr, inner float64
	var unit string
	if l, lerr := jsonUnmarshalLoose(raw); lerr == nil {
		wb, tr, inner, unit = l.Wheelbase, l.Track, l.InnerDeg, l.AngleUnit
	}
	rec := store.Record{
		BatchID:   batchID,
		Index:     index,
		Kind:      kindOf(batchID),
		Source:    source,
		Status:    store.StatusError,
		Wheelbase: wb,
		Track:     tr,
		InnerDeg:  inner,
		AngleUnit: unit,
		Direction: "invalid",
		Request:   requestJSON(raw),
		Result:    body,
		Error:     verr.Message,
		CreatedAt: s.now(),
	}
	_, _ = s.store.Save(ctx, rec)
}

func kindOf(batchID string) string {
	if batchID != "" {
		return store.KindBatch
	}
	return store.KindSingle
}

func requestJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	return []byte(raw)
}

// looseInput extracts parseable numeric fields from an invalid request, purely
// so history rows retain as much context as possible.
type looseInput struct {
	Wheelbase float64 `json:"wheelbase_m"`
	Track     float64 `json:"track_m"`
	InnerDeg  float64 `json:"inner_angle"`
	AngleUnit string  `json:"angle_unit"`
}

func jsonUnmarshalLoose(raw json.RawMessage) (*looseInput, error) {
	var l looseInput
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func ftoa(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
