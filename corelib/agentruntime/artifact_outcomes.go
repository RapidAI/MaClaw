package agentruntime

import "strings"

// ResponseHasPDF identifies a PDF artifact using only transport-neutral
// metadata.  Hosts can use it to choose the same visible success projection
// without importing GUI response types.
func ResponseHasPDF(fileName, localPath, mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "application/pdf" || strings.HasPrefix(mime, "application/pdf;") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".pdf") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(localPath)), ".pdf")
}

// HostOwnedGeneratePDFSucceeded recognizes the stable success marker emitted
// by the trusted PDF adapter.  Error/unknown markers always win, preventing a
// friendly-looking failure string from being treated as a committed effect.
func HostOwnedGeneratePDFSucceeded(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "PDF artifact published") &&
		!strings.Contains(trimmed, "[system rejected]") &&
		!strings.Contains(trimmed, "[system unknown]")
}

// KeepVisibleErrorAfterArtifactAttach marks errors that must remain visible
// even when a host managed to attach a file.  Cancellation, policy rejection,
// budget exhaustion and recovery failures are not repaired by an artifact.
func KeepVisibleErrorAfterArtifactAttach(err string) bool {
	trimmed := strings.TrimSpace(err)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "cancelled" || strings.HasPrefix(lower, "cancelled ") {
		return true
	}
	for _, keep := range []string{
		"semantic_capability_unmet",
		"[system rejected]",
		"daily_llm_budget",
		"recovery_checkpoint_failed",
		"panicked",
	} {
		if strings.Contains(lower, keep) {
			return true
		}
	}
	return false
}

// ShouldClearStaleErrorAfterArtifactAttach is the inverse projection used by
// hosts after a successfully materialized artifact.
func ShouldClearStaleErrorAfterArtifactAttach(err string) bool {
	return strings.TrimSpace(err) != "" && !KeepVisibleErrorAfterArtifactAttach(err)
}
