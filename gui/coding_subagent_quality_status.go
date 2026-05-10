package main

import "strings"

type codingSubAgentQualityStatus string

const (
	codingSubAgentQualityUnknown   codingSubAgentQualityStatus = ""
	codingSubAgentQualityPassed    codingSubAgentQualityStatus = "passed"
	codingSubAgentQualityFailed    codingSubAgentQualityStatus = "failed"
	codingSubAgentQualityWarning   codingSubAgentQualityStatus = "warning"
	codingSubAgentQualityMissing   codingSubAgentQualityStatus = "missing"
	codingSubAgentQualityNotNeeded codingSubAgentQualityStatus = "not_needed"
	codingSubAgentQualityExplored  codingSubAgentQualityStatus = "explored"
	codingSubAgentQualityReadOnly  codingSubAgentQualityStatus = "read_only"
)

func normalizeCodingSubAgentQualityStatus(status string) codingSubAgentQualityStatus {
	switch codingSubAgentQualityStatus(strings.TrimSpace(status)) {
	case codingSubAgentQualityPassed:
		return codingSubAgentQualityPassed
	case codingSubAgentQualityFailed:
		return codingSubAgentQualityFailed
	case codingSubAgentQualityWarning:
		return codingSubAgentQualityWarning
	case codingSubAgentQualityMissing:
		return codingSubAgentQualityMissing
	case codingSubAgentQualityNotNeeded:
		return codingSubAgentQualityNotNeeded
	case codingSubAgentQualityExplored:
		return codingSubAgentQualityExplored
	case codingSubAgentQualityReadOnly:
		return codingSubAgentQualityReadOnly
	default:
		return codingSubAgentQualityUnknown
	}
}
