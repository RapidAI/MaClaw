package main

import "strings"

const experienceReviewedAtTagPrefix = "reviewed_at:"

type experienceReviewLifecycleTagKind string

const (
	experienceReviewLifecycleTagUnknown            experienceReviewLifecycleTagKind = ""
	experienceReviewLifecycleTagDeferred           experienceReviewLifecycleTagKind = "review_deferred"
	experienceReviewLifecycleTagRollbackReviewed   experienceReviewLifecycleTagKind = "rollback_reviewed"
	experienceReviewLifecycleTagRollbackRejected   experienceReviewLifecycleTagKind = "rollback_rejected"
	experienceReviewLifecycleTagConflictReviewed   experienceReviewLifecycleTagKind = "conflict_reviewed"
	experienceReviewLifecycleTagConflictRejected   experienceReviewLifecycleTagKind = "conflict_rejected"
	experienceReviewLifecycleTagSkillNudgeReviewed experienceReviewLifecycleTagKind = "skill_nudge_reviewed"
	experienceReviewLifecycleTagSkillNudgeRejected experienceReviewLifecycleTagKind = "skill_nudge_rejected"
)

func normalizeExperienceReviewLifecycleTagKind(tag string) experienceReviewLifecycleTagKind {
	switch experienceReviewLifecycleTagKind(strings.TrimSpace(tag)) {
	case experienceReviewLifecycleTagDeferred:
		return experienceReviewLifecycleTagDeferred
	case experienceReviewLifecycleTagRollbackReviewed:
		return experienceReviewLifecycleTagRollbackReviewed
	case experienceReviewLifecycleTagRollbackRejected:
		return experienceReviewLifecycleTagRollbackRejected
	case experienceReviewLifecycleTagConflictReviewed:
		return experienceReviewLifecycleTagConflictReviewed
	case experienceReviewLifecycleTagConflictRejected:
		return experienceReviewLifecycleTagConflictRejected
	case experienceReviewLifecycleTagSkillNudgeReviewed:
		return experienceReviewLifecycleTagSkillNudgeReviewed
	case experienceReviewLifecycleTagSkillNudgeRejected:
		return experienceReviewLifecycleTagSkillNudgeRejected
	default:
		return experienceReviewLifecycleTagUnknown
	}
}

func (kind experienceReviewLifecycleTagKind) String() string {
	return string(kind)
}

func (kind experienceReviewLifecycleTagKind) IsStateTag() bool {
	return kind != experienceReviewLifecycleTagUnknown
}
