package main

import "strings"

type skillOutcomeStatus string

const (
	skillOutcomeStatusUnknown    skillOutcomeStatus = ""
	skillOutcomeStatusSuccess    skillOutcomeStatus = "success"
	skillOutcomeStatusFailure    skillOutcomeStatus = "failure"
	skillOutcomeStatusWorkaround skillOutcomeStatus = "workaround"
)

func normalizeSkillOutcomeStatus(outcome string) skillOutcomeStatus {
	switch skillOutcomeStatus(strings.TrimSpace(outcome)) {
	case skillOutcomeStatusSuccess:
		return skillOutcomeStatusSuccess
	case skillOutcomeStatusFailure:
		return skillOutcomeStatusFailure
	case skillOutcomeStatusWorkaround:
		return skillOutcomeStatusWorkaround
	default:
		return skillOutcomeStatusUnknown
	}
}
