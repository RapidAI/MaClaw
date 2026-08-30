package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

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
	TruncationBlockedReminders int
	NativePDFFallbackInjected  bool

	// MissFloorToolsUnlock asks the next round prep to union the invariant-11
	// floor tools (bash/write_file) back into the request surface. Set when the
	// current surface proved to be mis-scoped for this turn: either the model
	// promised a file deliverable on an ambient-only surface (deliverable
	// recover), or a floor tool call was policy_rejected because the surface
	// lacked it. The unlock is evidence-driven (runtime model behavior), never
	// a lexical routing decision.
	MissFloorToolsUnlock bool
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
	// Picker paths and host notes are not the user's request. A file named
	// weather-report.jpg would otherwise trip "report"/"pdf" skill hints.
	userText = semanticUserIntentText(userText)
	// Workflow agent loops have their execution method defined by phase instructions;
	// skill preference must not override them (the system-generated phase text often
	// contains trigger words like "文档" that would incorrectly activate skill search).
	if ctx != nil && ctx.WorkflowAgentLoop {
		return phase
	}
	if ctx != nil && ctx.Platform == "ve_group_executor" {
		return phase
	}
	ownerID := agentGuidedSkillOwnerID(ctx, "")
	exec := h.getSkillExecutor()
	skillName, skillReason, skillMode := matchPreferredLocalSkillMode(exec, userText, h.skillExperienceDomainForOwner(ownerID))
	taskWantsSkill := shouldPreferSkillForTask(userText)
	apply := func(name, reason string, mode skillPreferenceMode) agentLoopPhase {
		phase.ForceSkillPreference = true
		phase.PreferredSkillName = name
		phase.PreferredSkillReason = reason
		phase.SkillMode = mode
		if mode == skillPreferenceAgentGuided {
			h.rememberAgentGuidedSkill(ownerID, name)
		} else {
			h.forgetAgentGuidedSkill(ownerID)
		}
		return phase
	}
	if ctx != nil && ctx.IsAskUserResponse {
		if sticky := h.recallAgentGuidedSkill(ownerID, exec); sticky != "" {
			return apply(sticky, "continue named agent-guided workflow", skillPreferenceAgentGuided)
		}
		return phase
	}
	// Agent-guided identity is the execution plan even without a pdf/报告 hint.
	// A runnable skill name mentioned in passing must not steal the turn and
	// strip bash (local_only blocks host tools).
	if skillName != "" && skillMode == skillPreferenceAgentGuided {
		return apply(skillName, skillReason, skillMode)
	}
	if skillName != "" && taskWantsSkill {
		return apply(skillName, skillReason, skillMode)
	}
	if intent.ExplicitSkillInvocation(userText) && skillName == "" {
		h.forgetAgentGuidedSkill(ownerID)
		phase.ForceSkillPreference = true
		phase.SkillMode = skillPreferenceRemoteRequired
		return phase
	}
	if sticky := h.recallAgentGuidedSkill(ownerID, exec); sticky != "" && looksLikeAgentGuidedContinuation(userText) {
		return apply(sticky, "continue named agent-guided workflow", skillPreferenceAgentGuided)
	}
	if !taskWantsSkill {
		return phase
	}
	h.forgetAgentGuidedSkill(ownerID)
	phase.ForceSkillPreference = true
	phase.SkillMode = skillPreferenceRemoteRequired
	return phase
}
