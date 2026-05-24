package main

import "time"

type agentLoopToolSet struct {
	Tools            []map[string]interface{}
	BaseTools        []map[string]interface{}
	ToolsTokenBudget int
	PreparationTime  time.Duration
	WorkflowDecision workflowToolFilterDecision
	BrowserBeforeWF  int
	BrowserPinned    bool
}

func (h *IMMessageHandler) prepareAgentLoopTools(userID, userText string, ctx *LoopContext, phase agentLoopPhase) agentLoopToolSet {
	startedAt := time.Now()
	allTools := h.getTools()
	baseTools := h.routeTools(userText, allTools)
	tools := baseTools

	var browserSessionPinned bool
	if h.toolRouter != nil {
		browserSessionPinned = h.toolRouter.IsSessionPinned("browser")
	}
	BrowserDiagCP1_Route(userText, tools, browserSessionPinned)

	if phase.ForceSkillPreference {
		if shouldRestrictToSkillSearch(phase) {
			tools = filterToolsForRemoteSkillSearch(baseTools)
		} else {
			tools = filterToolsForSkillPreference(baseTools)
		}
	}

	browserBeforeWF := len(browserDiagExtractNames(tools))
	workflowFilterPolicy := workflowToolFilterNone
	workflowFilterSkipped := false
	skipNeedsConfirmGate := ctx != nil && ctx.SkipNeedsConfirmGate
	workflowAgentLoop := ctx != nil && ctx.WorkflowAgentLoop
	if engine := h.getWorkflowEngine(); engine != nil && shouldApplyWorkflowFilter(skipNeedsConfirmGate, engine.IsAwaitingReview(userID), workflowAgentLoop, engine.IsPhaseExecutionBlocked(userID)) {
		if p := engine.GetActivePhaseToolFilter(userID); p != "" {
			workflowFilterPolicy = workflowToolFilterDecision(p)
		}
		tools = h.applyWorkflowToolFilter(userID, tools)
	} else if skipNeedsConfirmGate {
		workflowFilterPolicy = workflowToolFilterSkippedConfirmBypass
		workflowFilterSkipped = true
	}
	BrowserDiagCP2_WorkflowFilter(browserBeforeWF, tools, workflowFilterPolicy.String(), workflowFilterSkipped)

	return agentLoopToolSet{
		Tools:            tools,
		BaseTools:        baseTools,
		ToolsTokenBudget: estimateToolsTokens(tools),
		PreparationTime:  time.Since(startedAt),
		WorkflowDecision: workflowFilterPolicy,
		BrowserBeforeWF:  browserBeforeWF,
		BrowserPinned:    browserSessionPinned,
	}
}
