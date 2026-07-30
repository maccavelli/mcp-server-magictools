package handler

import (
	"encoding/json"
	"testing"
)

// TestStripTrailingCommas_StringAware is the PROXY-M3 regression: trailing commas
// are removed only OUTSIDE string literals — a "," + "}" inside a string value
// (valid data) must be preserved.
func TestStripTrailingCommas_StringAware(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1,}`, `{"a":1}`},
		{`[1,2,]`, `[1,2]`},
		{`{"a":"x,}"}`, `{"a":"x,}"}`},         // ,} inside a string: untouched
		{`{"a":"v","b":2}`, `{"a":"v","b":2}`}, // ordinary commas untouched
		{`{"a":"es\"c,]"}`, `{"a":"es\"c,]"}`}, // escaped quote then ,] in string
	}
	for _, c := range cases {
		if got := stripTrailingCommas(c.in); got != c.want {
			t.Errorf("stripTrailingCommas(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAppendMissingCloseBraces_StringAware is the PROXY-M3 regression: braces
// inside string values must not be counted (no spurious '}' appended).
func TestAppendMissingCloseBraces_StringAware(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1`, `{"a":1}`},
		{`{"a":{"b":2`, `{"a":{"b":2}}`},
		{`{"a":"{{{"}`, `{"a":"{{{"}`}, // braces inside string: no append
		{`{"a":1}`, `{"a":1}`},         // already balanced
	}
	for _, c := range cases {
		if got := appendMissingCloseBraces(c.in); got != c.want {
			t.Errorf("appendMissingCloseBraces(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRepairJSONHeuristic_ValidUntouchedBrokenRepaired is the PROXY-M3 regression:
// already-valid JSON is returned byte-for-byte; genuinely broken JSON is repaired
// into valid JSON.
func TestRepairJSONHeuristic_ValidUntouchedBrokenRepaired(t *testing.T) {
	ps := &ProxyService{}

	// Valid JSON whose string value contains ",}" and "{" — must be untouched.
	valid := `{"note":"a,} weird { value","n":1}`
	if got := ps.repairJSONHeuristic(valid); got != valid {
		t.Errorf("valid JSON must be untouched: got %q", got)
	}

	// Broken: trailing comma + missing closing brace → repaired into valid JSON.
	got := ps.repairJSONHeuristic(`{"a":1,`)
	if !json.Valid([]byte(got)) {
		t.Errorf("repair should yield valid JSON, got %q", got)
	}
}
