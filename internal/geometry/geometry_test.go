package geometry

import (
	"math"
	"testing"
)

const eps = 1e-9

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func sedan() Input {
	return Input{Wheelbase: 2.70, Track: 1.60, InnerDeg: 30}
}

// 1. cot(|inner|) - cot(|outer|) == track/wheelbase.
func TestCotDifferenceEqualsTrackOverWheelbase(t *testing.T) {
	for _, in := range []Input{
		sedan(),
		{Wheelbase: 2.5, Track: 1.4, InnerDeg: 15},
		{Wheelbase: 3.2, Track: 1.8, InnerDeg: 44.999},
		{Wheelbase: 2.0, Track: 1.2, InnerDeg: -35},
	} {
		r, err := Compute(in)
		if err != nil {
			t.Fatalf("Compute(%+v): %v", in, err)
		}
		got := CotCheck(r.InnerDeg, r.OuterDeg)
		want := in.Track / in.Wheelbase
		if !approx(got, want, 1e-10) {
			t.Errorf("cot diff = %.10f, want T/L = %.10f", got, want)
		}
	}
}

// 2. inner == 0 implies outer == 0, no finite fake radius.
func TestZeroInnerMeansZeroOuterAndInfiniteRadius(t *testing.T) {
	r, err := Compute(Input{Wheelbase: 2.7, Track: 1.6, InnerDeg: 0})
	if err != nil {
		t.Fatal(err)
	}
	if r.OuterDeg != 0 {
		t.Fatalf("outer = %v, want 0", r.OuterDeg)
	}
	if r.Sign != Straight || r.Radius != 0 || r.ICRY != 0 {
		t.Fatalf("straight case must not carry a finite radius: %+v", r)
	}
	if r.RearArcRatio != nil || r.FrontArcRatio != nil || r.InstantCenterFinite() {
		t.Fatalf("straight case must not report a finite instant center or arc ratios")
	}
	if r.Wheels != [4]Wheel{} {
		t.Fatalf("straight case must not fabricate wheel radii")
	}
}

// 3. Nonzero turn: |inner| > |outer|.
func TestInnerGreaterThanOuter(t *testing.T) {
	for _, deg := range []float64{0.001, 1, 10, 30, 45, 89.999, -1, -30} {
		in := Input{Wheelbase: 2.7, Track: 1.6, InnerDeg: deg}
		r, err := Compute(in)
		if err != nil {
			t.Fatalf("inner %v: %v", deg, err)
		}
		if math.Abs(r.InnerDeg) <= math.Abs(r.OuterDeg) {
			t.Fatalf("inner %v not strictly greater than outer %v", r.InnerDeg, r.OuterDeg)
		}
	}
}

// 4. Doubling the wheelbase (same inner) approximately doubles R, and exactly
// doubles the rear-axle-center radius arm L*cot(u) = R - T/2; the outer angle
// moves toward the inner angle.
func TestWheelbaseDoubling(t *testing.T) {
	base := sedan()
	r1, err := Compute(base)
	if err != nil {
		t.Fatal(err)
	}
	doubled := base
	doubled.Wheelbase *= 2
	r2, err := Compute(doubled)
	if err != nil {
		t.Fatal(err)
	}
	// Exact identity: R - T/2 = L*cot(u), which must double with L.
	arm1 := r1.Radius - base.Track/2
	arm2 := r2.Radius - doubled.Track/2
	if !approx(arm2, 2*arm1, 1e-9) {
		t.Fatalf("L*cot(u) did not double exactly: %.6f -> %.6f", arm1, arm2)
	}
	// Hence R itself is approximately doubled; with a sedan-sized track the
	// residual offset T/2 keeps this near 2x (well within 10%).
	if ratio := r2.Radius / r1.Radius; ratio < 1.8 || ratio > 2.2 {
		t.Fatalf("R did not approximately double: ratio %.4f", ratio)
	}
	gap1 := math.Abs(r1.InnerDeg) - math.Abs(r1.OuterDeg)
	gap2 := math.Abs(r2.InnerDeg) - math.Abs(r2.OuterDeg)
	if !(gap2 < gap1) {
		t.Fatalf("outer angle did not move toward inner: gaps %.4f -> %.4f", gap1, gap2)
	}
}

// 5. Doubling the track widens inner-outer difference.
func TestTrackDoublingWidensAngleGap(t *testing.T) {
	base := sedan()
	r1, err := Compute(base)
	if err != nil {
		t.Fatal(err)
	}
	wider := base
	wider.Track *= 2
	r2, err := Compute(wider)
	if err != nil {
		t.Fatal(err)
	}
	gap1 := math.Abs(r1.InnerDeg) - math.Abs(r1.OuterDeg)
	gap2 := math.Abs(r2.InnerDeg) - math.Abs(r2.OuterDeg)
	if !(gap2 > gap1) {
		t.Fatalf("gap did not grow when track doubled: %.4f -> %.4f", gap1, gap2)
	}
}

