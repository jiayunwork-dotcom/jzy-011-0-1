package ackermann

import "math"

// SupportedUnits lists accepted angle unit identifiers.
var SupportedUnits = []string{"deg", "degree", "degrees", "rad", "radian", "radians"}

// NormalizeUnit maps an angle unit identifier to "deg" or "rad". Empty means
// default (deg). An ok=false result means the unit is not recognized.
func NormalizeUnit(u string) (canonical string, ok bool) {
	switch u {
	case "":
		return "deg", true
	case "deg", "degree", "degrees":
		return "deg", true
	case "rad", "radian", "radians":
		return "rad", true
	default:
		return "", false
	}
}

// ToDegrees converts an angle expressed in canonicalUnit to degrees.
func ToDegrees(v float64, canonicalUnit string) float64 {
	if canonicalUnit == "rad" {
		return v * deg
	}
	return v
}

// PresetInput is the ready-to-call example: passenger-car dimensions with a
// 30 degree inside lock. For L=2.7, T=1.55 the outside angle is roughly
// 23.4 degrees — clearly below 30.
var PresetInput = Input{Wheelbase: 2.7, Track: 1.55, InsideAngle: 30}

// UnitContradiction flags values that are inconsistent with their declared
// angle unit. A value >= 9 in radians (~516 degrees) or >= 360 in degrees
// cannot be a steering angle; callers receive a readable error instead of a
// silently wrong result.
func UnitContradiction(rawValue float64, canonicalUnit string) bool {
	if math.IsNaN(rawValue) || math.IsInf(rawValue, 0) {
		return false
	}
	switch canonicalUnit {
	case "rad":
		return math.Abs(rawValue) >= 9.0
	default:
		return math.Abs(rawValue) >= 360.0
	}
}
