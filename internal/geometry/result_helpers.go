package geometry

// InstantCenterFinite reports whether a finite instant center exists. It is
// false exactly for the straight-running case (inner angle zero), where the
// instant center lies at infinity and no finite radius may be reported.
func (r *Result) InstantCenterFinite() bool {
	return r.Sign != Straight
}
