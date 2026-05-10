package main

import "strings"

type groupDiscussionRiskKind string

const (
	groupDiscussionRiskUnknown  groupDiscussionRiskKind = ""
	groupDiscussionRiskLow      groupDiscussionRiskKind = "low"
	groupDiscussionRiskMedium   groupDiscussionRiskKind = "medium"
	groupDiscussionRiskHigh     groupDiscussionRiskKind = "high"
	groupDiscussionRiskCritical groupDiscussionRiskKind = "critical"
)

func normalizeGroupDiscussionRiskKind(value string) groupDiscussionRiskKind {
	switch groupDiscussionRiskKind(strings.ToLower(strings.TrimSpace(value))) {
	case groupDiscussionRiskLow:
		return groupDiscussionRiskLow
	case groupDiscussionRiskHigh:
		return groupDiscussionRiskHigh
	case groupDiscussionRiskCritical:
		return groupDiscussionRiskCritical
	case groupDiscussionRiskMedium, groupDiscussionRiskUnknown:
		return groupDiscussionRiskMedium
	default:
		return groupDiscussionRiskMedium
	}
}

func (kind groupDiscussionRiskKind) Rank() int {
	switch kind {
	case groupDiscussionRiskLow:
		return 1
	case groupDiscussionRiskHigh:
		return 3
	case groupDiscussionRiskCritical:
		return 4
	default:
		return 2
	}
}
