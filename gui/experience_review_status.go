package main

import "strings"

type experienceReviewStatus string

const (
	experienceReviewStatusUnknown  experienceReviewStatus = ""
	experienceReviewStatusRequired experienceReviewStatus = "required"
	experienceReviewStatusApproved experienceReviewStatus = experienceReviewOutcomeApproved
	experienceReviewStatusRejected experienceReviewStatus = experienceReviewOutcomeRejected
	experienceReviewStatusDeferred experienceReviewStatus = experienceReviewOutcomeDeferred
)

func normalizeExperienceReviewStatus(status string) experienceReviewStatus {
	switch experienceReviewStatus(strings.ToLower(strings.TrimSpace(status))) {
	case experienceReviewStatusRequired:
		return experienceReviewStatusRequired
	case experienceReviewStatusApproved:
		return experienceReviewStatusApproved
	case experienceReviewStatusRejected:
		return experienceReviewStatusRejected
	case experienceReviewStatusDeferred:
		return experienceReviewStatusDeferred
	default:
		return experienceReviewStatusUnknown
	}
}

func (s experienceReviewStatus) IsResolved() bool {
	switch s {
	case experienceReviewStatusApproved, experienceReviewStatusRejected:
		return true
	default:
		return false
	}
}

func (s experienceReviewStatus) IsRecordedReviewOutcome() bool {
	switch s {
	case experienceReviewStatusApproved, experienceReviewStatusRejected, experienceReviewStatusDeferred:
		return true
	default:
		return false
	}
}

func (s experienceReviewStatus) String() string {
	return string(s)
}
