package guiapp

import "github.com/RapidAI/CodeClaw/corelib/agentruntime"

// The review-lifecycle tag vocabulary is frozen in corelib/agentruntime;
// these aliases keep existing GUI call sites unchanged.
const experienceReviewedAtTagPrefix = agentruntime.ExperienceReviewedAtTagPrefix

type experienceReviewLifecycleTagKind = agentruntime.ExperienceReviewLifecycleTagKind

const (
	experienceReviewLifecycleTagUnknown              = agentruntime.ExperienceReviewLifecycleTagUnknown
	experienceReviewLifecycleTagDeferred             = agentruntime.ExperienceReviewLifecycleTagDeferred
	experienceReviewLifecycleTagRollbackReviewed     = agentruntime.ExperienceReviewLifecycleTagRollbackReviewed
	experienceReviewLifecycleTagRollbackRejected     = agentruntime.ExperienceReviewLifecycleTagRollbackRejected
	experienceReviewLifecycleTagConflictReviewed     = agentruntime.ExperienceReviewLifecycleTagConflictReviewed
	experienceReviewLifecycleTagConflictRejected     = agentruntime.ExperienceReviewLifecycleTagConflictRejected
	experienceReviewLifecycleTagSkillNudgeReviewed   = agentruntime.ExperienceReviewLifecycleTagSkillNudgeReviewed
	experienceReviewLifecycleTagSkillNudgeRejected   = agentruntime.ExperienceReviewLifecycleTagSkillNudgeRejected
	experienceReviewLifecycleTagToolRecoveryReviewed = agentruntime.ExperienceReviewLifecycleTagToolRecoveryReviewed
	experienceReviewLifecycleTagToolRecoveryRejected = agentruntime.ExperienceReviewLifecycleTagToolRecoveryRejected
)

func normalizeExperienceReviewLifecycleTagKind(tag string) experienceReviewLifecycleTagKind {
	return agentruntime.NormalizeExperienceReviewLifecycleTagKind(tag)
}
