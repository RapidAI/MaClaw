package petpack

import (
	"regexp"
	"strconv"
	"strings"
)

// packVersionPattern is the version number a market upload must carry.
// Dotted numeric versions compare by id + version on the desktop. A leading
// "v" is accepted by NormalizePackVersion and is not part of the stored value.
var packVersionPattern = regexp.MustCompile(`^\d{1,9}(?:\.\d{1,9}){0,3}$`)

// NormalizePackVersion trims space and a single leading v/V.
func NormalizePackVersion(version string) string {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		version = strings.TrimSpace(version[1:])
	}
	return version
}

// ValidPackVersion reports whether version is a dotted numeric version number.
func ValidPackVersion(version string) bool {
	return packVersionPattern.MatchString(NormalizePackVersion(version))
}

// ComparePackVersions compares dotted numeric versions the same way the
// desktop market does. It returns -1 when a is older, 0 when equal, and 1
// when a is newer. Missing segments count as 0, so 1.0 and 1.0.0 are equal.
// A non-numeric side that differs compares as newer, so it is not a downgrade.
func ComparePackVersions(a, b string) int {
	a = NormalizePackVersion(a)
	b = NormalizePackVersion(b)
	if a == b {
		return 0
	}
	pa, aOK := numericPackVersion(a)
	pb, bOK := numericPackVersion(b)
	if !aOK || !bOK {
		return 1
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func numericPackVersion(version string) ([]int, bool) {
	if !ValidPackVersion(version) {
		return nil, false
	}
	parts := strings.Split(version, ".")
	out := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
