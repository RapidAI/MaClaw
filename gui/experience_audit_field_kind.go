package main

import "strings"

type experienceAuditFieldKind string

const (
	experienceAuditFieldUnknown    experienceAuditFieldKind = ""
	experienceAuditFieldReviewer   experienceAuditFieldKind = "reviewer"
	experienceAuditFieldReviewedAt experienceAuditFieldKind = "reviewed at"
	experienceAuditFieldActionKind experienceAuditFieldKind = "action kind"
	experienceAuditFieldActor      experienceAuditFieldKind = "actor"
	experienceAuditFieldRecordedAt experienceAuditFieldKind = "recorded at"
	experienceAuditFieldNote       experienceAuditFieldKind = "note"
)

func normalizeExperienceAuditFieldKind(key string) experienceAuditFieldKind {
	switch experienceAuditFieldKind(strings.ToLower(strings.TrimSpace(key))) {
	case experienceAuditFieldReviewer:
		return experienceAuditFieldReviewer
	case experienceAuditFieldReviewedAt:
		return experienceAuditFieldReviewedAt
	case experienceAuditFieldActionKind:
		return experienceAuditFieldActionKind
	case experienceAuditFieldActor:
		return experienceAuditFieldActor
	case experienceAuditFieldRecordedAt:
		return experienceAuditFieldRecordedAt
	case experienceAuditFieldNote:
		return experienceAuditFieldNote
	default:
		return experienceAuditFieldUnknown
	}
}
