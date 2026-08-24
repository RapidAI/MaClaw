package browser

import (
	"strings"
	"testing"
)

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
		{"small page minor change", 50, 48, 0.05, true},   // 2/48 = 4.2%
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
		wantMut   int
	}{
		{
			"complete page",
			`{"ready":"complete","url":"https://example.com/app","text":4521,"mut":3}`,
			"complete", "https://example.com/app", 4521, 3,
		},
		{
			"interactive page",
			`{"ready":"interactive","url":"https://spa.test/","text":120}`,
			"interactive", "https://spa.test/", 120, 0,
		},
		{
			"loading page",
			`{"ready":"loading","url":"about:blank","text":0}`,
			"loading", "about:blank", 0, 0,
		},
		{
			"invalid json",
			`not json`,
			"", "", 0, 0,
		},
		{
			"empty string",
			``,
			"", "", 0, 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, url, textLen, mut := parseStabilitySignature(tc.sig)
			if ready != tc.wantReady || url != tc.wantURL || textLen != tc.wantText || mut != tc.wantMut {
				t.Fatalf("parseStabilitySignature(%q) = (%q, %q, %d, %d), want (%q, %q, %d, %d)",
					tc.sig, ready, url, textLen, mut, tc.wantReady, tc.wantURL, tc.wantText, tc.wantMut)
			}
		})
	}
}

func TestStabilityProbeWalksIframes(t *testing.T) {
	for _, marker := range []string{"__maclawMut", "contentDocument", "iframe", "readyState", "queryIframes", "shadowRoot"} {
		if !strings.Contains(stabilityProbeJS, marker) {
			t.Fatalf("stability probe missing %q", marker)
		}
	}
	if strings.Contains(stabilityProbeJS, "innerText") || strings.Contains(stabilityProbeJS, "textContent") {
		t.Fatal("stability probe must not use body text length")
	}
}

func TestWaitTimeoutSec(t *testing.T) {
	if got := waitTimeoutSec(0); got != 10 {
		t.Fatalf("default timeout = %d, want 10", got)
	}
	if got := waitTimeoutSec(500); got != 10 {
		t.Fatalf("sub-second timeout = %d, want 10", got)
	}
	if got := waitTimeoutSec(1000); got != 1 {
		t.Fatalf("1s timeout = %d, want 1", got)
	}
	if got := waitTimeoutSec(2500); got != 3 {
		t.Fatalf("2500ms timeout = %d, want 3", got)
	}
	if got := waitTimeoutSec(10000); got != 10 {
		t.Fatalf("10s timeout = %d, want 10", got)
	}
}
