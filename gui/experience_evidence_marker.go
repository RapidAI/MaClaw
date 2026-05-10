package main

import "strings"

type experienceEvidenceMarkerKind int

const (
	experienceEvidenceMarkerNone experienceEvidenceMarkerKind = iota
	experienceEvidenceMarkerMatchedRollbackTriggers
)

func classifyExperienceEvidenceMarker(text string) experienceEvidenceMarkerKind {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "matched rollback triggers"):
		return experienceEvidenceMarkerMatchedRollbackTriggers
	default:
		return experienceEvidenceMarkerNone
	}
}
