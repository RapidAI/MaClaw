package computeruse

import "strings"

// WindowTitlesMatch reports whether two window titles likely refer to the
// same top-level window. Empty values are treated as unknown (match).
func WindowTitlesMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}
