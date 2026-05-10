package main

import "strings"

type experienceReviewOutcomeKind string

const experienceReviewOutcomeUnknown experienceReviewOutcomeKind = ""

func normalizeExperienceReviewOutcomeKind(value string) experienceReviewOutcomeKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approve", "approved", "accept", "accepted", "ok":
		return experienceReviewOutcomeKind(experienceReviewOutcomeApproved)
	case "reject", "rejected", "deny", "denied":
		return experienceReviewOutcomeKind(experienceReviewOutcomeRejected)
	case "defer", "deferred", "later", "pending":
		return experienceReviewOutcomeKind(experienceReviewOutcomeDeferred)
	default:
		return experienceReviewOutcomeUnknown
	}
}

func (kind experienceReviewOutcomeKind) String() string {
	return string(kind)
}

func (kind experienceReviewOutcomeKind) IsKnown() bool {
	return kind != experienceReviewOutcomeUnknown
}
