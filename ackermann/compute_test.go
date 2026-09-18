package ackermann

import (
	"math"
	"testing"
)

const eps = 1e-9

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s: got %.12f, want %.12f (tol %g)", name, got, want, tol)
	}
}

// 1. cot(outside) - cot(inside) = track/wheelbase.
func TestCotDifferenceEqualsTrackOverWheelbase(t *testing.T) {
	cases := []Input{
		{Wheelbase: 2.7, Track: 1.55, InsideAngle: 30},
		{Wheelbase: 3.0, Track: 1.6, InsideAngle: 10},
		{Wheelbase: 2.5, Track: 1.4, InsideAngle: 45},
		{Wheelbase: 4.0, Track: 2.0, InsideAngle: 89},
	}
	for _, in := range cases {
		res, err := Compute(in, DefaultTol)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cot := math.Abs(1/math.Tan(res.OutsideAngleDeg/deg) -
			1/math.Tan(res.InsideAngleDeg/deg))
		want := in.Track / in.Wheelbase
		scale := 1.0 + want
		approx(t, "cot diff", cot, want, 1e-10*scale)
	}
}

// 2. Zero inside angle => zero outside angle, ICR at infinity (nil radii),
// never a fabricated finite radius.
func TestZeroInsideMeansStraight(t *testing.T) {
	res, err := Compute(Input{Wheelbase: 2.7, Track: 1.55, InsideAngle: 0}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Straight || res.Direction != "straight" {
		t.Fatalf("expected straight driving, got straight=%v dir=%s", res.Straight, res.Direction)
	}
	if res.OutsideAngleDeg != 0 || res.BicycleAngleDeg != 0 {
		t.Fatalf("outside/bicycle angles must be 0, got %f %f", res.OutsideAngleDeg, res.BicycleAngleDeg)
	}
	if res.Radius != nil || res.Center != nil {
		t.Fatalf("radius and center must be nil (infinity), got %v %v", res.Radius, res.Center)
	}
	if res.RearInsideRadius != nil || res.RearOutsideRadius != nil ||
		res.FrontInsideRadius != nil || res.FrontOutsideRadius != nil {
		t.Fatal("all wheel radii must be nil for straight driving")
	}
	for name, w := range res.Wheels {
		if w.Radius != nil {
			t.Fatalf("wheel %s radius must be nil", name)
		}
	}
	if res.Arc.RearOuterToRearInner != nil {
		t.Fatal("arc ratio must be nil for straight driving")
	}
}

// 3. For a nonzero angle the inside angle magnitude is strictly greater than
// the outside angle magnitude.
func TestInsideGreaterThanOutside(t *testing.T) {
	for _, di := range []float64{0.001, 1, 15, 30, 45, 60, 89.999} {
		res, err := Compute(Input{2.7, 1.55, di}, DefaultTol)
		if err != nil {
			t.Fatal(err)
		}
		if !(math.Abs(res.InsideAngleDeg) > math.Abs(res.OutsideAngleDeg)) {
			t.Errorf("di=%v: inside %v must be strictly greater than outside %v",
				di, res.InsideAngleDeg, res.OutsideAngleDeg)
		}
	}
}

// 4. Doubling only the wheelbase (same inside angle) roughly doubles the
// turning radius and moves the outside angle closer to the inside angle.
// R = L/tan(di) + T/2: doubling L doubles the dominant L/tan term exactly
// (checked via the rear-inside radius), and for a small lock R itself is
// approximately doubled since L/tan(di) then dwarfs the constant T/2.
func TestWheelbaseDoubling(t *testing.T) {
	in := Input{Wheelbase: 2.7, Track: 1.55, InsideAngle: 2}
	base, err := Compute(in, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	dbl, err := Compute(Input{2 * in.Wheelbase, in.Track, 2}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	ratio := *dbl.Radius / *base.Radius
	if math.Abs(ratio-2) > 0.015 {
		t.Errorf("R ratio %.4f should be ~2 after doubling wheelbase at small lock", ratio)
	}
	// The rear INSIDE radius r_i = L/tan(di) must double exactly for ANY angle.
	base30, _ := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	dbl30, _ := Compute(Input{5.4, 1.55, 30}, DefaultTol)
	approx(t, "rear inside doubles (30deg)", *dbl30.RearInsideRadius, 2**base30.RearInsideRadius, 1e-9)

	// Outside angle moves closer to the inside angle.
	gapBase := math.Abs(base30.InsideAngleDeg - base30.OutsideAngleDeg)
	gapDbl := math.Abs(dbl30.InsideAngleDeg - dbl30.OutsideAngleDeg)
	if gapDbl >= gapBase {
		t.Errorf("outside angle must move closer to inside: gaps %v vs %v", gapDbl, gapBase)
	}
}

// 5. Doubling only the track makes the inside/outside angle difference bigger.
func TestTrackDoublingWidensAngleGap(t *testing.T) {
	base, _ := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	dbl, _ := Compute(Input{2.7, 3.1, 30}, DefaultTol)
	gapBase := math.Abs(base.InsideAngleDeg - base.OutsideAngleDeg)
	gapDbl := math.Abs(dbl.InsideAngleDeg - dbl.OutsideAngleDeg)
	if !(gapDbl > gapBase) {
		t.Errorf("gap must grow with track: %v vs %v", gapDbl, gapBase)
	}
}

// 6. Left vs right lock differ only in sign: equal absolute values for every
// angle and signed radius, center mirrored in y.
func TestLeftRightOnlyDifferInSign(t *testing.T) {
	left, err := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Compute(Input{2.7, 1.55, -30}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	if left.Direction != "left" || right.Direction != "right" {
		t.Fatalf("directions %s/%s", left.Direction, right.Direction)
	}
	approx(t, "R abs", math.Abs(*right.Radius), math.Abs(*left.Radius), 0)
	approx(t, "outside abs", math.Abs(right.OutsideAngleDeg), math.Abs(left.OutsideAngleDeg), 0)
	approx(t, "bicycle abs", math.Abs(right.BicycleAngleDeg), math.Abs(left.BicycleAngleDeg), 0)
	if *right.Radius != -*left.Radius {
		t.Errorf("signed radii must be exact opposites: %v vs %v", *right.Radius, *left.Radius)
	}
	if right.Center.X != left.Center.X || right.Center.Y != -left.Center.Y {
		t.Errorf("centers must mirror in y: %+v vs %+v", right.Center, left.Center)
	}
	for name, wl := range left.Wheels {
		wr := right.Wheels[name]
		if *wr.Radius != -*wl.Radius {
			t.Errorf("wheel %s signed radii not opposite: %v vs %v", name, *wr.Radius, *wl.Radius)
		}
	}
}

// 7. The four wheel radii meet at ONE point — checked geometrically:
// for every wheel, (ICR - wheel center) is perpendicular to the wheel heading,
// and its length equals the reported radius.
func TestFourRadiiMeetAtOnePoint(t *testing.T) {
	for _, in := range []Input{
		{2.7, 1.55, 30},
		{3.0, 1.6, -42},
		{2.5, 1.4, 5},
	} {
		res, err := Compute(in, DefaultTol)
		if err != nil {
			t.Fatal(err)
		}
		if !res.GeometryOK {
			t.Fatalf("engine geometry check failed: deviation %.3e", res.MaxDeviation)
		}
		c := *res.Center
		scale := in.Wheelbase + math.Abs(*res.Radius)
		for name, w := range res.Wheels {
			// Vector from wheel center to ICR.
			vx, vy := c.X-w.X, c.Y-w.Y
			dist := math.Hypot(vx, vy)
			if math.Abs(dist-math.Abs(*w.Radius)) > 1e-9*scale {
				t.Errorf("%s: distance to ICR %.10f != reported |radius| %.10f",
					name, dist, math.Abs(*w.Radius))
			}
			// Heading direction: rotated by the steering angle from +X.
			a := 0.0
			if w.AngleDeg != nil {
				a = *w.AngleDeg / deg
			}
			hx, hy := math.Cos(a), math.Sin(a)
			dot := vx*hx + vy*hy
			if math.Abs(dot) > 1e-9*scale {
				t.Errorf("%s: ICR not on heading normal (dot %g)", name, dot)
			}
		}
	}
}

// 8. Inside angle magnitude reaching 90 is rejected.
func TestOver90Rejected(t *testing.T) {
	for _, di := range []float64{90, -90, 90.0001, 120, 360} {
		if _, err := Compute(Input{2.7, 1.55, di}, DefaultTol); err == nil {
			t.Errorf("angle %v must be rejected", di)
		}
	}
}

// 9. Illegal dimensions and non-finite values are rejected with field errors.
func TestInvalidParametersRejected(t *testing.T) {
	bad := []Input{
		{0, 1.5, 30},
		{-2.7, 1.5, 30},
		{2.7, 0, 30},
		{2.7, -1.5, 30},
		{math.NaN(), 1.5, 30},
		{2.7, math.Inf(1), 30},
		{2.7, 1.5, math.NaN()},
	}
	for _, in := range bad {
		_, err := Compute(in, DefaultTol)
		if err == nil {
			t.Errorf("input %+v must be rejected", in)
			continue
		}
		if fe, ok := err.(*FieldError); !ok || fe.Field == "" || fe.Message == "" {
			t.Errorf("expected readable FieldError, got %v", err)
		}
	}
}

// Arc length ratio equals the corresponding radius ratio.
func TestArcRatiosEqualRadiusRatios(t *testing.T) {
	res, err := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	wantRear := math.Abs(*res.RearOutsideRadius / *res.RearInsideRadius)
	approx(t, "rear arc ratio", *res.Arc.RearOuterToRearInner, wantRear, 1e-12)
	wantFront := math.Abs(*res.FrontOutsideRadius / *res.FrontInsideRadius)
	approx(t, "front arc ratio", *res.Arc.FrontOuterToFrontInner, wantFront, 1e-12)
	approx(t, "rear inside self ratio", res.Arc.PerWheel["rear_inside"], 1, 1e-12)
	if *res.Arc.RearOuterToRearInner <= 1 {
		t.Error("outside wheel must travel a longer arc than inside")
	}
}

// tan(bicycle angle) = wheelbase / R.
func TestBicycleModelRelation(t *testing.T) {
	res, err := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	got := math.Tan(res.BicycleAngleDeg / deg)
	want := res.Wheelbase / *res.Radius
	approx(t, "tan(bicycle)", got, want, 1e-12)
}

// Rear wheel radii: R - T/2 (inside) and R + T/2 (outside) from the rear
// axle center.
func TestRearRadiusDefinitions(t *testing.T) {
	res, err := Compute(Input{2.7, 1.55, 30}, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	half := res.Track / 2
	approx(t, "rear inside", *res.RearInsideRadius, *res.Radius-half, 1e-12)
	approx(t, "rear outside", *res.RearOutsideRadius, *res.Radius+half, 1e-12)
}

// Preset: passenger-car dimensions, 30 degree inside lock, outside clearly
// below 30.
func TestPresetExample(t *testing.T) {
	if PresetInput.InsideAngle != 30 {
		t.Fatal("preset should use 30 degrees")
	}
	res, err := Compute(PresetInput, DefaultTol)
	if err != nil {
		t.Fatal(err)
	}
	if !(res.OutsideAngleDeg < 25 && res.OutsideAngleDeg > 20) {
		t.Errorf("preset outside angle expected ~23.4, got %v", res.OutsideAngleDeg)
	}
	if 30-res.OutsideAngleDeg < 4 {
		t.Error("outside angle must be clearly below 30")
	}
	if !res.GeometryOK {
		t.Error("preset geometry must verify")
	}
}

// Unit helpers.
func TestUnitConversions(t *testing.T) {
	c, ok := NormalizeUnit("radians")
	if !ok || c != "rad" {
		t.Fatalf("normalize radians: %q %v", c, ok)
	}
	approx(t, "pi/4 rad to deg", ToDegrees(math.Pi/4, "rad"), 45, 1e-12)
	approx(t, "deg passthrough", ToDegrees(30, "deg"), 30, 0)
	if _, ok := NormalizeUnit("gon"); ok {
		t.Error("unknown unit must not normalize")
	}
	if !UnitContradiction(400, "deg") || !UnitContradiction(10, "rad") {
		t.Error("impossible magnitudes must be flagged")
	}
	if UnitContradiction(1, "rad") || UnitContradiction(89, "deg") {
		t.Error("plausible magnitudes must not be flagged")
	}
}
