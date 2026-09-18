package ackermann

import "math"

// verifyGeometry checks the actual geometric constraint rather than just the
// two front angles: at zero slip every wheel's velocity is perpendicular to
// the radius joining the wheel center to the single common ICR. Equivalently,
// the line through each wheel center along its heading NORMAL must pass
// through one common point.
//
// We intersect the normals of the two FRONT wheels (which carry distinct
// steering angles) to obtain the candidate ICR, then verify that this point
// (a) coincides with the analytic center (0, R) and (b) lies on the heading
// normals of all four wheels within tolerance.
func verifyGeometry(res *Result, tol Tol) {
	if res.Straight {
		res.MaxDeviation = 0
		res.GeometryOK = true
		return
	}

	R := *res.Radius
	s := 1.0
	if R < 0 {
		s = -1
	}
	L, half := res.Wheelbase, res.Track/2.0
	di := *res.Wheels["front_inside"].AngleDeg / deg
	do := *res.Wheels["front_outside"].AngleDeg / deg

	// Heading-normal directions (rotate heading by +90 degrees).
	nfi := vec{-math.Sin(di), math.Cos(di)}
	nfo := vec{-math.Sin(do), math.Cos(do)}
	nRear := vec{0, 1} // rear wheels head along +X, normal is the axle line

	cfi := vec{L, s * half}
	cfo := vec{L, -s * half}

	candidate, ok := intersectLines(cfi, nfi, cfo, nfo)
	if !ok {
		res.GeometryOK = false
		res.MaxDeviation = math.Inf(1)
		return
	}

	type wheelLine struct {
		center vec
		normal vec
	}
	lines := []wheelLine{
		{cfi, nfi},
		{cfo, nfo},
		{vec{0, s * half}, nRear},  // rear inside
		{vec{0, -s * half}, nRear}, // rear outside: same normal line (rear axle)
	}

	maxDev := 0.0
	for _, ln := range lines {
		// Distance from the candidate ICR to the wheel's heading-normal line.
		d := math.Abs(cross(ln.normal, sub(candidate, ln.center)))
		if d > maxDev {
			maxDev = d
		}
	}
	// Candidate must coincide with the analytic center (0, R).
	if d := math.Abs(candidate.x); d > maxDev {
		maxDev = d
	}
	if d := math.Abs(candidate.y - R); d > maxDev {
		maxDev = d
	}

	scale := L + math.Abs(R)
	limit := tol.Abs + tol.Rel*scale
	res.MaxDeviation = maxDev
	res.GeometryOK = maxDev <= limit
}

type vec struct{ x, y float64 }

func cross(a, b vec) float64 { return a.x*b.y - a.y*b.x }
func sub(a, b vec) vec       { return vec{a.x - b.x, a.y - b.y} }

// intersectLines returns the intersection of p1+t*d1 and p2+u*d2.
func intersectLines(p1, d1, p2, d2 vec) (vec, bool) {
	den := cross(d1, d2)
	if math.Abs(den) < 1e-15 {
		return vec{}, false
	}
	t := cross(sub(p2, p1), d2) / den
	return vec{p1.x + t*d1.x, p1.y + t*d1.y}, true
}
