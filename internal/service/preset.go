package service

import (
	"context"

	"ackermann/internal/geometry"
	"ackermann/internal/validate"
)

// PresetParams is the shipped worked example: a sedan-sized vehicle with the
// inner wheel at 30 degrees. The outer angle must come out visibly smaller
// than 30 (about 24-25 degrees for these dimensions).
var PresetParams = validate.Checked{
	Wheelbase: 2.70,
	Track:     1.60,
	InnerDeg:  30.0,
	AngleUnit: "deg",
}

// PresetExample computes (but does not persist) the worked example. It is a
// smoke check callers can hit before integrating.
func (s *Service) PresetExample(_ context.Context) Preset {
	res, err := geometry.Compute(geometry.Input{
		Wheelbase: PresetParams.Wheelbase,
		Track:     PresetParams.Track,
		InnerDeg:  PresetParams.InnerDeg,
	})
	if err != nil {
		// The preset is statically valid; a failure here is a programming error.
		panic("preset example invalid: " + err.Error())
	}
	dto := s.toDTO(&PresetParams, res, true)
	return Preset{
		Name: "sedan, inner wheel 30 degrees",
		Request: presetIn{
			Wheelbase: PresetParams.Wheelbase,
			Track:     PresetParams.Track,
			InnerDeg:  PresetParams.InnerDeg,
			AngleUnit: PresetParams.AngleUnit,
		},
		Response: Response{OK: true, Result: dto},
	}
}
