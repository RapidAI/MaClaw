package main

import "strings"

type experienceFollowUpOutcomeKind string

const experienceFollowUpOutcomeUnknown experienceFollowUpOutcomeKind = ""

func normalizeExperienceFollowUpOutcomeKind(value string) experienceFollowUpOutcomeKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "complete", "completed", "done", "resolved":
		return experienceFollowUpOutcomeKind(experienceFollowUpOutcomeCompleted)
	case "block", "blocked", "reject", "rejected":
		return experienceFollowUpOutcomeKind(experienceFollowUpOutcomeBlocked)
	case "defer", "deferred", "later", "pending":
		return experienceFollowUpOutcomeKind(experienceFollowUpOutcomeDeferred)
	default:
		return experienceFollowUpOutcomeUnknown
	}
}

func (k experienceFollowUpOutcomeKind) String() string {
	return string(k)
}

func (k experienceFollowUpOutcomeKind) IsKnown() bool {
	return k != experienceFollowUpOutcomeUnknown
}
