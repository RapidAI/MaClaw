package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

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
	profile := ExecutionProfile{}
	requestID := ""
	if ctx != nil {
		profile = ctx.Runtime.Execution
		requestID = ctx.Runtime.RequestID
	}

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
	beforeProfileFilter := len(tools)
	tools = filterToolsForExecutionProfile(tools, profile)
	if profile.IsLight() {
		tool.WriteToolExposureLog("execution_profile", userText, requestID, userID, profile.Layer, profile.TaskType, beforeProfileFilter, agentLoopToolNamesForLog(tools))
		log.Printf("[exec-profile] layer=%s task=%s request_id=%q user=%q confidence=%.2f reason=%q tool_budget=%d iteration_budget=%d routed_before=%d routed_after=%d tools=%q",
			profile.Layer, profile.TaskType, requestID, userID, profile.Confidence, profile.Reason, profile.ToolBudget, profile.IterationBudget, beforeProfileFilter, len(tools), executionProfileToolNames(tools))
	}

	browserBeforeWF := len(browserDiagExtractNames(tools))
	beforeWorkflowFilter := len(tools)
	workflowFilterPolicy := workflowToolFilterNone
	workflowFilterSkipped := false
	skipNeedsConfirmGate := ctx != nil && ctx.SkipNeedsConfirmGate
	policyOwnerID, policy, applyWorkflowFilter := h.workflowToolFilterOwnerPolicyAndDecision(userID, ctx)
	if applyWorkflowFilter {
		workflowFilterPolicy = workflowToolFilterDecision(string(policy))
		if policy == v2.ToolFilterNone {
			tools = nil
		} else {
			tools = h.applyWorkflowToolFilterWithCatalog(policyOwnerID, tools, allTools)
		}
	} else if skipNeedsConfirmGate {
		workflowFilterPolicy = workflowToolFilterSkippedConfirmBypass
		workflowFilterSkipped = true
	}
	if profile.IsLight() || workflowFilterPolicy != workflowToolFilterNone {
		tool.WriteToolExposureLog("workflow_filter", userText, requestID, userID, profile.Layer, profile.TaskType, beforeWorkflowFilter, agentLoopToolNamesForLog(tools))
	}
	BrowserDiagCP2_WorkflowFilter(browserBeforeWF, tools, workflowFilterPolicy.String(), workflowFilterSkipped)

	toolsForLLM := stripExecutionContractMetadataForLLM(tools)
	baseToolsForLLM := stripExecutionContractMetadataForLLM(baseTools)
	return agentLoopToolSet{
		Tools:            toolsForLLM,
		BaseTools:        baseToolsForLLM,
		ToolsTokenBudget: estimateToolsTokens(toolsForLLM),
		PreparationTime:  time.Since(startedAt),
		WorkflowDecision: workflowFilterPolicy,
		BrowserBeforeWF:  browserBeforeWF,
		BrowserPinned:    browserSessionPinned,
	}
}

func (h *IMMessageHandler) workflowToolFilterOwnerAndDecision(userID string, ctx *LoopContext) (string, bool) {
	ownerID, _, apply := h.workflowToolFilterOwnerPolicyAndDecision(userID, ctx)
	return ownerID, apply
}

func (h *IMMessageHandler) workflowToolFilterOwnerPolicyAndDecision(userID string, ctx *LoopContext) (string, v2.ToolFilterPolicy, bool) {
	policyOwnerID := h.workflowPolicyOwnerID(userID, ctx)
	if policyOwnerID == "" {
		policyOwnerID = h.workflowPolicyUserID(userID)
	}
	if policyOwnerID == "" {
		return policyOwnerID, v2.ToolFilterNone, false
	}
	if h != nil && h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.IsPhaseExecutionBlocked(policyOwnerID) {
		if h.app.workflowEngine.IsAwaitingReview(policyOwnerID) {
			return policyOwnerID, v2.ToolFilterDocOnly, true
		}
		return policyOwnerID, v2.ToolFilterNone, true
	}
	if h.shouldConstrainCodingWorkflowImplementationMainLoop(policyOwnerID) {
		return policyOwnerID, v2.ToolFilterFull, true
	}
	if h.isWorkflowV2Active(policyOwnerID) {
		wf := h.getWorkflowV2()
		if wf != nil {
			if state := wf.machine.GetActive(policyOwnerID); state != nil {
				phase := state.ActivePhase()
				if phase != nil {
					switch phase.ToolPolicy {
					case v2.ToolPolicyDocOnly:
						return policyOwnerID, v2.ToolFilterDocOnly, true
					}
				}
			}
		}
	}
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
		if state := wf.machine.GetActive(policyOwnerID); state != nil {
			if phase := state.ActivePhase(); phase != nil {
				policy := v2.ToolFilterPolicy(phase.ToolPolicy)
				if policy != v2.ToolFilterNone {
					return policyOwnerID, policy, true
				}
				if phase.Status == v2.PhaseWaitingConfirm {
					return policyOwnerID, v2.ToolFilterNone, true
				}
			}
		}
	}
	if h != nil && h.app != nil && h.app.workflowEngine != nil {
		if policy := h.app.workflowEngine.GetActivePhaseToolFilter(policyOwnerID); policy != v2.ToolFilterNone {
			return policyOwnerID, policy, true
		}
	}
	return policyOwnerID, v2.ToolFilterNone, false
}

func agentLoopToolNamesForLog(tools []map[string]interface{}) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, def := range tools {
		if name := tool.ExtractToolName(def); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func ensureWorkflowRequiredTools(policy interface{}, routed, allTools []map[string]interface{}) []map[string]interface{} {
	required := requiredWorkflowToolNamesForPolicy(policy)
	return ensureWorkflowRequiredToolsForNames(required, routed, allTools)
}

func requiredWorkflowToolNamesForPolicy(policy interface{}) []string {
	policyName := fmt.Sprint(policy)
	if policyName == "" || policyName == "<nil>" {
		return nil
	}
	switch v2.ToolFilterPolicy(policyName) {
	case v2.ToolFilterDocOnly:
		return []string{"read_file", "list_directory", "send_file"}
	case v2.ToolFilterOpsControlled:
		return []string{"read_file", "list_directory", "send_file", "bash", "ssh"}
	case v2.ToolFilterFull:
		return []string{"read_file", "list_directory", "send_file", "bash", "write_file", "edit_file", "task"}
	default:
		return nil
	}
}

func ensureWorkflowRequiredToolsForNames(required []string, routed, allTools []map[string]interface{}) []map[string]interface{} {
	if len(required) == 0 || len(allTools) == 0 {
		return routed
	}
	seen := make(map[string]bool, len(routed))
	for _, def := range routed {
		if name := tool.ExtractToolName(def); name != "" {
			seen[name] = true
		}
	}
	byName := make(map[string]map[string]interface{}, len(allTools))
	for _, def := range allTools {
		name := tool.ExtractToolName(def)
		if name != "" && byName[name] == nil {
			byName[name] = def
		}
	}
	merged := routed
	for _, name := range required {
		if seen[name] {
			continue
		}
		def := byName[name]
		if def == nil {
			continue
		}
		merged = append(merged, def)
		seen[name] = true
	}
	return merged
}
