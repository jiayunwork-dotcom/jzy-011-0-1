// Package api exposes the steering-geometry service over HTTP.
package api

import (
	"encoding/json"
	"fmt"
	"math"

	"ackermann-service/ackermann"
)

// rawSingle documents the accepted geometry fields; actual decoding is done
// against a raw message map so missing fields are distinguishable.
type parsedInput struct {
	Input         ackermann.Input
	AngleUnit     string // canonical: "deg" or "rad"
	Wheelbase     float64
	Track         float64
	InsideAngle   float64 // converted to degrees
	insidePresent bool
}

// apiError is the readable error envelope returned for every 4xx/5xx.
type apiError struct {
	Status int `json:"-"`
	Error  struct {
		Code    string     `json:"code"`
		Message string     `json:"message"`
		Details *errDetail `json:"details,omitempty"`
	} `json:"error"`
}

type errDetail struct {
	Field  string `json:"field,omitempty"`
	Index  *int   `json:"item_index,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func fieldErr(statusCode int, code, message, field, reason string, idx *int) *apiError {
	var e apiError
	e.Status = statusCode
	e.Error.Code = code
	e.Error.Message = message
	if field != "" || reason != "" || idx != nil {
		e.Error.Details = &errDetail{Field: field, Reason: reason, Index: idx}
	}
	return &e
}

// decodeNumber extracts one finite float64 from a raw JSON value.
func decodeNumber(raw json.RawMessage, field string, idx *int) (float64, *apiError) {
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fieldErr(400, "invalid_number",
			fmt.Sprintf("item %d: %s must be a JSON number", oneBased(idx), field),
			field, "not_a_number", idx)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fieldErr(400, "non_finite",
			fmt.Sprintf("item %d: %s must be a finite number", oneBased(idx), field),
			field, "not_finite", idx)
	}
	return v, nil
}

func decodeUnit(raw *json.RawMessage, idx *int) (string, *apiError) {
	if raw == nil {
		return "deg", nil
	}
	var u string
	if err := json.Unmarshal(*raw, &u); err != nil {
		return "", fieldErr(400, "invalid_unit",
			fmt.Sprintf("item %d: angle_unit must be a string", oneBased(idx)),
			"angle_unit", "invalid", idx)
	}
	canonical, ok := ackermann.NormalizeUnit(u)
	if !ok {
		return "", fieldErr(400, "unknown_angle_unit",
			fmt.Sprintf("item %d: unknown angle_unit %q (supported: deg, rad)", oneBased(idx), u),
			"angle_unit", "unknown_unit", idx)
	}
	return canonical, nil
}

// parseSingle turns a raw request body map into a validated parsedInput.
// body is the decoded object; keys presence is checked for missing fields.
func parseSingle(fields map[string]json.RawMessage, idx *int) (parsedInput, *apiError) {
	var p parsedInput

	unitRaw := optionalRaw(fields, "angle_unit")
	unit, errp := decodeUnit(unitRaw, idx)
	if errp != nil {
		return p, errp
	}
	p.AngleUnit = unit

	for _, f := range []string{"wheelbase", "track", "inside_angle"} {
		if _, ok := fields[f]; !ok {
			return p, fieldErr(400, "missing_field",
				fmt.Sprintf("item %d: missing required field %q", oneBased(idx), f),
				f, "missing_field", idx)
		}
	}

	if v, e := decodeNumber(fields["wheelbase"], "wheelbase", idx); e != nil {
		return p, e
	} else {
		p.Wheelbase = v
	}
	if v, e := decodeNumber(fields["track"], "track", idx); e != nil {
		return p, e
	} else {
		p.Track = v
	}
	if v, e := decodeNumber(fields["inside_angle"], "inside_angle", idx); e != nil {
		return p, e
	} else {
		p.insidePresent = true
		// Unit consistency check happens before conversion: a value like
		// 400 cannot be degrees of steering; reject rather than wrap.
		if ackermann.UnitContradiction(v, unit) {
			return p, fieldErr(400, "angle_unit_contradiction",
				fmt.Sprintf("item %d: inside_angle %v contradicts angle_unit %q (magnitude impossible for a steering angle in that unit)",
					oneBased(idx), v, unit),
				"inside_angle", "unit_mismatch", idx)
		}
		p.InsideAngle = ackermann.ToDegrees(v, unit)
	}

	p.Input = ackermann.Input{
		Wheelbase:   p.Wheelbase,
		Track:       p.Track,
		InsideAngle: p.InsideAngle,
	}
	if err := ackermann.Validate(p.Input); err != nil {
		return p, mapGeomError(err, idx)
	}
	return p, nil
}

func optionalRaw(fields map[string]json.RawMessage, key string) *json.RawMessage {
	if r, ok := fields[key]; ok {
		return &r
	}
	return nil
}

func mapGeomError(err error, idx *int) *apiError {
	fe, ok := err.(*ackermann.FieldError)
	if !ok {
		return fieldErr(400, "invalid_geometry", err.Error(), "", "", idx)
	}
	return fieldErr(400, reasonToCode(fe.Reason),
		fmt.Sprintf("item %d: %s", oneBased(idx), fe.Message),
		fe.Field, fe.Reason, idx)
}

func reasonToCode(r string) string {
	switch r {
	case "non_positive":
		return "non_positive_dimension"
	case "angle_out_of_range":
		return "angle_out_of_range"
	case "not_finite":
		return "non_finite"
	case "infeasible_geometry":
		return "infeasible_geometry"
	default:
		return "invalid_input"
	}
}

func oneBased(idx *int) interface{} {
	if idx == nil {
		return 1
	}
	return *idx + 1
}
