package experience

import (
	"regexp"
	"strings"
)

var dangerousCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+-[^\n;&|]*r[^\n;&|]*f\b`),
	regexp.MustCompile(`(?i)\bdel\s+/(?:s|q)\b`),
	regexp.MustCompile(`(?i)\brmdir\s+/(?:s|q)\b`),
	regexp.MustCompile(`(?i)\bsudo\b`),
	regexp.MustCompile(`(?i)\bchmod\s+(?:777|a\+w)\b`),
	regexp.MustCompile(`(?i)\bchown\s+-R\b`),
	regexp.MustCompile(`(?i)\bcurl\b[^\n;&|]*\|\s*(?:sh|bash|sudo)\b`),
	regexp.MustCompile(`(?i)\bwget\b[^\n;&|]*\|\s*(?:sh|bash|sudo)\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=.*\s+of=/dev/`),
	regexp.MustCompile(`(?i)\bmkfs\.[a-z0-9]+\b`),
	regexp.MustCompile(`(?i)\b(systemctl|service)\s+(?:stop|disable)\b`),
}

func containsDangerousOperation(p Pattern) bool {
	for _, step := range p.Steps {
		if dangerousText(step.Action) || dangerousText(step.OnError) {
			return true
		}
		for _, value := range step.Params {
			if valueContainsDangerousOperation(value) {
				return true
			}
		}
	}
	return false
}

func valueContainsDangerousOperation(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return dangerousText(v)
	case []interface{}:
		for _, item := range v {
			if valueContainsDangerousOperation(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, item := range v {
			if dangerousText(key) || valueContainsDangerousOperation(item) {
				return true
			}
		}
	}
	return false
}

func dangerousText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, pattern := range dangerousCommandPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}
