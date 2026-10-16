package guiapp

import "github.com/RapidAI/CodeClaw/corelib/agentruntime"

// Review status vocabulary is frozen in corelib/agentruntime; aliases keep
// existing GUI call sites unchanged.
type experienceReviewStatus = agentruntime.ExperienceReviewStatus

const (
	experienceReviewStatusUnknown  = agentruntime.ExperienceReviewStatusUnknown
	experienceReviewStatusRequired = agentruntime.ExperienceReviewStatusRequired
	experienceReviewStatusApproved = agentruntime.ExperienceReviewStatusApproved
	experienceReviewStatusRejected = agentruntime.ExperienceReviewStatusRejected
	experienceReviewStatusDeferred = agentruntime.ExperienceReviewStatusDeferred
)

func normalizeExperienceReviewStatus(status string) experienceReviewStatus {
	return agentruntime.NormalizeExperienceReviewStatus(status)
}
