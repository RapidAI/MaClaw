package skill

import "testing"

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input string
		want  SemVer
		ok    bool
	}{
		{"1.2.3", SemVer{1, 2, 3, ""}, true},
		{"0.0.1", SemVer{0, 0, 1, ""}, true},
		{"10.20.30", SemVer{10, 20, 30, ""}, true},
		{"1.2.3-beta.1", SemVer{1, 2, 3, "beta.1"}, true},
		{"v1.2.3", SemVer{1, 2, 3, ""}, true},
		{"1.0", SemVer{1, 0, 0, ""}, true},
		{"1", SemVer{1, 0, 0, ""}, true},
		{"", SemVer{}, false},
		{"abc", SemVer{}, false},
		{"-1.0.0", SemVer{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseSemVer(tt.input)
		if ok != tt.ok {
			t.Errorf("ParseSemVer(%q) ok=%v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("ParseSemVer(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSemVerCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.0-beta", 1},      // release > pre-release
		{"1.0.0-beta", "1.0.0", -1},      // pre-release < release
		{"1.0.0-alpha", "1.0.0-beta", -1}, // lexicographic
		{"1.0.0-beta.2", "1.0.0-beta.1", 1},
	}
	for _, tt := range tests {
		a, _ := ParseSemVer(tt.a)
		b, _ := ParseSemVer(tt.b)
		got := a.Compare(b)
		if got != tt.want {
			t.Errorf("(%s).Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestVersionConstraint(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		// Wildcard
		{"*", "1.2.3", true},
		{"", "1.2.3", true},
		// Exact
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		// Range operators
		{">=1.2.0", "1.2.0", true},
		{">=1.2.0", "1.3.0", true},
		{">=1.2.0", "1.1.9", false},
		{">1.0.0", "1.0.1", true},
		{">1.0.0", "1.0.0", false},
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"<=2.0.0", "2.0.0", true},
		{"!=1.5.0", "1.5.0", false},
		{"!=1.5.0", "1.5.1", true},
		// Caret (same major)
		{"^1.2.0", "1.2.0", true},
		{"^1.2.0", "1.9.9", true},
		{"^1.2.0", "2.0.0", false},
		{"^1.2.0", "1.1.0", false},
		// Caret with major 0 (same minor)
		{"^0.2.0", "0.2.0", true},
		{"^0.2.0", "0.2.9", true},
		{"^0.2.0", "0.3.0", false},
		{"^0.2.0", "1.0.0", false},
		// Caret with 0.0.x (exact patch)
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		// Tilde (same minor)
		{"~1.2.0", "1.2.0", true},
		{"~1.2.0", "1.2.9", true},
		{"~1.2.0", "1.3.0", false},
		// Combined
		{">=1.2.0,<2.0.0", "1.5.0", true},
		{">=1.2.0,<2.0.0", "2.0.0", false},
		{">=1.2.0,<2.0.0", "1.1.0", false},
	}
	for _, tt := range tests {
		vc, err := ParseVersionConstraint(tt.constraint)
		if err != nil {
			t.Errorf("ParseVersionConstraint(%q) error: %v", tt.constraint, err)
			continue
		}
		got := vc.Satisfies(tt.version)
		if got != tt.want {
			t.Errorf("constraint %q satisfies %q = %v, want %v", tt.constraint, tt.version, got, tt.want)
		}
	}
}

func TestIsVersionGreater(t *testing.T) {
	if !IsVersionGreater("1.1.0", "1.0.0") {
		t.Error("1.1.0 should be > 1.0.0")
	}
	if IsVersionGreater("1.0.0", "1.0.0") {
		t.Error("1.0.0 should not be > 1.0.0")
	}
	if IsVersionGreater("invalid", "1.0.0") {
		t.Error("invalid should not be > 1.0.0")
	}
}
