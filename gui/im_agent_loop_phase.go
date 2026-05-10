package main

import "strings"

type agentLoopStage string

type skillPreferenceMode string

type agentRecoverReason string

const (
	agentStageOrient   agentLoopStage = "orient"
	agentStageExecute  agentLoopStage = "execute"
	agentStageRecover  agentLoopStage = "recover"
	agentStageConverge agentLoopStage = "converge"
	agentStageFinalize agentLoopStage = "finalize"
)

const (
	skillPreferenceNone            skillPreferenceMode = "none"
	skillPreferenceLocalOnly       skillPreferenceMode = "local_only"
	skillPreferenceRemoteRequired  skillPreferenceMode = "remote_required"
	skillPreferenceFallbackAllowed skillPreferenceMode = "fallback_allowed"
)

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

type agentLoopPhase struct {
	Stage                     agentLoopStage
	ConsecutiveNoTool         int
	ConsecutiveEmptyResponses int
	TotalRecoverInjections    int
	DeliverableRecoverCount   int
	ForceSkillPreference      bool
	SkillMode                 skillPreferenceMode
	PreferredSkillName        string
	PreferredSkillReason      string
	PreferredSkillRunID       string
	SkillAttempted            bool
	SkillFailed               bool
	RemoteSearchAttempted     bool
	RemoteSearchExhausted     bool
	RecoverReason             agentRecoverReason
	RecoverPrompt             string

	FailedSkillName  string
	FailedSkillError string

	ToolHallucinationCorrected bool
	LengthContinuations        int
	TruncationRetries          int
	TruncationBlockedTools     map[string]bool
}

func enterRecoverPhase(phase *agentLoopPhase, reason agentRecoverReason, prompt string) {
	if phase == nil {
		return
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	phase.Stage = agentStageRecover
	phase.RecoverReason = reason
	phase.RecoverPrompt = prompt
	phase.TotalRecoverInjections++
}

func (h *IMMessageHandler) initialAgentLoopPhase(userText string, ctx *LoopContext) agentLoopPhase {
	phase := agentLoopPhase{Stage: agentStageOrient, SkillMode: skillPreferenceNone}
	if ctx != nil && ctx.IsAskUserResponse {
		return phase
	}
	if !shouldPreferSkillForTask(userText) {
		return phase
	}
	phase.ForceSkillPreference = true
	phase.SkillMode = skillPreferenceRemoteRequired
	if skillName, skillReason := matchPreferredLocalSkill(h.getSkillExecutor(), userText); skillName != "" {
		phase.SkillMode = skillPreferenceLocalOnly
		phase.PreferredSkillName = skillName
		phase.PreferredSkillReason = skillReason
	}
	return phase
}
