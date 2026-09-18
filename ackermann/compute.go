package ackermann

import "math"

// Compute performs the full Ackermann geometry calculation for one input.
//
// Geometry convention (low speed, no tire slip):
//
//   - R is the signed distance from the REAR AXLE CENTER to the ICR, which
//     lies on the rear axle line. A positive inside angle steers LEFT and the
//     ICR is at (0, +R).
//   - Equivalent bicycle model: tan(delta) = L / R.
//   - The inside front wheel turns more than the outside one:
//     cot(do) - cot(di) = T / L.
//   - With r_i = L/tan(|di|) the distance from the rear INSIDE wheel to the
//     ICR: R = r_i + T/2, rear outside radius = r_i + T,
//     front inside radius  = hypot(L, r_i),
//     front outside radius = hypot(L, r_i + T).
//
// do is solved from cot(do) = cot(di) + T/L and is strictly smaller in
// magnitude than di for any T > 0 and di != 0.
func Compute(in Input, tol Tol) (*Result, error) {
	if err := validateInput(in); err != nil {
		return nil, err
	}
	L, T := in.Wheelbase, in.Track

	res := &Result{
		Wheelbase:      L,
		Track:          T,
		InsideAngleDeg: in.InsideAngle,
		Wheels:         map[string]WheelResult{},
		AbsTolerance:   tol.Abs,
		RelTolerance:   tol.Rel,
	}

	// Zero inside angle: straight driving, ICR at infinity. No finite radius
	// is fabricated; all radius pointers stay nil and angles are exactly zero.
	if in.InsideAngle == 0 {
		res.Straight = true
		res.Direction = "straight"
		fillStraightWheels(res, L, T)
		res.Arc = ArcRatios{PerWheel: straightArcRatios()}
		res.GeometryOK = true // parallel headings share a point at infinity
		return res, nil
	}

	s := 1.0
	if in.InsideAngle < 0 {
		s = -1
	}
	res.Direction = "left"
	if s < 0 {
		res.Direction = "right"
	}

	diAbs := math.Abs(in.InsideAngle)
	diRad := diAbs / deg

	// r_i: rear inside wheel to ICR (along the rear axle, unsigned).
	ri := L / math.Tan(diRad)
	// R: rear axle center to ICR.
	RAbs := ri + T/2.0
	R := s * RAbs

	// Outside angle from the Ackermann cotangent condition.
	doRad := math.Atan(1.0 / (1.0/math.Tan(diRad) + T/L))
	// Equivalent bicycle-model angle.
	deltaRad := math.Atan(L / RAbs)

	di := s * diAbs
	do := s * doRad * deg
	delta := s * deltaRad * deg

	res.OutsideAngleDeg = do
	res.BicycleAngleDeg = delta
	res.Radius = fp(R)
	res.Center = &Point{X: 0, Y: R}

	half := T / 2.0

	// Signed wheel radii. Rear wheels sit on the rear axle (x = 0) and do not
	// steer; their radius is the signed distance along the axle to the ICR.
	rearInside := s * ri
	rearOutside := s * (ri + T)

	// Front wheel radii are distances from (L, +/- T/2) to (0, R).
	frontInside := s * math.Hypot(L, ri)
	frontOutside := s * math.Hypot(L, ri+T)

	res.RearInsideRadius = fp(rearInside)
	res.RearOutsideRadius = fp(rearOutside)
	res.FrontInsideRadius = fp(frontInside)
	res.FrontOutsideRadius = fp(frontOutside)

	addWheel(res, "rear_inside", 0, s*half, rearInside, 0)
	addWheel(res, "rear_outside", 0, -s*half, rearOutside, 0)
	addWheel(res, "front_inside", L, s*half, frontInside, di)
	addWheel(res, "front_outside", L, -s*half, frontOutside, do)

	// Arc length ratios equal the radius ratios: all wheels share one ICR and
	// one angular velocity omega, so arc length s = radius * omega * t.
	res.Arc = ArcRatios{
		RearOuterToRearInner:   fp((ri + T) / ri),
		FrontOuterToFrontInner: fp(math.Hypot(L, ri+T) / math.Hypot(L, ri)),
		PerWheel: map[string]float64{
			"rear_inside":   1,
			"rear_outside":  (ri + T) / ri,
			"front_inside":  math.Hypot(L, ri) / ri,
			"front_outside": math.Hypot(L, ri+T) / ri,
		},
	}

	verifyGeometry(res, tol)

	// Any wheel whose steering angle reaches 90 degrees is geometrically
	// infeasible. Front angles are strictly below the inside input (already
	// validated < 90), but enforce it explicitly so the guarantee cannot
	// silently regress.
	for name, w := range res.Wheels {
		if w.AngleDeg != nil && math.Abs(*w.AngleDeg) >= 90.0 {
			return nil, &FieldError{
				Field:   "inside_angle",
				Reason:  reasonInfeasible,
				Message: name + " steering angle magnitude reaches 90 degrees; geometry infeasible",
			}
		}
	}

	return res, nil
}

func addWheel(res *Result, name string, x, y, radius, angleDeg float64) {
	res.Wheels[name] = WheelResult{
		Wheel:    name,
		X:        x,
		Y:        y,
		Radius:   fp(radius),
		AngleDeg: fp(angleDeg),
	}
}

func fillStraightWheels(res *Result, L, T float64) {
	half := T / 2.0
	addStraightWheel(res, "rear_inside", 0, half)
	addStraightWheel(res, "rear_outside", 0, -half)
	addStraightWheel(res, "front_inside", L, half)
	addStraightWheel(res, "front_outside", L, -half)
}

func addStraightWheel(res *Result, name string, x, y float64) {
	res.Wheels[name] = WheelResult{
		Wheel:  name,
		X:      x,
		Y:      y,
		Radius: nil, // infinity
	}
}

func straightArcRatios() map[string]float64 {
	return map[string]float64{
		"rear_inside":   1,
		"rear_outside":  1,
		"front_inside":  1,
		"front_outside": 1,
	}
}
