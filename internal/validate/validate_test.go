package validate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidItem(t *testing.T) {
	c, err := Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Wheelbase != 2.7 || c.Track != 1.6 || c.InnerDeg != 30 || c.AngleUnit != "deg" {
		t.Fatalf("unexpected checked item: %+v", c)
	}
}

func TestRadiansConversion(t *testing.T) {
	c, err := Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":0.5235987755982988,"angle_unit":"rad"}`))
	if err != nil {
		t.Fatal(err)
	}
	if d := c.InnerDeg - 30; d > 1e-9 || d < -1e-9 {
		t.Fatalf("radian conversion: got %.12f degrees", c.InnerDeg)
	}
}

func TestMissingFields(t *testing.T) {
	cases := []struct {
		body  string
		field string
	}{
		{`{"track_m":1.6,"inner_angle":30}`, "wheelbase_m"},
		{`{"wheelbase_m":2.7,"inner_angle":30}`, "track_m"},
		{`{"wheelbase_m":2.7,"track_m":1.6}`, "inner_angle"},
	}
	for _, tc := range cases {
		_, err := Item(json.RawMessage(tc.body))
		if err == nil {
			t.Fatalf("body %s expected error", tc.body)
		}
		if !hasField(err, tc.field) {
			t.Fatalf("error for %s does not mention field %q: %+v", tc.body, tc.field, err.Fields)
		}
	}
}

func TestNonNumericFields(t *testing.T) {
	cases := []struct {
		body  string
		field string
	}{
		{`{"wheelbase_m":"2.7","track_m":1.6,"inner_angle":30}`, "wheelbase_m"},
		{`{"wheelbase_m":2.7,"track_m":true,"inner_angle":30}`, "track_m"},
		{`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":null}`, "inner_angle"},
		{`{"wheelbase_m":[1],"track_m":1.6,"inner_angle":30}`, "wheelbase_m"},
	}
	for _, tc := range cases {
		_, err := Item(json.RawMessage(tc.body))
		if err == nil || !hasField(err, tc.field) {
			t.Fatalf("body %s: expected field error on %q, got %+v", tc.body, tc.field, err)
		}
	}
}

func TestNonPositiveDimensions(t *testing.T) {
	for _, body := range []string{
		`{"wheelbase_m":0,"track_m":1.6,"inner_angle":30}`,
		`{"wheelbase_m":-2,"track_m":1.6,"inner_angle":30}`,
		`{"wheelbase_m":2.7,"track_m":0,"inner_angle":30}`,
		`{"wheelbase_m":2.7,"track_m":-1,"inner_angle":30}`,
	} {
		_, err := Item(json.RawMessage(body))
		if err == nil || err.Code != "invalid_parameters" {
			t.Fatalf("body %s: expected invalid_parameters, got %+v", body, err)
		}
	}
}

func TestInnerAt90Rejected(t *testing.T) {
	for _, body := range []string{
		`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":90}`,
		`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":-90}`,
		`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":120}`,
	} {
		_, err := Item(json.RawMessage(body))
		if err == nil || err.Code != "inner_angle_out_of_range" || !hasField(err, "inner_angle") {
			t.Fatalf("body %s: expected inner_angle_out_of_range, got %+v", body, err)
		}
	}
}

func TestAngleUnitContradiction(t *testing.T) {
	// 30 declared as radians converts to ~1719 degrees: contradiction.
	_, err := Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30,"angle_unit":"rad"}`))
	if err == nil || err.Code != "angle_unit_mismatch" || !hasField(err, "inner_angle") {
		t.Fatalf("expected angle_unit_mismatch, got %+v", err)
	}
	// pi/2 exactly tagged rad is out of range.
	_, err = Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":1.5707963267948966,"angle_unit":"rad"}`))
	if err == nil || err.Code != "angle_unit_mismatch" {
		t.Fatalf("expected angle_unit_mismatch for pi/2 rad, got %+v", err)
	}
	// Bogus unit string.
	_, err = Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30,"angle_unit":"gon"}`))
	if err == nil || !hasField(err, "angle_unit") {
		t.Fatalf("expected angle_unit field error, got %+v", err)
	}
	// Numeric unit value.
	_, err = Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30,"angle_unit":1}`))
	if err == nil || !hasField(err, "angle_unit") {
		t.Fatalf("expected angle_unit field error, got %+v", err)
	}
}

func TestMalformedJSON(t *testing.T) {
	for _, body := range []string{
		``, `not json`, `{"wheelbase_m":2.7,}`,
		`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30,"extra":1}`,
		`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":30} {"a":1}`,
	} {
		_, err := Item(json.RawMessage(body))
		if err == nil {
			t.Fatalf("body %q expected error", body)
		}
	}
}

func TestMultipleErrorsReportedTogether(t *testing.T) {
	_, err := Item(json.RawMessage(`{"wheelbase_m":-1,"track_m":0,"inner_angle":"x"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	want := map[string]bool{"wheelbase_m": false, "track_m": false, "inner_angle": false}
	for _, f := range err.Fields {
		if _, ok := want[f.Field]; ok {
			want[f.Field] = true
		}
	}
	for f, seen := range want {
		if !seen {
			t.Fatalf("missing field error for %s in %+v", f, err.Fields)
		}
	}
}

func TestErrorsAreReadable(t *testing.T) {
	_, err := Item(json.RawMessage(`{"wheelbase_m":2.7,"track_m":1.6,"inner_angle":90}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "90") || !strings.Contains(err.Error(), "inner") {
		t.Fatalf("error message not readable/specific: %q", err.Error())
	}
}

func hasField(err *Error, field string) bool {
	if err == nil {
		return false
	}
	for _, f := range err.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}
