package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestParseDesktopDisplayIndexAcceptsJSONShapes(t *testing.T) {
	cases := []struct {
		raw  any
		want int
	}{
		{raw: float64(2), want: 2},
		{raw: 3, want: 3},
		{raw: " 4 ", want: 4},
		{raw: json.Number("5"), want: 5},
	}
	for _, tc := range cases {
		got, err := ParseDesktopDisplayIndex(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("ParseDesktopDisplayIndex(%#v) = %d, %v want %d", tc.raw, got, err, tc.want)
		}
	}
}

func TestParseDesktopDisplayIndexRejectsInvalidValues(t *testing.T) {
	for _, raw := range []any{"1abc", "", true} {
		if _, err := ParseDesktopDisplayIndex(raw); err == nil {
			t.Fatalf("ParseDesktopDisplayIndex(%#v) error = nil, want error", raw)
		}
	}
}
