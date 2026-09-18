// Package ackermann implements low-speed (no tire slip) Ackermann steering
// geometry. All angles that cross the package boundary are expressed in
// degrees; internally radians are used only for trigonometry.
package ackermann

import "math"

// Input is a single steering-geometry calculation request.
type Input struct {
	Wheelbase   float64 // L: distance between front and rear axles, must be > 0
	Track       float64 // T: distance between left/right wheel centers, must be > 0
	InsideAngle float64 // signed inside front wheel angle in degrees, |a| < 90
}

// Point is a position in the vehicle plane. Origin is the rear axle center,
// X points toward the front axle, Y points toward the vehicle's left side.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WheelResult carries a single wheel's position, signed turning radius and
// steering angle. Radius is the signed distance from the wheel center to the
// instantaneous center of rotation (ICR); its sign flips with the steering
// direction. Both Radius and AngleDeg are nil for straight driving, where the
// ICR is at infinity.
type WheelResult struct {
	Wheel    string   `json:"wheel"`
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	Radius   *float64 `json:"radius"`
	AngleDeg *float64 `json:"angle_deg"`
}

// ArcRatios holds arc length ratios. At zero slip every wheel rotates around
// the same ICR with the same angular velocity, so traveled arc length is
// proportional to the distance from the ICR: arc = radius * angle. All ratio
// fields are nil for straight driving (infinite radii).
type ArcRatios struct {
	// RearOuterToRearInner is the primary ratio the service is asked for,
	// computed from the rear wheels: s_outer/s_inner = (R+T/2)/(R-T/2).
	RearOuterToRearInner   *float64 `json:"rear_outer_to_rear_inner"`
	FrontOuterToFrontInner *float64 `json:"front_outer_to_front_inner"`
	// PerWheel is arc length relative to the rear INSIDE wheel (1 for it).
	PerWheel map[string]float64 `json:"per_wheel_vs_rear_inner"`
}

// Result is the full steering-geometry calculation output. Radius/Center are
// nil for straight driving (inside angle zero, ICR at infinity).
type Result struct {
	Wheelbase float64 `json:"wheelbase"`
	Track     float64 `json:"track"`

	// Direction is "left" for a positive inside angle, "right" for negative.
	Direction string `json:"direction"`
	Straight  bool   `json:"straight"`

	InsideAngleDeg  float64 `json:"inside_angle_deg"`
	OutsideAngleDeg float64 `json:"outside_angle_deg"`

	// BicycleAngleDeg is the equivalent bicycle-model steering angle,
	// tan(bicycle) = wheelbase / R (signed); zero for straight driving.
	BicycleAngleDeg float64 `json:"bicycle_angle_deg"`

	// R is the signed turning radius: rear axle center to the ICR. Nil when
	// the ICR is at infinity (straight driving) — never a fake finite value.
	Radius *float64 `json:"radius"`

	// Center is the ICR coordinate with the rear axle center as origin. Nil
	// for straight driving.
	Center *Point `json:"center"`

	// Wheels are the four wheel radii/angles. Front axle x=wheelbase, rear x=0.
	Wheels map[string]WheelResult `json:"wheels"`

	RearInsideRadius   *float64 `json:"rear_inside_radius"`
	RearOutsideRadius  *float64 `json:"rear_outside_radius"`
	FrontInsideRadius  *float64 `json:"front_inside_radius"`
	FrontOutsideRadius *float64 `json:"front_outside_radius"`

	Arc ArcRatios `json:"arc_ratios"`

	// Geometry verification: do the four wheel heading directions all agree
	// on ONE common ICR? Checked geometrically via wheel-center + heading.
	GeometryOK   bool    `json:"geometry_ok"`
	MaxDeviation float64 `json:"geometry_max_deviation"`
	AbsTolerance float64 `json:"geometry_abs_tolerance"`
	RelTolerance float64 `json:"geometry_rel_tolerance"`
}

// Tol controls geometric verification tolerances. A deviation is acceptable
// when |d| <= abs + rel*scale, where scale is the characteristic size
// (wheelbase + |R|).
type Tol struct {
	Abs float64
	Rel float64
}

// DefaultTol is used unless the caller overrides it.
var DefaultTol = Tol{Abs: 1e-9, Rel: 1e-9}

const deg = 180.0 / math.Pi

func fp(v float64) *float64 { return &v }
