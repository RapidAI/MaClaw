package main

import "strings"

type skillQualityGateStatus string

const (
	skillQualityGateStatusUnknown  skillQualityGateStatus = ""
	skillQualityGateStatusDraft    skillQualityGateStatus = "draft"
	skillQualityGateStatusApproved skillQualityGateStatus = "approved"
)

func normalizeSkillQualityGateStatus(status string) skillQualityGateStatus {
	switch skillQualityGateStatus(strings.ToLower(strings.TrimSpace(status))) {
	case skillQualityGateStatusDraft:
		return skillQualityGateStatusDraft
	case skillQualityGateStatusApproved:
		return skillQualityGateStatusApproved
	default:
		return skillQualityGateStatusUnknown
	}
}

func skillQualityGateStatusForScore(score int) skillQualityGateStatus {
	if score >= 1 {
		return skillQualityGateStatusApproved
	}
	return skillQualityGateStatusDraft
}
