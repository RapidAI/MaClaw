package experience

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const minPatternQualityScore = defaultMinPatternQualityScore

var templateArgPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
var windowsAbsPathPattern = regexp.MustCompile(`(?i)\b[A-Z]:\\(?:Users|work|workspace|projects|tmp|temp)\\[^\s"']+`)
var unixAbsPathPattern = regexp.MustCompile(`/(?:home|Users|work|workspace|projects|tmp|var/tmp)/[^\s"']+`)

// QualityReport explains why a pattern is or is not worth turning into a skill.
type QualityReport struct {
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

func (r QualityReport) Passes() bool {
	return r.PassesThreshold(minPatternQualityScore)
}

func (r QualityReport) PassesThreshold(threshold int) bool {
	return r.Score >= threshold
}

// EvaluatePatternQuality scores a candidate pattern with deterministic local
// checks. The LLM proposes experience; this gate decides whether it is reusable
// enough to persist.
func EvaluatePatternQuality(p Pattern) QualityReport {
	report := QualityReport{}
	name := NormalizePatternName(p.Name)
	desc := strings.TrimSpace(p.Description)
	triggers := normalizeTriggers(p.Triggers)

	if learnedSkillNamePattern.MatchString(name) {
		report.add(1, "stable kebab-case name")
	}
	if len(desc) >= 40 {
		report.add(2, "description explains reusable context")
	} else if len(desc) >= 20 {
		report.add(1, "description is minimally informative")
	}
	if len(triggers) >= 3 {
		report.add(2, "has several trigger phrases")
	} else if len(triggers) >= 2 {
		report.add(1, "has minimal trigger coverage")
	}

	if len(p.Steps) >= 2 {
		report.add(2, "multi-step workflow")
	} else if len(p.Steps) == 1 && !isTrivialSingleCommand(p.Steps[0]) {
		report.add(1, "single non-trivial operation")
	}

	if len(ExtractRequiredArgs(p)) > 0 {
		report.add(2, "contains template parameters")
	}
	if hasRecoveryPolicy(p) {
		report.add(1, "has explicit error policy")
	}
	if containsOneOffPath(p) {
		report.add(-3, "contains one-off absolute paths")
	}
	if containsRedactionMarker(p) {
		report.add(-5, "contains redacted secret material")
	}
	if containsDangerousOperation(p) {
		report.add(-6, "contains dangerous operation")
	}
	if len(p.Steps) == 1 && isTrivialSingleCommand(p.Steps[0]) {
		report.add(-3, "looks like a trivial single command")
	}

	return report
}

// ExtractRequiredArgs returns sorted template variables used by step params.
func ExtractRequiredArgs(p Pattern) []string {
	seen := map[string]bool{}
	for _, step := range p.Steps {
		collectTemplateArgs(step.Action, seen)
		collectTemplateArgs(step.OnError, seen)
		for _, value := range step.Params {
			collectTemplateArgsFromValue(value, seen)
		}
	}
	out := make([]string, 0, len(seen))
	for arg := range seen {
		out = append(out, arg)
	}
	sort.Strings(out)
	return out
}

func (r *QualityReport) add(delta int, reason string) {
	r.Score += delta
	if reason != "" {
		r.Reasons = append(r.Reasons, fmt.Sprintf("%+d %s", delta, reason))
	}
}

func hasRecoveryPolicy(p Pattern) bool {
	for _, step := range p.Steps {
		onError := strings.TrimSpace(step.OnError)
		if onError == "continue" || onError == "stop" {
			return true
		}
	}
	return false
}

func containsRedactionMarker(p Pattern) bool {
	if containsRedactionMarkerText(p.Name) || containsRedactionMarkerText(p.Description) {
		return true
	}
	for _, trigger := range p.Triggers {
		if containsRedactionMarkerText(trigger) {
			return true
		}
	}
	for _, step := range p.Steps {
		if containsRedactionMarkerText(step.Action) || containsRedactionMarkerText(step.OnError) {
			return true
		}
		for _, value := range step.Params {
			if valueContainsRedactionMarker(value) {
				return true
			}
		}
	}
	return false
}

func valueContainsRedactionMarker(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return containsRedactionMarkerText(v)
	case []interface{}:
		for _, item := range v {
			if valueContainsRedactionMarker(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, item := range v {
			if containsRedactionMarkerText(key) || valueContainsRedactionMarker(item) {
				return true
			}
		}
	}
	return false
}

func containsRedactionMarkerText(text string) bool {
	return strings.Contains(text, "[REDACTED]")
}
func containsOneOffPath(p Pattern) bool {
	if containsOneOffPathText(p.Name) || containsOneOffPathText(p.Description) {
		return true
	}
	for _, trigger := range p.Triggers {
		if containsOneOffPathText(trigger) {
			return true
		}
	}
	for _, step := range p.Steps {
		if containsOneOffPathText(step.Action) || containsOneOffPathText(step.OnError) {
			return true
		}
		for _, value := range step.Params {
			if valueContainsOneOffPath(value) {
				return true
			}
		}
	}
	return false
}

func valueContainsOneOffPath(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return containsOneOffPathText(v)
	case []interface{}:
		for _, item := range v {
			if valueContainsOneOffPath(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, item := range v {
			if containsOneOffPathText(key) || valueContainsOneOffPath(item) {
				return true
			}
		}
	}
	return false
}

func containsOneOffPathText(text string) bool {
	return windowsAbsPathPattern.MatchString(text) || unixAbsPathPattern.MatchString(text)
}

func isTrivialSingleCommand(step Step) bool {
	if strings.TrimSpace(step.Action) != "bash" {
		return false
	}
	cmd, _ := step.Params["command"].(string)
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	cmd = strings.TrimSuffix(cmd, ";")
	trivial := map[string]bool{
		"git pull":          true,
		"git status":        true,
		"pwd":               true,
		"ls":                true,
		"ls -la":            true,
		"dir":               true,
		"npm install":       true,
		"go mod tidy":       true,
		"docker ps":         true,
		"docker compose ps": true,
	}
	if trivial[cmd] {
		return true
	}
	return len(strings.Fields(cmd)) <= 2 && !strings.Contains(cmd, "{{")
}

func collectTemplateArgsFromValue(value interface{}, seen map[string]bool) {
	switch v := value.(type) {
	case string:
		collectTemplateArgs(v, seen)
	case []interface{}:
		for _, item := range v {
			collectTemplateArgsFromValue(item, seen)
		}
	case map[string]interface{}:
		for key, item := range v {
			collectTemplateArgs(key, seen)
			collectTemplateArgsFromValue(item, seen)
		}
	}
}

func collectTemplateArgs(text string, seen map[string]bool) {
	for _, match := range templateArgPattern.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			seen[match[1]] = true
		}
	}
}
