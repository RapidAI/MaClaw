package guiapp

import "github.com/RapidAI/CodeClaw/corelib/agentruntime"

// Review outcome vocabulary is frozen in corelib/agentruntime; aliases keep
// existing GUI call sites unchanged.
type experienceReviewOutcomeKind = agentruntime.ExperienceReviewOutcomeKind

const experienceReviewOutcomeUnknown = agentruntime.ExperienceReviewOutcomeUnknown

func normalizeExperienceReviewOutcomeKind(value string) experienceReviewOutcomeKind {
	return agentruntime.NormalizeExperienceReviewOutcomeKind(value)
}
