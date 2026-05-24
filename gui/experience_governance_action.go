package main

import "strings"

type experienceGovernanceActionKind string

const (
	experienceGovernanceActionUnknown                           experienceGovernanceActionKind = ""
	experienceGovernanceActionReviewTriggeredRollbackSignal     experienceGovernanceActionKind = "review_triggered_rollback_signal"
	experienceGovernanceActionReviewRequiredTraces              experienceGovernanceActionKind = "review_required_traces"
	experienceGovernanceActionReviewSignal                      experienceGovernanceActionKind = "review_signal"
	experienceGovernanceActionInspectTriggeredRollbackFollowups experienceGovernanceActionKind = "inspect_triggered_rollback_followups"
	experienceGovernanceActionInspectFollowUpActions            experienceGovernanceActionKind = "inspect_follow_up_actions"
	experienceGovernanceActionExecuteApprovedSkillDraftReviews  experienceGovernanceActionKind = "execute_approved_skill_draft_reviews"
	experienceGovernanceActionInspectBlockedSkillDraftReviews   experienceGovernanceActionKind = "inspect_blocked_skill_draft_reviews"
	experienceGovernanceActionReviewRoutingCandidates           experienceGovernanceActionKind = "review_routing_candidates"
	experienceGovernanceActionInspectRoutingSignals             experienceGovernanceActionKind = "inspect_routing_signals"
	experienceGovernanceActionInspectToolRecoveryGovernance     experienceGovernanceActionKind = "inspect_tool_recovery_governance"
	experienceGovernanceActionInspectSkillNudgeCandidates       experienceGovernanceActionKind = "inspect_skill_nudge_candidates"
	experienceGovernanceActionBuildMemoryMaintenanceDraft       experienceGovernanceActionKind = "build_memory_maintenance_draft"
	experienceGovernanceActionInspectMemoryCandidates           experienceGovernanceActionKind = "inspect_memory_candidates"
	experienceGovernanceActionDraftSkillManually                experienceGovernanceActionKind = "draft_skill_manually"
	experienceGovernanceActionDraftRollbackWorkflow             experienceGovernanceActionKind = "draft_rollback_workflow"
	experienceGovernanceActionPrepareEscalationBrief            experienceGovernanceActionKind = "prepare_escalation_brief"
	experienceGovernanceActionResolveA2AConflictManually        experienceGovernanceActionKind = "resolve_a2a_conflict_manually"
	experienceGovernanceActionCollectA2AConflictEvidence        experienceGovernanceActionKind = "collect_a2a_conflict_evidence"
	experienceGovernanceActionCollectRollbackEvidence           experienceGovernanceActionKind = "collect_rollback_evidence"
	experienceGovernanceActionCollectSkillEvidence              experienceGovernanceActionKind = "collect_skill_evidence"
	experienceGovernanceActionBlockRollbackUse                  experienceGovernanceActionKind = "block_rollback_use"
	experienceGovernanceActionSuppressSkillCandidate            experienceGovernanceActionKind = "suppress_skill_candidate"
	experienceGovernanceActionKeepRejectedConflictEvidence      experienceGovernanceActionKind = "keep_rejected_conflict_evidence"
	experienceGovernanceActionNormalOperation                   experienceGovernanceActionKind = "normal_operation"
	experienceGovernanceActionManualFollowUp                    experienceGovernanceActionKind = "manual_follow_up"
)

