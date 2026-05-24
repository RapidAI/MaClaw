package main

import "strings"

type experienceLearningToolAction string

const (
	experienceLearningToolActionSnapshot                    experienceLearningToolAction = "snapshot"
	experienceLearningToolActionGovernanceSummary           experienceLearningToolAction = "governance_summary"
	experienceLearningToolActionNextActions                 experienceLearningToolAction = "next_actions"
	experienceLearningToolActionQueues                      experienceLearningToolAction = "queues"
	experienceLearningToolActionFollowUpActions             experienceLearningToolAction = "follow_up_actions"
	experienceLearningToolActionRoutingSignals              experienceLearningToolAction = "routing_signals"
	experienceLearningToolActionToolRecovery                experienceLearningToolAction = "tool_recovery"
	experienceLearningToolActionBuildRoutingAdjustmentDraft experienceLearningToolAction = "build_routing_adjustment_draft"
	experienceLearningToolActionMemoryCandidates            experienceLearningToolAction = "memory_candidates"
	experienceLearningToolActionBuildMemoryMaintenanceDraft experienceLearningToolAction = "build_memory_maintenance_draft"
	experienceLearningToolActionTraceDetails                experienceLearningToolAction = "trace_details"
	experienceLearningToolActionBuildFollowUp               experienceLearningToolAction = "build_followup"
	experienceLearningToolActionBuildSkillDraft             experienceLearningToolAction = "build_skill_draft"
	experienceLearningToolActionBuildBlockedSkillDraft      experienceLearningToolAction = "build_blocked_skill_draft"
	experienceLearningToolActionBuildRollbackDraft          experienceLearningToolAction = "build_rollback_draft"
	experienceLearningToolActionBuildEscalationBrief        experienceLearningToolAction = "build_escalation_brief"
	experienceLearningToolActionBuildConflictDraft          experienceLearningToolAction = "build_conflict_draft"
	experienceLearningToolActionRecordFollowUp              experienceLearningToolAction = "record_followup"
	experienceLearningToolActionRecordReview                experienceLearningToolAction = "record_review"
	experienceLearningToolActionRecordDraftReview           experienceLearningToolAction = "record_draft_review"
	experienceLearningToolActionRecordBlockedSkillDraft     experienceLearningToolAction = "record_blocked_skill_draft_review"
	experienceLearningToolActionUnknown                     experienceLearningToolAction = ""
)

func normalizeExperienceLearningToolAction(value string) experienceLearningToolAction {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(experienceLearningToolActionSnapshot):
		return experienceLearningToolActionSnapshot
	case string(experienceLearningToolActionGovernanceSummary), "summary":
		return experienceLearningToolActionGovernanceSummary
	case string(experienceLearningToolActionNextActions):
		return experienceLearningToolActionNextActions
	case string(experienceLearningToolActionQueues), "governance_queues":
		return experienceLearningToolActionQueues
	case string(experienceLearningToolActionFollowUpActions), "followup_actions":
		return experienceLearningToolActionFollowUpActions
	case string(experienceLearningToolActionRoutingSignals):
		return experienceLearningToolActionRoutingSignals
	case string(experienceLearningToolActionToolRecovery), "inspect_tool_recovery_governance", "recovery_governance", "tool_recovery_governance":
		return experienceLearningToolActionToolRecovery
	case string(experienceLearningToolActionBuildRoutingAdjustmentDraft):
		return experienceLearningToolActionBuildRoutingAdjustmentDraft
	case string(experienceLearningToolActionMemoryCandidates):
		return experienceLearningToolActionMemoryCandidates
	case string(experienceLearningToolActionBuildMemoryMaintenanceDraft):
		return experienceLearningToolActionBuildMemoryMaintenanceDraft
	case string(experienceLearningToolActionTraceDetails):
		return experienceLearningToolActionTraceDetails
	case string(experienceLearningToolActionBuildFollowUp):
		return experienceLearningToolActionBuildFollowUp
	case string(experienceLearningToolActionBuildSkillDraft):
		return experienceLearningToolActionBuildSkillDraft
	case string(experienceLearningToolActionBuildBlockedSkillDraft), "build_blocked_skill_repair_draft":
		return experienceLearningToolActionBuildBlockedSkillDraft
	case string(experienceLearningToolActionBuildRollbackDraft):
		return experienceLearningToolActionBuildRollbackDraft
	case string(experienceLearningToolActionBuildEscalationBrief):
		return experienceLearningToolActionBuildEscalationBrief
	case string(experienceLearningToolActionBuildConflictDraft):
		return experienceLearningToolActionBuildConflictDraft
	case string(experienceLearningToolActionRecordFollowUp):
		return experienceLearningToolActionRecordFollowUp
	case string(experienceLearningToolActionRecordReview):
		return experienceLearningToolActionRecordReview
	case string(experienceLearningToolActionRecordDraftReview):
		return experienceLearningToolActionRecordDraftReview
	case string(experienceLearningToolActionRecordBlockedSkillDraft), "resolve_blocked_skill_draft_review":
		return experienceLearningToolActionRecordBlockedSkillDraft
	default:
		return experienceLearningToolActionUnknown
	}
}
