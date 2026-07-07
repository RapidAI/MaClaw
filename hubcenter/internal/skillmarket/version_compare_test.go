package skillmarket

import "testing"

func TestIsVersionGreaterThan(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		// Basic numeric comparisons
		{"2.0.0", "1.0.0", true},
		{"1.1.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "2.0.0", false},
		// Pre-release vs release
		{"1.0.0", "1.0.0-beta.1", true},   // release > pre-release
		{"1.0.0-beta.1", "1.0.0", false},   // pre-release < release
		{"1.0.0-beta.2", "1.0.0-beta.1", true}, // lexicographic
		{"1.0.0-alpha", "1.0.0-beta", false},    // alpha < beta
		// Partial versions
		{"2", "1", true},
		{"1.1", "1.0", true},
		// With v prefix
		{"v2.0.0", "v1.0.0", true},
		{"v1.0.0", "v2.0.0", false},
		// Non-semver fallback (lexicographic)
		{"abc", "aab", true},
		{"", "", false},
	}
	for _, tt := range tests {
		got := isVersionGreaterThan(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("isVersionGreaterThan(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseSemVerParts(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
		major int
		minor int
		patch int
		pre   string
	}{
		{"1.2.3", true, 1, 2, 3, ""},
		{"1.2.3-beta.1", true, 1, 2, 3, "beta.1"},
		{"v10.0.0", true, 10, 0, 0, ""},
		{"1.0", true, 1, 0, 0, ""},
		{"1", true, 1, 0, 0, ""},
		{"", false, 0, 0, 0, ""},
		{"abc", false, 0, 0, 0, ""},
	}
	for _, tt := range tests {
		got, ok := parseSemVerParts(tt.input)
		if ok != tt.ok {
			t.Errorf("parseSemVerParts(%q) ok=%v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.major != tt.major || got.minor != tt.minor || got.patch != tt.patch || got.pre != tt.pre {
			t.Errorf("parseSemVerParts(%q) = %+v, want {%d,%d,%d,%q}", tt.input, got, tt.major, tt.minor, tt.patch, tt.pre)
		}
	}
}
