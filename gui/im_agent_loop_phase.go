package main

import "strings"

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
	NoToolActionPrompted      bool
	LocalInfoRecallPrompted   bool

	FailedSkillName  string
	FailedSkillError string

	ToolHallucinationCorrected bool
	LengthContinuations        int
	TruncationRetries          int
	EssentialTruncationHints   int
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
	// Workflow agent loops have their execution method defined by phase instructions;
	// skill preference must not override them (the system-generated phase text often
	// contains trigger words like "文档" that would incorrectly activate skill search).
	if ctx != nil && ctx.WorkflowAgentLoop {
		return phase
	}
	if ctx != nil && ctx.Platform == "ve_group_executor" {
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
