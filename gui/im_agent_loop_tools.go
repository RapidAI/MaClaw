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
	if engine := h.getWorkflowEngine(); engine != nil && !ctx.SkipNeedsConfirmGate {
		if p := engine.GetPhaseToolFilter(userID); p != "" {
			workflowFilterPolicy = workflowToolFilterDecision(p)
		}
		tools = h.applyWorkflowToolFilter(userID, tools)
	} else if ctx.SkipNeedsConfirmGate {
		workflowFilterPolicy = workflowToolFilterSkippedConfirmBypass
	}
	BrowserDiagCP2_WorkflowFilter(browserBeforeWF, tools, workflowFilterPolicy.String(), ctx.SkipNeedsConfirmGate)

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
