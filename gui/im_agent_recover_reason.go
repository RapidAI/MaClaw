package main

type agentRecoverReason string

const (
	agentRecoverNone                  agentRecoverReason = ""
	agentRecoverSkillFailed           agentRecoverReason = "skill_failed"
	agentRecoverTrialFailed           agentRecoverReason = "trial_failed"
	agentRecoverDriftDetected         agentRecoverReason = "drift_detected"
	agentRecoverPendingSkillRunNoTool agentRecoverReason = "pending_skill_run_no_tool"
	agentRecoverNoToolStall           agentRecoverReason = "no_tool_stall"
	agentRecoverEmptyFinalResponse    agentRecoverReason = "empty_final_response"
	agentRecoverDeliverablePending    agentRecoverReason = "deliverable_pending"
)

func (r agentRecoverReason) String() string {
	return string(r)
}
