package guiapp

import "strings"

// appendMissingPathEntries returns currentPath with every entry of extras that
// is not already present prepended, preserving order within each group. It is
// separated from the platform-specific caller so the merging rules (no
// duplicates, nothing lost, stable result under repeated calls) are the same
// everywhere and testable on the platform CI runs.
//
// Empty entries are dropped: appending "" to PATH would put the current
// directory first, which turns every relative exec into a search of whatever
// directory the process happens to sit in.
func appendMissingPathEntries(currentPath string, extras ...string) (string, bool) {
	parts := strings.Split(currentPath, ":")
	inPath := make(map[string]bool, len(parts))
	for _, p := range parts {
		inPath[p] = true
	}
	var missing []string
	for _, p := range extras {
		if p == "" || inPath[p] {
			continue
		}
		inPath[p] = true
		missing = append(missing, p)
	}
	if len(missing) == 0 {
		return currentPath, false
	}
	return strings.Join(append(missing, parts...), ":"), true
}
