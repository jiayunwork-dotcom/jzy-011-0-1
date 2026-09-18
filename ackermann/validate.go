package ackermann

import (
	"fmt"
	"math"
)

// Error reasons shared with the HTTP layer. They are stable strings clients
// can switch on.
const (
	reasonMissing     = "missing_field"
	reasonNotNumber   = "not_a_number"
	reasonNotFinite   = "not_finite"
	reasonNonPositive = "non_positive"
	reasonOutOfRange  = "angle_out_of_range"
	reasonUnit        = "unit_mismatch"
	reasonInfeasible  = "infeasible_geometry"
	reasonUnknownUnit = "unknown_unit"
)

// FieldError is a readable, field-anchored validation error.
type FieldError struct {
	Field   string `json:"field"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Field, e.Message, e.Reason)
}

// Validate is exported so the API layer and tests can pre-check an Input.
func Validate(in Input) error { return validateInput(in) }

func validateInput(in Input) error {
	if math.IsNaN(in.Wheelbase) || math.IsInf(in.Wheelbase, 0) {
		return &FieldError{Field: "wheelbase", Reason: reasonNotFinite,
			Message: "wheelbase must be a finite number"}
	}
	if in.Wheelbase <= 0 {
		return &FieldError{Field: "wheelbase", Reason: reasonNonPositive,
			Message: fmt.Sprintf("wheelbase must be > 0, got %v", in.Wheelbase)}
	}
	if math.IsNaN(in.Track) || math.IsInf(in.Track, 0) {
		return &FieldError{Field: "track", Reason: reasonNotFinite,
			Message: "track must be a finite number"}
	}
	if in.Track <= 0 {
		return &FieldError{Field: "track", Reason: reasonNonPositive,
			Message: fmt.Sprintf("track must be > 0, got %v", in.Track)}
	}
	if math.IsNaN(in.InsideAngle) || math.IsInf(in.InsideAngle, 0) {
		return &FieldError{Field: "inside_angle", Reason: reasonNotFinite,
			Message: "inside_angle must be a finite number"}
	}
	if math.Abs(in.InsideAngle) >= 90.0 {
		return &FieldError{Field: "inside_angle", Reason: reasonOutOfRange,
			Message: fmt.Sprintf("inside angle magnitude must be strictly < 90 degrees, got %v", in.InsideAngle)}
	}
	return nil
}
