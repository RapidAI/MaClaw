package skill

import (
	"fmt"
	"strconv"
	"strings"
)

// ────────────────────────────────────────────────────────────────────────────
// Semantic Versioning utilities for skill dependency resolution.
//
// Supports: exact ("1.2.3"), range (">=1.2.0"), caret ("^1.2.0"),
// tilde ("~1.2.0"), wildcard ("*"), and combined constraints (">=1.2.0,<2.0.0").
// ────────────────────────────────────────────────────────────────────────────

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string // e.g. "beta.1"
}

// ParseSemVer parses a version string like "1.2.3" or "1.2.3-beta.1".
// Tolerates leading "v" prefix.
func ParseSemVer(s string) (SemVer, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return SemVer{}, false
	}

	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return SemVer{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return SemVer{}, false
	}

	minor := 0
	if len(parts) >= 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil || minor < 0 {
			return SemVer{}, false
		}
	}

	patch := 0
	if len(parts) >= 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil || patch < 0 {
			return SemVer{}, false
		}
	}

	return SemVer{Major: major, Minor: minor, Patch: patch, PreRelease: pre}, true
}

// String returns the canonical string representation.
func (v SemVer) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		s += "-" + v.PreRelease
	}
	return s
}

// Compare returns -1, 0, or 1 comparing v to other.
// Pre-release versions are lower than the release (1.0.0-beta < 1.0.0).
func (v SemVer) Compare(other SemVer) int {
	if v.Major != other.Major {
		return intCmp(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return intCmp(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return intCmp(v.Patch, other.Patch)
	}
	// Pre-release comparison
	if v.PreRelease == "" && other.PreRelease == "" {
		return 0
	}
	if v.PreRelease == "" {
		return 1 // release > pre-release
	}
	if other.PreRelease == "" {
		return -1 // pre-release < release
	}
	// Both have pre-release — lexicographic
	if v.PreRelease < other.PreRelease {
		return -1
	}
	if v.PreRelease > other.PreRelease {
		return 1
	}
	return 0
}

// IsGreaterThan returns true if v > other.
func (v SemVer) IsGreaterThan(other SemVer) bool {
	return v.Compare(other) > 0
}

func intCmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ────────────────────────────────────────────────────────────────────────────
// Version Constraint
// ────────────────────────────────────────────────────────────────────────────

// VersionConstraint represents a set of version requirements that must all
// be satisfied simultaneously (AND semantics for comma-separated ranges).
type VersionConstraint struct {
	Raw    string
	checks []versionCheck
}

type versionCheck struct {
	op      string // ">=", "<=", ">", "<", "=", "!="
	version SemVer
}

// ParseVersionConstraint parses a constraint string.
// Supported formats:
//   - "*" (any version)
//   - "1.2.3" (exact)
//   - ">=1.2.0" / ">1.0.0" / "<=2.0.0" / "<2.0.0" / "!=1.5.0"
//   - "^1.2.0" (same major: >=1.2.0, <2.0.0)
//   - "~1.2.0" (same minor: >=1.2.0, <1.3.0)
//   - ">=1.2.0,<2.0.0" (combined, comma separated)
func ParseVersionConstraint(s string) (*VersionConstraint, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return &VersionConstraint{Raw: s}, nil // matches everything
	}

	vc := &VersionConstraint{Raw: s}

	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Caret: ^X.Y.Z
		// When Major > 0: >=X.Y.Z, <(X+1).0.0 (same major)
		// When Major == 0 && Minor > 0: >=0.Y.Z, <0.(Y+1).0 (same minor)
		// When Major == 0 && Minor == 0: >=0.0.Z, <0.0.(Z+1) (exact patch)
		if strings.HasPrefix(part, "^") {
			v, ok := ParseSemVer(part[1:])
			if !ok {
				return nil, fmt.Errorf("invalid version in constraint %q", part)
			}
			vc.checks = append(vc.checks, versionCheck{op: ">=", version: v})
			var upper SemVer
			if v.Major > 0 {
				upper = SemVer{Major: v.Major + 1}
			} else if v.Minor > 0 {
				upper = SemVer{Major: 0, Minor: v.Minor + 1}
			} else {
				upper = SemVer{Major: 0, Minor: 0, Patch: v.Patch + 1}
			}
			vc.checks = append(vc.checks, versionCheck{op: "<", version: upper})
			continue
		}

		// Tilde: ~1.2.0 → >=1.2.0, <1.3.0
		if strings.HasPrefix(part, "~") {
			v, ok := ParseSemVer(part[1:])
			if !ok {
				return nil, fmt.Errorf("invalid version in constraint %q", part)
			}
			vc.checks = append(vc.checks,
				versionCheck{op: ">=", version: v},
				versionCheck{op: "<", version: SemVer{Major: v.Major, Minor: v.Minor + 1}},
			)
			continue
		}

		// Operators: >=, <=, >, <, !=, =
		op, vStr := parseOp(part)
		v, ok := ParseSemVer(vStr)
		if !ok {
			return nil, fmt.Errorf("invalid version in constraint %q", part)
		}
		vc.checks = append(vc.checks, versionCheck{op: op, version: v})
	}

	return vc, nil
}

// Satisfies checks if the given version satisfies all constraints.
func (vc *VersionConstraint) Satisfies(version string) bool {
	if vc == nil || len(vc.checks) == 0 {
		return true // no constraints = matches everything
	}
	v, ok := ParseSemVer(version)
	if !ok {
		return false
	}
	for _, check := range vc.checks {
		if !checkSatisfied(v, check) {
			return false
		}
	}
	return true
}

func checkSatisfied(v SemVer, c versionCheck) bool {
	cmp := v.Compare(c.version)
	switch c.op {
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "=", "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	default:
		return cmp == 0 // bare version = exact match
	}
}

func parseOp(s string) (op, version string) {
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "=="} {
		if strings.HasPrefix(s, prefix) {
			return prefix, strings.TrimSpace(s[len(prefix):])
		}
	}
	if strings.HasPrefix(s, "=") {
		return "=", strings.TrimSpace(s[1:])
	}
	return "=", s // bare version = exact match
}

// IsVersionGreater returns true if version a > version b (semver comparison).
// Returns false if either version cannot be parsed.
func IsVersionGreater(a, b string) bool {
	va, okA := ParseSemVer(a)
	vb, okB := ParseSemVer(b)
	if !okA || !okB {
		return false
	}
	return va.IsGreaterThan(vb)
}
