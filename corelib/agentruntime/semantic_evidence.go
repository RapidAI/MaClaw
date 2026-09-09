package agentruntime

import "strings"

// LookupEvidenceUntrusted rejects tool/control-plane markers from text that
// is about to be reused as user-facing evidence or artifact input. Evidence
// may originate from a model or a tool adapter, so marker checks are kept in
// the shared Runtime contract instead of being reimplemented by each host.
func LookupEvidenceUntrusted(evidence string) bool {
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return true
	}
	if strings.Contains(evidence, "[system rejected]") ||
		strings.Contains(evidence, "[file_base64|") ||
		strings.Contains(evidence, "[system unknown]") {
		return true
	}
	lower := strings.ToLower(evidence)
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<turn: tool_call") ||
		strings.Contains(lower, "|dsml|") ||
		strings.Contains(evidence, "｜DSML｜")
}

// TrustedLookupEvidence returns evidence only when it is safe to reuse; an
// empty string is the stable fail-closed result for missing or tainted input.
func TrustedLookupEvidence(evidence string) string {
	evidence = strings.TrimSpace(evidence)
	if LookupEvidenceUntrusted(evidence) {
		return ""
	}
	return evidence
}
