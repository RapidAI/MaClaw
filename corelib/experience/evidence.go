package experience

import (
	"regexp"
	"strings"
)

const minEvidenceScore = defaultMinEvidenceScore

var evidenceTokenPattern = regexp.MustCompile(`[A-Za-z0-9_\-]{2,}`)
var commandSegmentPattern = regexp.MustCompile(`\s*(?:&&|\|\||\||;)\s*`)

var evidenceStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "when": true, "that": true,
	"this": true, "from": true, "into": true, "using": true, "use": true, "run": true,
	"step": true, "steps": true, "project": true, "service": true, "target": true,
}

var evidenceCommandStopwords = map[string]bool{
	"cd": true, "set": true, "export": true, "echo": true, "printf": true, "pwd": true,
	"dir": true, "ls": true, "cat": true, "type": true,
}

// EvidenceReport explains how strongly a candidate pattern is grounded in the
// session evidence that produced it.
type EvidenceReport struct {
	Score            int      `json:"score"`
	Reasons          []string `json:"reasons,omitempty"`
	UnsupportedSteps []string `json:"unsupported_steps,omitempty"`
}

func (r EvidenceReport) Passes() bool {
	return r.PassesThreshold(minEvidenceScore)
}

func (r EvidenceReport) PassesThreshold(threshold int) bool {
	return r.Score >= threshold && len(r.UnsupportedSteps) == 0
}

func EvaluateEvidenceSupport(p Pattern, history string) EvidenceReport {
	if strings.TrimSpace(history) == "" {
		return EvidenceReport{Score: minEvidenceScore, Reasons: []string{"no evidence gate input"}}
	}
	historyTokens := tokenSet(history)
	patternTokens := patternEvidenceTokens(p)

	overlap := 0
	for token := range patternTokens {
		if historyTokens[token] {
			overlap++
		}
	}

	report := EvidenceReport{}
	if overlap > 0 {
		report.Score += overlap
		report.Reasons = append(report.Reasons, "token overlap with session evidence")
	}

	supportedCommands, totalCommands, unsupportedCommands := commandEvidenceSupport(p, historyTokens)
	if totalCommands > 0 {
		if supportedCommands == totalCommands {
			report.Score += 3
			report.Reasons = append(report.Reasons, "all bash command signatures appear in session evidence")
		} else if supportedCommands > 0 {
			report.Score++
			report.Reasons = append(report.Reasons, "some bash command signatures appear in session evidence")
		}
		report.UnsupportedSteps = append(report.UnsupportedSteps, unsupportedCommands...)
	}
	if nonBashActionSupported(p, historyTokens) {
		report.Score++
		report.Reasons = append(report.Reasons, "non-bash action evidence appears in session evidence")
	}
	if report.Score > 6 {
		report.Score = 6
	}
	return report
}

func patternEvidenceTokens(p Pattern) map[string]bool {
	set := tokenSet(p.Name + " " + p.Description + " " + strings.Join(p.Triggers, " "))
	for _, step := range p.Steps {
		set[strings.ToLower(strings.TrimSpace(step.Action))] = true
		for _, value := range step.Params {
			collectEvidenceTokensFromValue(value, set)
		}
	}
	return set
}

func collectEvidenceTokensFromValue(value interface{}, set map[string]bool) {
	switch v := value.(type) {
	case string:
		for token := range tokenSet(v) {
			set[token] = true
		}
	case []interface{}:
		for _, item := range v {
			collectEvidenceTokensFromValue(item, set)
		}
	case map[string]interface{}:
		for key, item := range v {
			for token := range tokenSet(key) {
				set[token] = true
			}
			collectEvidenceTokensFromValue(item, set)
		}
	}
}

func commandEvidenceSupport(p Pattern, historyTokens map[string]bool) (supported int, total int, unsupported []string) {
	for _, step := range p.Steps {
		if strings.TrimSpace(step.Action) != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		for _, signature := range commandSignatures(cmd) {
			total++
			if commandSignatureSupported(signature, historyTokens) {
				supported++
			} else {
				unsupported = append(unsupported, signature)
			}
		}
	}
	return supported, total, unsupported
}

func commandSignatures(command string) []string {
	parts := commandSegmentPattern.Split(command, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		root := normalizeEvidenceToken(fields[0])
		if root == "" || evidenceCommandStopwords[root] {
			continue
		}
		sig := root
		for _, field := range fields[1:] {
			if strings.Contains(field, "{{") {
				continue
			}
			token := normalizeEvidenceToken(field)
			if token == "" || strings.HasPrefix(token, "-") || evidenceCommandStopwords[token] {
				continue
			}
			sig = root + ":" + token
			break
		}
		out = append(out, sig)
	}
	return out
}

func commandSignatureSupported(signature string, historyTokens map[string]bool) bool {
	parts := strings.Split(signature, ":")
	for _, part := range parts {
		if part == "" || !historyTokens[part] {
			return false
		}
	}
	return true
}

func nonBashActionSupported(p Pattern, historyTokens map[string]bool) bool {
	for _, step := range p.Steps {
		action := normalizeEvidenceToken(step.Action)
		if action == "" || action == "bash" {
			continue
		}
		if historyTokens[action] {
			return true
		}
		for key, value := range step.Params {
			if token := normalizeEvidenceToken(key); token != "" && historyTokens[token] {
				return true
			}
			if valueEvidenceSupported(value, historyTokens) {
				return true
			}
		}
	}
	return false
}

func valueEvidenceSupported(value interface{}, historyTokens map[string]bool) bool {
	supportedTokens := 0
	for token := range valueEvidenceTokens(value) {
		if historyTokens[token] {
			supportedTokens++
		}
	}
	return supportedTokens >= 2
}

func valueEvidenceTokens(value interface{}) map[string]bool {
	set := map[string]bool{}
	collectEvidenceTokensFromValue(value, set)
	return set
}

func tokenSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, raw := range evidenceTokenPattern.FindAllString(strings.ToLower(text), -1) {
		token := normalizeEvidenceToken(raw)
		if token != "" {
			set[token] = true
		}
	}
	return set
}

func normalizeEvidenceToken(token string) string {
	if strings.Contains(token, "{{") {
		return ""
	}
	token = strings.Trim(token, "'\"`.,:;()[]{}<>|/\\")
	if token == "" || evidenceStopwords[token] {
		return ""
	}
	if strings.HasPrefix(token, "--") {
		return ""
	}
	return token
}
