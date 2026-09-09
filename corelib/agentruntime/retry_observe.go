package agentruntime

import (
	"fmt"
	"strings"
)

// GuardDisabledTool returns a stable error when AdaptiveRetry has disabled the
// tool after repeated classified failures. Hosts must call this before
// executing a tool so GUI and srv share the same gate.
func (r *AdaptiveRetry) GuardDisabledTool(name string) (string, bool) {
	if r == nil || !r.IsDisabled(name) {
		return "", false
	}
	return fmt.Sprintf("Error: tool %s is disabled after repeated failures", strings.TrimSpace(name)), true
}

// ObserveToolFailure classifies a failed tool result and records it. A nil
// receiver is a no-op so hosts can omit AdaptiveRetry in tests.
func (r *AdaptiveRetry) ObserveToolFailure(name, result string, attempt int) RetryDecision {
	if r == nil || IsCapabilityUnavailable(nil, result) {
		return RetryDecision{}
	}
	category := r.Classify(name, fmt.Errorf("%s", strings.TrimSpace(result)))
	decision := r.Decide(name, category, attempt)
	if decision.ErrorContext == "" {
		decision.ErrorContext = truncateRetryContext(result)
	}
	r.RecordFailure(name, category, decision)
	return decision
}

func truncateRetryContext(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= 240 {
		return text
	}
	return string(runes[:240])
}