func normalizeExperienceGovernanceActionKind(action string) experienceGovernanceActionKind {
	switch experienceGovernanceActionKind(strings.TrimSpace(action)) {
	case experienceGovernanceActionReviewTriggeredRollbackSignal:
		return experienceGovernanceActionReviewTriggeredRollbackSignal
	case experienceGovernanceActionReviewRequiredTraces:
		return experienceGovernanceActionReviewRequiredTraces
	case experienceGovernanceActionReviewSignal:
		return experienceGovernanceActionReviewSignal
	case experienceGovernanceActionInspectTriggeredRollbackFollowups:
		return experienceGovernanceActionInspectTriggeredRollbackFollowups
	case experienceGovernanceActionInspectFollowUpActions:
		return experienceGovernanceActionInspectFollowUpActions
	case experienceGovernanceActionExecuteApprovedSkillDraftReviews:
		return experienceGovernanceActionExecuteApprovedSkillDraftReviews
	case experienceGovernanceActionInspectBlockedSkillDraftReviews:
		return experienceGovernanceActionInspectBlockedSkillDraftReviews
	case experienceGovernanceActionReviewRoutingCandidates:
		return experienceGovernanceActionReviewRoutingCandidates
	case experienceGovernanceActionInspectRoutingSignals:
		return experienceGovernanceActionInspectRoutingSignals
	case experienceGovernanceActionInspectToolRecoveryGovernance:
		return experienceGovernanceActionInspectToolRecoveryGovernance
	case experienceGovernanceActionInspectSkillNudgeCandidates:
		return experienceGovernanceActionInspectSkillNudgeCandidates
	case experienceGovernanceActionBuildMemoryMaintenanceDraft:
		return experienceGovernanceActionBuildMemoryMaintenanceDraft
	case experienceGovernanceActionInspectMemoryCandidates:
		return experienceGovernanceActionInspectMemoryCandidates
	case experienceGovernanceActionDraftSkillManually:
		return experienceGovernanceActionDraftSkillManually
	case experienceGovernanceActionDraftRollbackWorkflow:
		return experienceGovernanceActionDraftRollbackWorkflow
	case experienceGovernanceActionPrepareEscalationBrief:
		return experienceGovernanceActionPrepareEscalationBrief
	case experienceGovernanceActionResolveA2AConflictManually:
		return experienceGovernanceActionResolveA2AConflictManually
	case experienceGovernanceActionCollectA2AConflictEvidence:
		return experienceGovernanceActionCollectA2AConflictEvidence
	case experienceGovernanceActionCollectRollbackEvidence:
		return experienceGovernanceActionCollectRollbackEvidence
	case experienceGovernanceActionCollectSkillEvidence:
		return experienceGovernanceActionCollectSkillEvidence
	case experienceGovernanceActionBlockRollbackUse:
		return experienceGovernanceActionBlockRollbackUse
	case experienceGovernanceActionSuppressSkillCandidate:
		return experienceGovernanceActionSuppressSkillCandidate
	case experienceGovernanceActionKeepRejectedConflictEvidence:
		return experienceGovernanceActionKeepRejectedConflictEvidence
	case experienceGovernanceActionNormalOperation:
		return experienceGovernanceActionNormalOperation
	case experienceGovernanceActionManualFollowUp:
		return experienceGovernanceActionManualFollowUp
	default:
		return experienceGovernanceActionKind(strings.TrimSpace(action))
	}
}

func (action experienceGovernanceActionKind) String() string {
	return string(action)
}

func (action experienceGovernanceActionKind) IsNormalOrEmpty() bool {
	return action == experienceGovernanceActionUnknown || action == experienceGovernanceActionNormalOperation
}

func (action experienceGovernanceActionKind) IsReviewSignal() bool {
	return action == experienceGovernanceActionReviewSignal
}

func (action experienceGovernanceActionKind) IsDraftBuildAction() bool {
	switch action {
	case experienceGovernanceActionDraftSkillManually,
		experienceGovernanceActionDraftRollbackWorkflow,
		experienceGovernanceActionPrepareEscalationBrief,
		experienceGovernanceActionResolveA2AConflictManually:
		return true
	default:
		return false
	}
}

func (action experienceGovernanceActionKind) IsFollowUpBuildAction() bool {
	switch action {
	case experienceGovernanceActionCollectA2AConflictEvidence,
		experienceGovernanceActionCollectRollbackEvidence,
		experienceGovernanceActionCollectSkillEvidence,
		experienceGovernanceActionBlockRollbackUse,
		experienceGovernanceActionSuppressSkillCandidate,
		experienceGovernanceActionKeepRejectedConflictEvidence:
		return true
	default:
		return false
	}
}

func (action experienceGovernanceActionKind) DraftToolAction() string {
	switch action {
	case experienceGovernanceActionDraftSkillManually:
		return "build_skill_draft"
	case experienceGovernanceActionDraftRollbackWorkflow:
		return "build_rollback_draft"
	case experienceGovernanceActionPrepareEscalationBrief:
		return "build_escalation_brief"
	case experienceGovernanceActionResolveA2AConflictManually:
		return "build_conflict_draft"
	default:
		return ""
	}
}

func (action experienceGovernanceActionKind) NeedsPriorityTraceTarget() bool {
	return action == experienceGovernanceActionReviewTriggeredRollbackSignal || action.IsDraftBuildAction() || action.IsFollowUpBuildAction()
}
