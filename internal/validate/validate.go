// Package validate decodes and validates steering calculation requests.
//
// The service's canonical angle unit is degrees; callers may instead submit
// radians via the "angle_unit" field. Validation is deliberately separate from
// the pure geometry package and from persistence.
package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"ackermann/internal/geometry"
)

// FieldError describes one rejected parameter.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is a readable request-level validation failure.
type Error struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func (e *Error) Error() string {
	if len(e.Fields) == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s: %s: %s", e.Message, e.Fields[0].Field, e.Fields[0].Message)
}

func badRequest(code, msg string, fields ...FieldError) *Error {
	return &Error{Code: code, Message: msg, Fields: fields}
}

// RawItem keeps each field as raw JSON so a non-numeric value in one field can
// be reported against that exact field instead of failing the whole decode.
type RawItem struct {
	Wheelbase json.RawMessage `json:"wheelbase_m"`
	Track     json.RawMessage `json:"track_m"`
	Inner     json.RawMessage `json:"inner_angle"`
	AngleUnit json.RawMessage `json:"angle_unit"`
}

// Checked is a validated, degree-based request.
type Checked struct {
	Wheelbase float64
	Track     float64
	InnerDeg  float64
	AngleUnit string // canonical: "deg" or "rad"
}

// Item parses and validates one item.
func Item(raw json.RawMessage) (*Checked, *Error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, badRequest("empty_item", "request item is empty")
	}

	var r RawItem
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, badRequest("invalid_json", "request body is not a valid JSON object with known fields",
			FieldError{Field: "body", Message: err.Error()})
	}
	if dec.More() {
		return nil, badRequest("invalid_json", "request body must contain a single JSON object")
	}

	var fields []FieldError
	wheelbase, ok1 := numericField(&fields, "wheelbase_m", r.Wheelbase, true)
	track, ok2 := numericField(&fields, "track_m", r.Track, true)
	inner, ok3 := numericField(&fields, "inner_angle", r.Inner, false)

	// angle_unit: absent means degrees. A non-string value or an unknown unit
	// is rejected; unit/value contradictions are checked after conversion.
	unit := "deg"
	if len(bytes.TrimSpace(r.AngleUnit)) > 0 {
		var s string
		var num float64
		if err := json.Unmarshal(r.AngleUnit, &s); err == nil {
			u, err := CanonicalUnit(s)
			if err != nil {
				fields = append(fields, FieldError{Field: "angle_unit", Message: err.Error()})
			} else {
				unit = u
			}
		} else if jerr := json.Unmarshal(r.AngleUnit, &num); jerr == nil {
			fields = append(fields, FieldError{
				Field: "angle_unit", Message: "must be a string such as \"deg\" or \"rad\", got a number"})
		} else {
			fields = append(fields, FieldError{
				Field: "angle_unit", Message: "must be a string such as \"deg\" or \"rad\""})
		}
	}

	if len(fields) > 0 || !(ok1 && ok2 && ok3) {
		return nil, badRequest("invalid_parameters", "one or more parameters are invalid", fields...)
	}

	rawInner := inner
	if unit == "rad" {
		// A value tagged "rad" must itself lie in the radian steering range
		// |v| < pi/2; e.g. 30 sent as "rad" converts to ~1719 degrees, which
		// is a unit/value contradiction, not a large steering angle.
		if math.Abs(rawInner) >= math.Pi/2 {
			return nil, badRequest("angle_unit_mismatch",
				"angle_unit is \"rad\" but inner_angle lies outside |v| < pi/2; if the value is in degrees set angle_unit to \"deg\"",
				FieldError{Field: "inner_angle",
					Message: fmt.Sprintf("%.6f rad converts to %.6f degrees", rawInner, rawInner*geometry.Rad2Deg)})
		}
		inner = rawInner * geometry.Rad2Deg
	}

	if math.Abs(inner) >= 90 {
		return nil, badRequest("inner_angle_out_of_range",
			"absolute inner steering angle must be strictly less than 90 degrees",
			FieldError{Field: "inner_angle",
				Message: fmt.Sprintf("got %.6f degrees (raw %.6f in %q)", inner, rawInner, unit)})
	}

	return &Checked{Wheelbase: wheelbase, Track: track, InnerDeg: inner, AngleUnit: unit}, nil
}

// numericField parses one JSON numeric field and records a precise field error
// for missing, non-numeric, non-finite or non-positive values.
func numericField(fields *[]FieldError, name string, raw json.RawMessage, positive bool) (float64, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		*fields = append(*fields, FieldError{Field: name, Message: "missing required field"})
		return 0, false
	}
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		*fields = append(*fields, FieldError{Field: name, Message: "must be a number, got null"})
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(trimmed, &v); err != nil {
		*fields = append(*fields, FieldError{
			Field: name, Message: "must be a JSON number, got " + string(bytes.TrimSpace(raw))})
		return 0, false
	}
	if !finite(v) {
		*fields = append(*fields, FieldError{
			Field: name, Message: fmt.Sprintf("must be a finite number, got %v", v)})
		return 0, false
	}
	if positive && v <= 0 {
		*fields = append(*fields, FieldError{
			Field: name, Message: fmt.Sprintf("must be strictly positive, got %v", v)})
		return 0, false
	}
	return v, true
}

// CanonicalUnit normalizes user supplied unit strings.
func CanonicalUnit(s string) (string, error) {
	switch s {
	case "", "deg", "degree", "degrees":
		return "deg", nil
	case "rad", "radian", "radians":
		return "rad", nil
	default:
		return "", fmt.Errorf("unknown angle unit %q: use \"deg\" or \"rad\"", s)
	}
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
