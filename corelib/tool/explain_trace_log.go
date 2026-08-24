package tool

import (
	"log"
	"strconv"
	"strings"
	"unicode"
)

const (
	explainTraceLogEventLimit = 32
	explainTraceTokenMaxRunes = 96
)

// FormatExplainTraceLines renders a desensitised planner trace. It records
// plan identity and TP-stage decisions only: no user text, paths, parameters,
// or provider secrets are added by this formatter.
func FormatExplainTraceLines(trace ExplainTrace) []string {
	planID := sanitizeExplainTraceToken(trace.PlanID)
	snapshot := sanitizeExplainTraceToken(trace.SnapshotDigest)
	if planID == "" && snapshot == "" && len(trace.Events) == 0 {
		return nil
	}
	lines := []string{"[explain-trace] plan=" + planID + " snapshot=" + snapshot + " events=" + strconv.Itoa(len(trace.Events))}
	limit := len(trace.Events)
	if limit > explainTraceLogEventLimit {
		limit = explainTraceLogEventLimit
	}
	for i := 0; i < limit; i++ {
		event := trace.Events[i]
		stage := sanitizeExplainTraceToken(event.Stage)
		subject := sanitizeExplainTraceToken(event.Subject)
		name := sanitizeExplainTraceToken(event.Event)
		reason := sanitizeExplainTraceToken(event.ReasonCode)
		if stage == "" && subject == "" && name == "" && reason == "" {
			continue
		}
		lines = append(lines, "[explain-trace] stage="+stage+" subject="+subject+" event="+name+" reason="+reason)
	}
	return lines
}

// LogExplainTrace writes FormatExplainTraceLines to the process logger.
func LogExplainTrace(trace ExplainTrace) {
	for _, line := range FormatExplainTraceLines(trace) {
		log.Print(line)
	}
}

func sanitizeExplainTraceToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	runes := []rune(cleaned)
	if len(runes) > explainTraceTokenMaxRunes {
		return string(runes[:explainTraceTokenMaxRunes])
	}
	return cleaned
}
