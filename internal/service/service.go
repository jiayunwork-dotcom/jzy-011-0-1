// Package service orchestrates input validation, Ackermann geometry and
// persistence. Geometry lives in internal/geometry, request validation in
// internal/validate and storage in internal/store; this package only wires
// them together and defines the on-the-wire DTOs.
package service

import (
	"encoding/json"
	"time"

	"ackermann/internal/geometry"
	"ackermann/internal/store"
	"ackermann/internal/validate"
)

// Config are the geometry tolerances and default units echoed back by the
// configuration endpoint.
type Config struct {
	AngleUnit       string  `json:"angle_unit"`        // canonical input default
	OutputAngleUnit string  `json:"output_angle_unit"` // angles are always returned in degrees
	ToleranceMM     float64 `json:"icr_tolerance_mm"`  // accepted four-line concurrency residual
	ToleranceDeg    float64 `json:"numeric_eps_deg"`   // angle comparison epsilon
}

// ArcRatios are outer/inner arc-length ratios, equal to the radius ratios.
type ArcRatios struct {
	Rear  float64 `json:"rear_outer_over_inner"`
	Front float64 `json:"front_outer_over_inner"`
}

// WheelDTO is one wheel in the four-wheel concurrency layout.
type WheelDTO struct {
	Name         string  `json:"name"`
	X            float64 `json:"x_m"`
	Y            float64 `json:"y_m"`
	SteerAngle   float64 `json:"steer_angle_deg"`
	SignedRadius float64 `json:"signed_radius_m"`
	Radius       float64 `json:"radius_m"`
	ICRResidual  float64 `json:"icr_residual_mm"`
}

// RadiiDTO describes the four wheel radii and the instant center coordinates.
type RadiiDTO struct {
	RearInner  float64 `json:"rear_inner_m"`
	RearOuter  float64 `json:"rear_outer_m"`
	FrontInner float64 `json:"front_inner_m"`
	FrontOuter float64 `json:"front_outer_m"`
}

// ResultDTO is the full calculation result returned to callers.
type ResultDTO struct {
	Wheelbase     float64     `json:"wheelbase_m"`
	Track         float64     `json:"track_m"`
	InnerAngle    float64     `json:"inner_angle_deg"`
	OuterAngle    float64     `json:"outer_angle_deg"`
	BicycleAngle  float64     `json:"bicycle_angle_deg"`
	TurnDirection string      `json:"turn_direction"` // left | right | straight
	TurnRadius    *float64    `json:"turn_radius_m"`  // R, nil when straight
	InstantCenter *[2]float64 `json:"instant_center_xy_m"`
	Radii         *RadiiDTO   `json:"radii"`
	Wheels        []WheelDTO  `json:"wheels,omitempty"`
	ArcRatio      *ArcRatios  `json:"arc_length_ratio_outer_over_inner"`
	MaxResidualMM float64     `json:"max_icr_residual_mm"`
	Straight      bool        `json:"straight"`
}

// Response wraps a single calculation.
type Response struct {
	OK       bool            `json:"ok"`
	RecordID string          `json:"record_id,omitempty"`
	Result   *ResultDTO      `json:"result,omitempty"`
	Error    *validate.Error `json:"error,omitempty"`
}

// ItemResult is one element of a batch response.
type ItemResult struct {
	Index    int             `json:"index"`
	OK       bool            `json:"ok"`
	RecordID string          `json:"record_id,omitempty"`
	Result   *ResultDTO      `json:"result,omitempty"`
	Error    *validate.Error `json:"error,omitempty"`
}

// BatchResponse is the result of a batch submission.
type BatchResponse struct {
	OK        bool         `json:"ok"`
	BatchID   string       `json:"batch_id"`
	Total     int          `json:"total"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Items     []ItemResult `json:"items"`
}

// RecordDTO is one history entry.
type RecordDTO struct {
	ID        string          `json:"id"`
	BatchID   string          `json:"batch_id,omitempty"`
	Index     int             `json:"index"`
	Kind      string          `json:"kind"`
	Source    string          `json:"source"`
	Status    string          `json:"status"`
	Request   json.RawMessage `json:"request"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Direction string          `json:"direction"`
	Radius    *float64        `json:"turn_radius_m,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// HistoryResponse is returned by the history endpoint.
type HistoryResponse struct {
	Total int         `json:"total"`
	Count int         `json:"count"`
	Items []RecordDTO `json:"items"`
}

// Preset is the shipped worked example (sedan-sized vehicle, inner 30 deg).
type Preset struct {
	Name     string   `json:"name"`
	Request  presetIn `json:"request"`
	Response Response `json:"response"`
}

type presetIn struct {
	Wheelbase float64 `json:"wheelbase_m"`
	Track     float64 `json:"track_m"`
	InnerDeg  float64 `json:"inner_angle"`
	AngleUnit string  `json:"angle_unit"`
}

// Service contains the business logic.
type Service struct {
	store store.Store
	cfg   Config
	now   func() time.Time
}

// New constructs a Service.
func New(s store.Store, cfg Config) *Service {
	if cfg.AngleUnit == "" {
		cfg.AngleUnit = "deg"
	}
	if cfg.OutputAngleUnit == "" {
		cfg.OutputAngleUnit = "deg"
	}
	if cfg.ToleranceMM <= 0 {
		cfg.ToleranceMM = 1e-6
	}
	if cfg.ToleranceDeg <= 0 {
		cfg.ToleranceDeg = 1e-9
	}
	return &Service{store: s, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

// Config returns the active configuration.
func (s *Service) Config() Config { return s.cfg }

// directionOf maps a geometry sign to a string.
func directionOf(sign int) string {
	switch sign {
	case geometry.LeftTurn:
		return "left"
	case geometry.RightTurn:
		return "right"
	default:
		return "straight"
	}
}

// CalcError carries both a machine code and a readable message for failures
// that happen after validation (infeasible geometry / concurrency tolerance).
type CalcError struct {
	Code    string
	Message string
}

func (e *CalcError) Error() string { return e.Message }

func toValidateError(code, msg string) *validate.Error {
	return &validate.Error{Code: code, Message: msg}
}
