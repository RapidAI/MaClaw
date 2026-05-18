package needledata

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{12,}|ghp_[a-z0-9_]{12,}|xox[baprs]-[a-z0-9-]{12,})\b`),
		regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|pwd)\s*[:=]\s*[^\s,;]+`),
		regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/-]+=*`),
	}
	absPathPattern = regexp.MustCompile(`(?i)([a-z]:\\[^\s"']+|/[^\s"']+)`)
	emailPattern   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// RedactText removes high-risk secrets and machine-specific paths while
// keeping enough task shape for routing fine-tuning.
func RedactText(s string) string {
	s = strings.TrimSpace(s)
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED_SECRET]")
	}
	s = emailPattern.ReplaceAllString(s, "[REDACTED_EMAIL]")
	s = absPathPattern.ReplaceAllStringFunc(s, func(path string) string {
		if strings.Contains(path, "://") {
			return path
		}
		return "[REDACTED_PATH]"
	})
	return s
}

func HashProject(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(project))
	return hex.EncodeToString(sum[:8])
}

func RedactEvent(e Event) Event {
	e.Input.UserText = RedactText(e.Input.UserText)
	e.Input.ShortContext = RedactText(e.Input.ShortContext)
	for i := range e.Input.AvailableTools {
		e.Input.AvailableTools[i].Description = RedactText(e.Input.AvailableTools[i].Description)
	}
	if e.NeedlePrediction != nil {
		redactDecision(e.NeedlePrediction)
	}
	if e.LLMPrediction != nil {
		redactDecision(e.LLMPrediction)
	}
	if e.RulePrediction != nil {
		redactDecision(e.RulePrediction)
	}
	redactDecision(&e.FinalDecision)
	e.Outcome.ToolError = RedactText(e.Outcome.ToolError)
	e.Privacy.Redacted = true
	return e
}

func redactDecision(d *Decision) {
	if d == nil {
		return
	}
	for k, v := range d.Arguments {
		if s, ok := v.(string); ok {
			d.Arguments[k] = RedactText(s)
		}
	}
}
