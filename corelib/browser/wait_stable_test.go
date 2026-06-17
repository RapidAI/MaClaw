package browser

import "testing"

func TestTextLenWithinTolerance(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		previous int
		tol      float64
		want     bool
	}{
		{"both zero", 0, 0, 0.05, true},
		{"previous zero current nonzero", 100, 0, 0.05, false},
		{"exact match", 1000, 1000, 0.05, true},
		{"within 5%", 1040, 1000, 0.05, true},
		{"at boundary", 1050, 1000, 0.05, true},
		{"over 5%", 1060, 1000, 0.05, false},
		{"decrease within", 960, 1000, 0.05, true},
		{"decrease over", 940, 1000, 0.05, false},
		{"small page minor change", 50, 48, 0.05, true},  // 2/48 = 4.2%
		{"small page larger change", 45, 48, 0.05, false}, // 3/48 = 6.3%
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := textLenWithinTolerance(tc.current, tc.previous, tc.tol)
			if got != tc.want {
				t.Fatalf("textLenWithinTolerance(%d, %d, %f) = %v, want %v",
					tc.current, tc.previous, tc.tol, got, tc.want)
			}
		})
	}
}

func TestParseStabilitySignature(t *testing.T) {
	tests := []struct {
		name      string
		sig       string
		wantReady string
		wantURL   string
		wantText  int
	}{
		{
			"complete page",
			`{"ready":"complete","url":"https://example.com/app","text":4521}`,
			"complete", "https://example.com/app", 4521,
		},
		{
			"interactive page",
			`{"ready":"interactive","url":"https://spa.test/","text":120}`,
			"interactive", "https://spa.test/", 120,
		},
		{
			"loading page",
			`{"ready":"loading","url":"about:blank","text":0}`,
			"loading", "about:blank", 0,
		},
		{
			"invalid json",
			`not json`,
			"", "", 0,
		},
		{
			"empty string",
			``,
			"", "", 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, url, textLen := parseStabilitySignature(tc.sig)
			if ready != tc.wantReady || url != tc.wantURL || textLen != tc.wantText {
				t.Fatalf("parseStabilitySignature(%q) = (%q, %q, %d), want (%q, %q, %d)",
					tc.sig, ready, url, textLen, tc.wantReady, tc.wantURL, tc.wantText)
			}
		})
	}
}
