package main

import "strings"

type experienceTraceQueryFilterKind string

const (
	experienceTraceQueryFilterAll           experienceTraceQueryFilterKind = ""
	experienceTraceQueryFilterReview        experienceTraceQueryFilterKind = "review"
	experienceTraceQueryFilterReviewed      experienceTraceQueryFilterKind = "reviewed"
	experienceTraceQueryFilterActions       experienceTraceQueryFilterKind = "actions"
	experienceTraceQueryFilterManualActions experienceTraceQueryFilterKind = "manual_actions"
	experienceTraceQueryFilterFollowUps     experienceTraceQueryFilterKind = "followups"
	experienceTraceQueryFilterA2A           experienceTraceQueryFilterKind = "a2a"
	experienceTraceQueryFilterTools         experienceTraceQueryFilterKind = "tools"
	experienceTraceQueryFilterSessions      experienceTraceQueryFilterKind = "sessions"
	experienceTraceQueryFilterUnknown       experienceTraceQueryFilterKind = "__unknown__"
)

func normalizeExperienceTraceQueryFilterKind(value string) experienceTraceQueryFilterKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return experienceTraceQueryFilterAll
	case "review", "needs_review", "required":
		return experienceTraceQueryFilterReview
	case "reviewed":
		return experienceTraceQueryFilterReviewed
	case "actions", "next_actions":
		return experienceTraceQueryFilterActions
	case "manual_actions", "action_queue":
		return experienceTraceQueryFilterManualActions
	case "followups", "follow_ups":
		return experienceTraceQueryFilterFollowUps
	case "a2a":
		return experienceTraceQueryFilterA2A
	case "tools":
		return experienceTraceQueryFilterTools
	case "sessions":
		return experienceTraceQueryFilterSessions
	default:
		return experienceTraceQueryFilterUnknown
	}
}

func (kind experienceTraceQueryFilterKind) String() string {
	return string(kind)
}

func (kind experienceTraceQueryFilterKind) IsUnknown() bool {
	return kind == experienceTraceQueryFilterUnknown
}
