package main

import "strings"

type experienceTraceKindID string

const (
	experienceTraceKindUnknown               experienceTraceKindID = ""
	experienceTraceKindA2AConflictReview     experienceTraceKindID = "a2a_conflict_review"
	experienceTraceKindA2ARollbackReview     experienceTraceKindID = "a2a_rollback_review"
	experienceTraceKindSkillNudgeReview      experienceTraceKindID = "skill_nudge_review"
	experienceTraceKindSkillNudgeCandidate   experienceTraceKindID = "skill_nudge_candidate"
	experienceTraceKindManualFollowUp        experienceTraceKindID = "manual_follow_up"
	experienceTraceKindReviewSignal          experienceTraceKindID = "review_signal"
	experienceTraceKindA2AEscalationEvidence experienceTraceKindID = "a2a_escalation_evidence"
	experienceTraceKindA2ADiscussionResult   experienceTraceKindID = "a2a_discussion_result"
	experienceTraceKindToolMemory            experienceTraceKindID = "tool_memory"
	experienceTraceKindRoutingHint           experienceTraceKindID = "routing_hint"
	experienceTraceKindUsagePattern          experienceTraceKindID = "usage_pattern"
	experienceTraceKindToolRecoveryPattern   experienceTraceKindID = "tool_recovery_pattern"
	experienceTraceKindSessionHistory        experienceTraceKindID = "session_history"
)

func normalizeExperienceTraceKind(kind string) experienceTraceKindID {
	switch experienceTraceKindID(strings.TrimSpace(kind)) {
	case experienceTraceKindA2AConflictReview:
		return experienceTraceKindA2AConflictReview
	case experienceTraceKindA2ARollbackReview:
		return experienceTraceKindA2ARollbackReview
	case experienceTraceKindSkillNudgeReview:
		return experienceTraceKindSkillNudgeReview
	case experienceTraceKindSkillNudgeCandidate:
		return experienceTraceKindSkillNudgeCandidate
	case experienceTraceKindManualFollowUp:
		return experienceTraceKindManualFollowUp
	case experienceTraceKindReviewSignal:
		return experienceTraceKindReviewSignal
	case experienceTraceKindA2AEscalationEvidence:
		return experienceTraceKindA2AEscalationEvidence
	case experienceTraceKindA2ADiscussionResult:
		return experienceTraceKindA2ADiscussionResult
	case experienceTraceKindToolMemory:
		return experienceTraceKindToolMemory
	case experienceTraceKindRoutingHint:
		return experienceTraceKindRoutingHint
	case experienceTraceKindUsagePattern:
		return experienceTraceKindUsagePattern
	case experienceTraceKindToolRecoveryPattern:
		return experienceTraceKindToolRecoveryPattern
	case experienceTraceKindSessionHistory:
		return experienceTraceKindSessionHistory
	default:
		return experienceTraceKindUnknown
	}
}

func (kind experienceTraceKindID) String() string {
	return string(kind)
}

func (kind experienceTraceKindID) IsA2A() bool {
	switch kind {
	case experienceTraceKindA2AConflictReview,
		experienceTraceKindA2ARollbackReview,
		experienceTraceKindA2AEscalationEvidence,
		experienceTraceKindA2ADiscussionResult:
		return true
	default:
		return false
	}
}

func (kind experienceTraceKindID) IsToolLearning() bool {
	switch kind {
	case experienceTraceKindToolMemory,
		experienceTraceKindRoutingHint,
		experienceTraceKindUsagePattern,
		experienceTraceKindToolRecoveryPattern,
		experienceTraceKindSkillNudgeCandidate,
		experienceTraceKindSkillNudgeReview:
		return true
	default:
		return false
	}
}

type experienceTraceSourceType string

const (
	experienceTraceSourceUnknown        experienceTraceSourceType = ""
	experienceTraceSourceToolUsage      experienceTraceSourceType = "tool_usage"
	experienceTraceSourceSessionHistory experienceTraceSourceType = "session_history"
)

const experienceTraceTagUsageRoutingHint = "usage_routing_hint"

func normalizeExperienceTraceSourceType(sourceType string) experienceTraceSourceType {
	switch experienceTraceSourceType(strings.TrimSpace(sourceType)) {
	case experienceTraceSourceToolUsage:
		return experienceTraceSourceToolUsage
	case experienceTraceSourceSessionHistory:
		return experienceTraceSourceSessionHistory
	default:
		return experienceTraceSourceUnknown
	}
}

func (sourceType experienceTraceSourceType) IsToolUsage() bool {
	return sourceType == experienceTraceSourceToolUsage
}

func (sourceType experienceTraceSourceType) IsSessionHistory() bool {
	return sourceType == experienceTraceSourceSessionHistory
}
