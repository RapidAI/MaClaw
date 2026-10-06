package experience

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
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

	sane, implausible := classifyTemplateArgs(p)
	if sane > 0 && implausible == 0 {
		report.add(2, "contains template parameters")
	} else if implausible > 0 {
		// Single-letter {{args}} are almost always shell loop variables or
		// format items misread from recorded commands — treating them as
		// template parameters used to be rewarded, which let broken learned
		// skills through the gate.
		report.add(-2, "template arguments look like misread shell syntax")
	}
	if hasRecoveryPolicy(p) {
		report.add(1, "has explicit error policy")
	}
	if n := countOneOffPaths(p); n > 0 {
		penalty := -3 * n
		if penalty < -12 {
			penalty = -12
		}
		report.add(penalty, fmt.Sprintf("contains %d distinct one-off absolute paths", n))
		if n >= 3 {
			// Many distinct unreplaced absolute paths means the pattern is a
			// verbatim recording of one session, not a reusable workflow.
			report.add(-4, "looks like a session snapshot (multiple unreplaced paths)")
		}
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

// classifyTemplateArgs splits ExtractRequiredArgs results into plausible
// parameter names and implausible ones. A canonical key shorter than two
// characters cannot be a meaningful user-facing parameter.
func classifyTemplateArgs(p Pattern) (sane, implausible int) {
	for _, arg := range ExtractRequiredArgs(p) {
		if len([]rune(coreskill.CanonicalRunVarKey(arg))) >= 2 {
			sane++
		} else {
			implausible++
		}
	}
	return sane, implausible
}

// countOneOffPaths returns the number of distinct one-off absolute paths in a
// pattern. A single incidental path is common; several distinct paths from the
// source session indicate a verbatim recording rather than a reusable skill.
func countOneOffPaths(p Pattern) int {
	seen := map[string]bool{}
	collect := func(text string) {
		for _, m := range windowsAbsPathPattern.FindAllString(text, -1) {
			seen[strings.ToLower(m)] = true
		}
		for _, m := range unixAbsPathPattern.FindAllString(text, -1) {
			seen[m] = true
		}
	}
	collect(p.Name)
	collect(p.Description)
	for _, trigger := range p.Triggers {
		collect(trigger)
	}
	for _, step := range p.Steps {
		collect(step.Action)
		collect(step.OnError)
		collectOneOffPathsFromValue(step.Params, seen)
	}
	return len(seen)
}

func collectOneOffPathsFromValue(value interface{}, seen map[string]bool) {
	switch v := value.(type) {
	case string:
		for _, m := range windowsAbsPathPattern.FindAllString(v, -1) {
			seen[strings.ToLower(m)] = true
		}
		for _, m := range unixAbsPathPattern.FindAllString(v, -1) {
			seen[m] = true
		}
	case []interface{}:
		for _, item := range v {
			collectOneOffPathsFromValue(item, seen)
		}
	case map[string]interface{}:
		for key, item := range v {
			for _, m := range windowsAbsPathPattern.FindAllString(key, -1) {
				seen[strings.ToLower(m)] = true
			}
			for _, m := range unixAbsPathPattern.FindAllString(key, -1) {
				seen[m] = true
			}
			collectOneOffPathsFromValue(item, seen)
		}
	}
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
