package geometry

import "math"

// RadiusLay describes the four wheel radii about the instant center.
//
// Signed values follow the turn sign convention: wheels on the ICR side (the
// inner side) carry a positive signed radius, outer wheels a negative one.
// Negating the inner angle negates every signed radius and the ICR's Y
// coordinate while all absolute lengths stay unchanged.
type RadiusLay struct {
	RearInner  float64
	RearOuter  float64
	FrontInner float64
	FrontOuter float64
}

// Wheel is one contact point used for the geometric concurrency check.
type Wheel struct {
	Name       string  `json:"name"`
	X          float64 `json:"x_m"`
	Y          float64 `json:"y_m"`
	AngleDeg   float64 `json:"steer_angle_deg"` // signed, relative to +X
	SignedR    float64 `json:"signed_radius_m"`
	AbsR       float64 `json:"radius_m"`
	ResidualMM float64 `json:"icr_residual_mm"` // signed perpendicular gap
	// between the wheel's own radius line and the common instant center, mm.
}

// LayRadii computes the four wheel path radii for a finite turn radius R
// measured from the rear-axle midpoint:
//
//	rear inner = R - T/2, rear outer = R + T/2
//	front inner/outer are the hypotenuses from (±T/2, L) to the ICR.
func LayRadii(wheelbase, track, R float64) RadiusLay {
	half := track / 2
	return RadiusLay{
		RearInner:  R - half,
		RearOuter:  R + half,
		FrontInner: math.Hypot(R-half, wheelbase),
		FrontOuter: math.Hypot(R+half, wheelbase),
	}
}

// Layout builds the four contact-point descriptors, each with its position,
// signed steering angle and signed/absolute radius, and measures how far the
// common instant center (0, icrY) lies from the wheel's own heading-normal.
//
// sign is +1 for a left turn (inner wheels at +Y) and -1 for a right turn.
// innerDeg is the signed inner angle; outerAbsDeg is the positive outer angle
// magnitude.
func Layout(wheelbase, track float64, sign int, innerDeg, outerAbsDeg float64, lay RadiusLay, icrY float64) [4]Wheel {
	s := float64(sign)
	half := track / 2
	outerDeg := s * outerAbsDeg

	wheels := [4]Wheel{
		{Name: "rear_inner", X: 0, Y: s * half, AngleDeg: 0, SignedR: s * lay.RearInner, AbsR: lay.RearInner},
		{Name: "rear_outer", X: 0, Y: -s * half, AngleDeg: 0, SignedR: -s * lay.RearOuter, AbsR: lay.RearOuter},
		{Name: "front_inner", X: wheelbase, Y: s * half, AngleDeg: innerDeg, SignedR: s * lay.FrontInner, AbsR: lay.FrontInner},
		{Name: "front_outer", X: wheelbase, Y: -s * half, AngleDeg: outerDeg, SignedR: -s * lay.FrontOuter, AbsR: lay.FrontOuter},
	}
	for i := range wheels {
		wheels[i].ResidualMM = ICRResidual(wheels[i], 0, icrY) * 1000
	}
	return wheels
}

// ICRResidual is the signed perpendicular distance in meters between the
// instant center and the line through a wheel's contact point normal to its
// travel direction — i.e. the wheel's own radius line.
//
// At low speed without side slip each wheel velocity is perpendicular to its
// radius, so all four such lines must concur at a single instant center. The
// residual is a geometric concurrency check, not a comparison of two angle
// numbers: zero (up to floating point) means this wheel's radius line passes
// through the common ICR.
func ICRResidual(w Wheel, icx, icy float64) float64 {
	// Heading unit vector h = (cos a, sin a). The radius line through the
	// wheel is perpendicular to h, so the perpendicular offset of the ICR is
	// (ICR - wheel) projected onto h.
	a := w.AngleDeg * Deg2Rad
	hx, hy := math.Cos(a), math.Sin(a)
	return (icx-w.X)*hx + (icy-w.Y)*hy
}
