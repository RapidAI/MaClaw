package skill

// runner_shared.go provides exported utility functions that are shared between
// GUI and TUI skill execution paths. These were originally unexported functions
// in gui/skill_runner.go. Exporting them here enables the TUI to reuse the
// same logic without duplicating code.
//
// The GUI skill_runner.go should be migrated to call these functions instead
// of its own copies. That migration is tracked as a follow-up to avoid a
// large diff in this change.

import (
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// DetectImplicitRequiredArgs scans step command templates for {{key}} / ${key}
// placeholders that are not provided in vars. Returns the list of missing keys.
// This catches skills that use {{input}}/{{output}} without declaring
// required_args in their frontmatter.
//
// Only scans bash action steps (aligned with GUI detectImplicitRequiredArgs).
func DetectImplicitRequiredArgs(steps []corelib.NLSkillStep, vars map[string]string) []string {
	seen := make(map[string]bool)
	for _, step := range steps {
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}
		for _, key := range ExtractPlaceholderKeys(cmd) {
			if key == "" || seen[key] {
				continue
			}
			if vars != nil && strings.TrimSpace(vars[key]) != "" {
				continue // already provided
			}
			seen[key] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	return result
}

// SubstituteVarsInString replaces {{key}}, ${key}, and {key} placeholders in s
// with values from vars. Handles all three formats that placeholderRe matches.
func SubstituteVarsInString(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		s = strings.ReplaceAll(s, "${"+k+"}", v)
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// EvaluateSimpleCondition evaluates a simple boolean expression string.
// Supports: "true"/"false", "yes"/"no", "!expr", "a == b", "a != b",
// bare non-empty string → true, empty string → false.
func EvaluateSimpleCondition(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	lower := strings.ToLower(expr)
	switch lower {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	}
	if strings.HasPrefix(expr, "!") {
		return !EvaluateSimpleCondition(expr[1:])
	}
	if strings.Contains(expr, "==") {
		parts := strings.SplitN(expr, "==", 2)
		return strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1])
	}
	if strings.Contains(expr, "!=") {
		parts := strings.SplitN(expr, "!=", 2)
		return strings.TrimSpace(parts[0]) != strings.TrimSpace(parts[1])
	}
	return true // non-empty string → true
}

// CaptureOutputVariables extracts variables from step output using regex
// patterns defined in the step's Capture map. Each key is a variable name,
// each value is a regex pattern with a capture group.
func CaptureOutputVariables(output string, captures map[string]string) map[string]string {
	result := make(map[string]string)
	for varName, pattern := range captures {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatch(output)
		if len(m) >= 2 {
			result[varName] = m[1]
		}
	}
	return result
}
