package experience

import (
	"regexp"
	"strings"
)

var nonSkillNameCharPattern = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizePatternName converts a model-proposed pattern name into the stable
// kebab-case skill identifier used by learned skills. It intentionally keeps a
// narrow ASCII surface so names remain portable across config files, folders,
// tool definitions, and API payloads.
func NormalizePatternName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	name = nonSkillNameCharPattern.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if len(name) > 64 {
		name = strings.TrimRight(name[:64], "-")
	}
	return name
}