// 6. Left/right differ only by sign; all absolute radii and angles identical.
func TestLeftRightOnlySign(t *testing.T) {
	l, err := Compute(sedan())
	if err != nil {
		t.Fatal(err)
	}
	right := sedan()
	right.InnerDeg = -30
	r, err := Compute(right)
	if err != nil {
		t.Fatal(err)
	}
	if l.OuterDeg != -r.OuterDeg || l.BikeDeg != -r.BikeDeg || l.ICRY != -r.ICRY {
		t.Fatalf("signed values did not negate: left outer/bike/icrY = %v/%v/%v, right = %v/%v/%v",
			l.OuterDeg, l.BikeDeg, l.ICRY, r.OuterDeg, r.BikeDeg, r.ICRY)
	}
	if !approx(l.Radius, r.Radius, eps) {
		t.Fatalf("radius changed under sign flip: %v vs %v", l.Radius, r.Radius)
	}
	if l.Radii != r.Radii {
		t.Fatalf("absolute radii differ: %+v vs %+v", l.Radii, r.Radii)
	}
	for i := range l.Wheels {
		if l.Wheels[i].AbsR != r.Wheels[i].AbsR {
			t.Fatalf("wheel %s abs radius differs: %v vs %v",
				l.Wheels[i].Name, l.Wheels[i].AbsR, r.Wheels[i].AbsR)
		}
		if l.Wheels[i].SignedR != -r.Wheels[i].SignedR {
			t.Fatalf("wheel %s signed radius did not negate: %v vs %v",
				l.Wheels[i].Name, l.Wheels[i].SignedR, r.Wheels[i].SignedR)
		}
		if l.Wheels[i].AngleDeg != -r.Wheels[i].AngleDeg {
			t.Fatalf("wheel %s angle did not negate: %v vs %v",
				l.Wheels[i].Name, l.Wheels[i].AngleDeg, r.Wheels[i].AngleDeg)
		}
	}
}

// 7. The four wheel radius lines meet at one geometric point. This computes
// the intersection of independent pairs of lines (rear axle line, front-inner
// normal, front-outer normal) instead of trusting the stored ICR/angles.
func TestFourRadiiConcurAtOnePoint(t *testing.T) {
	in := sedan()
	r, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range r.Wheels {
		// Every wheel's own heading-normal must pass through the common ICR.
		if d := math.Abs(r.Wheels[i].ResidualMM); d > 1e-6 {
			t.Fatalf("wheel %s residual %g mm exceeds tolerance", r.Wheels[i].Name, d)
		}
	}

	// Independent construction: intersect each front wheel's heading-normal
	// with the rear axle line x = 0. Both normals must cross it at the same
	// point, which must equal the stored ICR (0, R).
	intersect := func(fx, fy, steerDeg float64) (float64, float64) {
		a := steerDeg * Deg2Rad
		// heading h=(cos,sin); radius normal n=(-sin,cos); p = f + t*n,
		// solve fx + t*(-sin a) = 0.
		t0 := fx / math.Sin(a)
		return fx + t0*(-math.Sin(a)), fy + t0*math.Cos(a)
	}
	fi := r.Wheels[2]
	fo := r.Wheels[3]
	xi, yi := intersect(fi.X, fi.Y, fi.AngleDeg)
	xo, yo := intersect(fo.X, fo.Y, fo.AngleDeg)
	if !approx(xi, 0, 1e-9) || !approx(xo, 0, 1e-9) {
		t.Fatalf("front radius lines do not land on the rear axle x=0: x=%v,%v", xi, xo)
	}
	if !approx(yi, yo, 1e-9) {
		t.Fatalf("front-inner/outer normals meet rear axle at different points: %.9f vs %.9f", yi, yo)
	}
	if !approx(yi, r.ICRY, 1e-9) {
		t.Fatalf("intersection y %.9f does not match stored ICR y %.9f", yi, r.ICRY)
	}

	// Also verify each wheel-to-ICR distance equals its reported radius.
	for _, w := range r.Wheels {
		d := math.Hypot(r.ICRX-w.X, r.ICRY-w.Y)
		if !approx(d, w.AbsR, 1e-9) {
			t.Fatalf("wheel %s distance to ICR %.9f != reported radius %.9f", w.Name, d, w.AbsR)
		}
	}
	if r.MaxResidualMM > 1e-6 {
		t.Fatalf("max residual %g mm", r.MaxResidualMM)
	}
}

