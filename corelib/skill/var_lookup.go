package skill

import (
	"sort"
	"strings"
)

func lookupCanonicalVar(vars map[string]string, key string) (string, bool) {
	key = canonicalRunVarKey(key)
	if key == "" || len(vars) == 0 {
		return "", false
	}
	if v := strings.TrimSpace(vars[key]); v != "" {
		return vars[key], true
	}

	candidates := make([]string, 0, len(vars))
	for candidate, value := range vars {
		if canonicalRunVarKey(candidate) == key && strings.TrimSpace(value) != "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.Strings(candidates)
	return vars[candidates[0]], true
}
