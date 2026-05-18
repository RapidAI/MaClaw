package main

import "strings"

type experienceReviewKind string

const (
	experienceReviewKindUnknown      experienceReviewKind = ""
	experienceReviewKindRollback     experienceReviewKind = "rollback"
	experienceReviewKindConflict     experienceReviewKind = "conflict"
	experienceReviewKindSkillNudge   experienceReviewKind = "skill_nudge"
	experienceReviewKindToolRecovery experienceReviewKind = "tool_recovery"
)

func normalizeExperienceReviewKind(kind string) experienceReviewKind {
	switch experienceReviewKind(strings.TrimSpace(kind)) {
	case experienceReviewKindRollback:
		return experienceReviewKindRollback
	case experienceReviewKindConflict:
		return experienceReviewKindConflict
	case experienceReviewKindSkillNudge:
		return experienceReviewKindSkillNudge
	case experienceReviewKindToolRecovery:
		return experienceReviewKindToolRecovery
	default:
		return experienceReviewKindUnknown
	}
}

func (kind experienceReviewKind) String() string {
	return string(kind)
}