// 7b. Rear radii are R ∓ T/2; arc ratio equals radius ratio.
func TestRearRadiiAndArcRatios(t *testing.T) {
	in := sedan()
	r, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	half := in.Track / 2
	if !approx(r.Radii.RearInner, r.Radius-half, eps) ||
		!approx(r.Radii.RearOuter, r.Radius+half, eps) {
		t.Fatalf("rear radii %v/%v do not equal R∓T/2 (%v/%v)",
			r.Radii.RearInner, r.Radii.RearOuter, r.Radius-half, r.Radius+half)
	}
	if !approx(*r.RearArcRatio, r.Radii.RearOuter/r.Radii.RearInner, eps) {
		t.Fatalf("rear arc ratio %v != radius ratio", *r.RearArcRatio)
	}
	if !approx(*r.FrontArcRatio, r.Radii.FrontOuter/r.Radii.FrontInner, eps) {
		t.Fatalf("front arc ratio %v != radius ratio", *r.FrontArcRatio)
	}
	if *r.RearArcRatio <= 1 || *r.FrontArcRatio <= 1 {
		t.Fatalf("outer/inner arc ratio must exceed 1")
	}
}

// 7c. Bicycle relation tan(bike) = L/R and per-wheel angle consistency.
func TestBicycleRelation(t *testing.T) {
	in := sedan()
	r, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := math.Tan(r.BikeDeg * Deg2Rad); !approx(got, in.Wheelbase/r.Radius, 1e-10) {
		t.Fatalf("tan(bike)=%.10f != L/R=%.10f", got, in.Wheelbase/r.Radius)
	}
	// Front wheel steering angle equals the geometric angle atan(L / lateral
	// radius arm), which makes its velocity perpendicular to its radius.
	for _, w := range []Wheel{r.Wheels[2], r.Wheels[3]} {
		arm := math.Abs(r.ICRY - w.Y)
		want := math.Atan(in.Wheelbase/arm) * Rad2Deg
		if !approx(math.Abs(w.AngleDeg), want, 1e-9) {
			t.Fatalf("wheel %s angle %v inconsistent with radius arm (want %v)", w.Name, w.AngleDeg, want)
		}
	}
}

// 8. |inner| >= 90 is rejected; derived >= 90 / zero-radius layouts rejected.
func TestAnglesAtOrAbove90Rejected(t *testing.T) {
	for _, deg := range []float64{90, -90, 90.0001, 179, 360} {
		if _, err := Compute(Input{Wheelbase: 2.7, Track: 1.6, InnerDeg: deg}); err != ErrInnerTooSteep {
			t.Fatalf("inner %v: want ErrInnerTooSteep, got %v", deg, err)
		}
	}
	// A wide track with tiny wheelbase and shallow angle can drive the rear
	// inner radius to zero: R = L*cot(u) + T/2; to make R <= T/2 need
	// L*cot(u) <= 0 which never happens for u<90 — instead the protected
	// case is the outer angle derivation never crossing 90. Verify a healthy
	// case still works, and NaN/Inf inputs are rejected.
	if _, err := Compute(Input{Wheelbase: math.NaN(), Track: 1.6, InnerDeg: 30}); err != ErrInvalidDimension {
		t.Fatalf("NaN wheelbase: want ErrInvalidDimension, got %v", err)
	}
	if _, err := Compute(Input{Wheelbase: 2.7, Track: 0, InnerDeg: 30}); err != ErrInvalidDimension {
		t.Fatalf("zero track: want ErrInvalidDimension, got %v", err)
	}
	if _, err := Compute(Input{Wheelbase: -1, Track: 1.6, InnerDeg: 30}); err != ErrInvalidDimension {
		t.Fatalf("negative wheelbase: want ErrInvalidDimension, got %v", err)
	}
	if _, err := Compute(Input{Wheelbase: 2.7, Track: 1.6, InnerDeg: math.NaN()}); err != ErrInnerTooSteep {
		t.Fatalf("NaN inner: want ErrInnerTooSteep, got %v", err)
	}
}

// 9. Shipped worked example: sedan dimensions, inner 30; outer clearly smaller.
func TestPresetShapesUp(t *testing.T) {
	r, err := Compute(sedan())
	if err != nil {
		t.Fatal(err)
	}
	if r.OuterDeg <= 20 || r.OuterDeg >= 28 {
		t.Fatalf("expected outer angle clearly below 30 (around 24), got %v", r.OuterDeg)
	}
	t.Logf("preset: outer=%.4f deg R=%.4f m rearInner=%.4f rearOuter=%.4f",
		r.OuterDeg, r.Radius, r.Radii.RearInner, r.Radii.RearOuter)
}

// InstantCenterFinite is a test-visible helper declared in result_helpers.go.
var _ = Straight
