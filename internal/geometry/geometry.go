// Package geometry implements pure Ackermann low-speed (no side slip) steering
// geometry. All angles at this layer are in degrees.
//
// Coordinate system: origin O is the midpoint of the rear axle, +X points
// forward toward the front axle and +Y points to the driver's left. A positive
// inner angle is a left turn; the instant center then lies on +Y and the
// inner (left) wheels trace the smaller radius R - T/2.
package geometry

import (
	"errors"
	"math"
)

// Deg2Rad converts degrees to radians. Rad2Deg converts radians to degrees.
const (
	Deg2Rad = math.Pi / 180
	Rad2Deg = 180 / math.Pi
)

// Direction sign values. Inner == 0 is the straight (infinite-radius) case.
const (
	LeftTurn  = 1
	RightTurn = -1
	Straight  = 0
)

var (
	// ErrInvalidDimension is returned when the wheelbase or track is not positive.
	ErrInvalidDimension = errors.New("wheelbase and track must be positive")
	// ErrInnerTooSteep is returned when |inner| >= 90 degrees.
	ErrInnerTooSteep = errors.New("absolute inner steering angle must be strictly less than 90 degrees")
	// ErrInfeasible is returned when a derived wheel angle reaches 90 degrees
	// or a wheel's radius collapses to zero (no finite instant center).
	ErrInfeasible = errors.New("geometry infeasible: a derived wheel angle reaches 90 degrees or radius becomes zero")
)

// Input is a validated steering calculation request. Angles are in degrees.
type Input struct {
	Wheelbase float64 // L: distance between front and rear axles
	Track     float64 // T: distance between left and right wheels of an axle
	InnerDeg  float64 // signed inner wheel steering angle, degrees
}

// Result is the full output of one steering geometry calculation.
// In the straight case R/ICR/wheels/arc ratios are nil and RadiusLay is zero.
type Result struct {
	InnerDeg float64
	OuterDeg float64 // signed outer wheel angle; zero in the straight case
	BikeDeg  float64 // equivalent bicycle-model angle, tan(bike) = L/R

	Sign       int     // +1 left turn, -1 right turn, 0 straight
	Radius     float64 // R: rear-axle center to instant center, |.|
	ICRX       float64 // instant center x coordinate (always 0)
	ICRY       float64 // instant center y coordinate (signed)
	RearInnerR float64 // |R - T/2|
	RearOuterR float64 // R + T/2
	Radii      RadiusLay

	Wheels [4]Wheel
	// MaxResidualMM is the largest |ICR residual| among the four wheels.
	MaxResidualMM float64

	// Arc ratios outer/inner. Straight -> nil (center at infinity).
	RearArcRatio  *float64
	FrontArcRatio *float64
}

// Compute evaluates Ackermann geometry for a validated Input.
//
// Relationships used (u = |inner|, v = |outer|, L wheelbase, T track):
//
//	cot(u) - cot(v) = T/L
//	cot(bike)       = (cot(u) + cot(v))/2   (bicycle angle, tan = L/R)
//	R               = L*tan(bike)
//
// Hence cot(u) = R/L - T/(2L), cot(v) = R/L + T/(2L).
func Compute(in Input) (*Result, error) {
	if math.IsNaN(in.Wheelbase) || math.IsInf(in.Wheelbase, 0) ||
		math.IsNaN(in.Track) || math.IsInf(in.Track, 0) ||
		!(in.Wheelbase > 0) || !(in.Track > 0) {
		return nil, ErrInvalidDimension
	}
	if math.IsNaN(in.InnerDeg) || math.IsInf(in.InnerDeg, 0) || abs(in.InnerDeg) >= 90 {
		return nil, ErrInnerTooSteep
	}

	res := &Result{InnerDeg: in.InnerDeg}

	// Straight running: instant center at infinity, no finite radius.
	if in.InnerDeg == 0 {
		return res, nil
	}

	sign := LeftTurn
	if in.InnerDeg < 0 {
		sign = RightTurn
	}
	s := float64(sign)
	u := abs(in.InnerDeg) * Deg2Rad

	// cot(u) = R/L - T/(2L)  =>  R = L*cot(u) + T/2
	R := in.Wheelbase/math.Tan(u) + in.Track/2
	// The inner rear wheel radius is R - T/2 = L*cot(u), always positive for
	// |u| < 90; guard it anyway: an ICR on or past the inner rear wheel means
	// no wheel can trace a non-degenerate radius and the layout is infeasible.
	rearInner := R - in.Track/2
	if !(R > 0) || !(rearInner > 0) || math.IsInf(R, 0) {
		return nil, ErrInfeasible
	}

	// cot(v) = R/L + T/(2L). atan of a positive finite number is strictly
	// inside (0, 90 deg), so no derived outer wheel angle can reach 90 deg;
	// the explicit range guard documents and enforces that contract.
	v := math.Atan(in.Wheelbase / (R + in.Track/2))
	if !(v > 0) || v >= 90*Deg2Rad {
		return nil, ErrInfeasible
	}
	bike := math.Atan(in.Wheelbase / R)
	if bike >= 90*Deg2Rad {
		return nil, ErrInfeasible
	}

	res.Sign = sign
	res.OuterDeg = s * v * Rad2Deg
	res.BikeDeg = s * bike * Rad2Deg
	res.Radius = R
	res.ICRX = 0
	res.ICRY = s * R
	res.Radii = LayRadii(in.Wheelbase, in.Track, R)
	res.RearInnerR = res.Radii.RearInner
	res.RearOuterR = res.Radii.RearOuter

	rearRatio := res.Radii.RearOuter / res.Radii.RearInner
	frontRatio := res.Radii.FrontOuter / res.Radii.FrontInner
	res.RearArcRatio = &rearRatio
	res.FrontArcRatio = &frontRatio

	res.Wheels = Layout(in.Wheelbase, in.Track, sign, in.InnerDeg, v*Rad2Deg, res.Radii, res.ICRY)
	for i := range res.Wheels {
		if r := abs(res.Wheels[i].ResidualMM); r > res.MaxResidualMM {
			res.MaxResidualMM = r
		}
	}

	return res, nil
}

// CotCheck returns cot(|outer|) - cot(|inner|), the Ackermann identity whose
// value must equal T/L (the outer wheel is shallower, so cot(outer) is
// larger). It is a convenience used by tests and diagnostics.
func CotCheck(innerDeg, outerDeg float64) float64 {
	return 1/math.Tan(abs(outerDeg)*Deg2Rad) - 1/math.Tan(abs(innerDeg)*Deg2Rad)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
