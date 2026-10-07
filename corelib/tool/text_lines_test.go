package tool

import "testing"

func TestSplitTextLinesDropsOnlyTheTrailingSplitSegment(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		last string
	}{
		{in: "", n: 1, last: ""},
		{in: "a", n: 1, last: "a"},
		{in: "a\n", n: 1, last: "a\n"},
		{in: "a\nb", n: 2, last: "b"},
		{in: "a\nb\n", n: 2, last: "b\n"},
		{in: "a\n\n", n: 2, last: "\n"},
		{in: "\n", n: 1, last: "\n"},
	}
	for _, tc := range cases {
		lines := SplitTextLines(tc.in)
		if len(lines) != tc.n || lines[len(lines)-1] != tc.last {
			t.Fatalf("SplitTextLines(%q) = %#v, want %d lines ending %q", tc.in, lines, tc.n, tc.last)
		}
	}
}
