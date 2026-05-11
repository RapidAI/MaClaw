package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopCodingGateResult struct {
	Conversation []interface{}
	History      []agent.ConversationEntry
	Response     *IMAgentResponse
	Handled      bool
	ContinueLoop bool
}

func (h *IMMessageHandler) prepareAgentLoopCodingGate(userID, userText string, ctx *LoopContext, milestoneTracker *progress.AgentProgressTracker) (codingToolGateConfig, bool, func() bool) {
	var gic *GateIntentClassifier
	if h.app != nil {
		gic = h.getGateIntentClassifier()
	}
	gateConfig := newCodingToolGateConfigWithClassifier(userText, ctx.Kind, gic, h.getUnifiedClassifier(), h.lastUserID)
	workflowOff := h.app != nil && h.app.workflowDisabled.Load()
	if workflowOff {
		gateConfig.active = false
	}
	if gateConfig.intent.IsKnown() && milestoneTracker != nil {
		milestoneTracker.RefineIntent(string(gateConfig.intent))
	}
	orchestratorActive := func() bool {
		if h.taskOrchestratorRegistry == nil {
			return false
		}
		o := h.taskOrchestratorRegistry.Get(userID)
		return o != nil && o.IsActive()
	}
	return gateConfig, workflowOff, orchestratorActive
}

func (h *IMMessageHandler) applyInitialCodingToolGate(tools []map[string]interface{}, gateConfig codingToolGateConfig, skipCodingGate bool, orchestratorActive func() bool) ([]map[string]interface{}, int) {
	if skipCodingGate && gateConfig.active {
		log.Printf("[coding-gate] bypassed: SkipNeedsConfirmGate=true intent=%v (not coding)", gateConfig.intent)
	}
	if gateConfig.active && !skipCodingGate && !orchestratorActive() {
		browserBeforeGate := len(browserDiagExtractNames(tools))
		filtered := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			name := tool.ExtractToolName(t)
			if !codingToolBlocklist[name] || deliveryToolAllowlist[name] {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) < len(tools) {
			log.Printf("[coding-gate] filtered %d blocked tool definitions from tool list", len(tools)-len(filtered))
			tools = filtered
		}
		BrowserDiagCP3_CodingGate(browserBeforeGate, tools, true, "")
		return tools, estimateToolsTokens(tools)
	}

	browserInTools := browserDiagExtractNames(tools)
	if len(browserInTools) > 0 {
		skipReason := ""
		if !gateConfig.active {
			skipReason = "gate_inactive"
		} else if skipCodingGate {
			skipReason = fmt.Sprintf("skipCodingGate(intent=%v)", gateConfig.intent)
		} else if orchestratorActive() {
			skipReason = "orchestrator_active"
		}
		BrowserDiagCP3_CodingGate(len(browserInTools), tools, false, skipReason)
	}
	return tools, estimateToolsTokens(tools)
}

func (h *IMMessageHandler) applyAgentLoopCodingGateAfterAssistantTurn(
	ctx *LoopContext,
	userID string,
	iteration int,
	platform string,
	gateConfig codingToolGateConfig,
	skipCodingGate bool,
	orchestratorActive func() bool,
	choice *llm.Choice,
	assistantMsg map[string]interface{},
	conversation []interface{},
	history []agent.ConversationEntry,
	msgContent string,
	msgReasoning string,
	phase *agentLoopPhase,
	steeringDetector *SteeringWorkflowDetector,
	recordSystemMessages func(int, []interface{}),
	attachLLMTelemetry func(*IMAgentResponse),
	attachPendingVisibleArtifacts func(*IMAgentResponse),
) agentLoopCodingGateResult {
	result := agentLoopCodingGateResult{Conversation: conversation, History: history}
	if choice == nil {
		return result
	}
	if !(gateConfig.active && !skipCodingGate && !orchestratorActive() && len(choice.Message.ToolCalls) > 0) {
		if !gateConfig.active && iteration == 0 && len(choice.Message.ToolCalls) > 0 {
			log.Printf("[coding-gate] DEBUG: gate inactive: %s", gateConfig.reason)
		}
		return result
	}

	gateResult := applyCodingToolGate(choice.Message.ToolCalls)
	if !gateResult.applied {
		return result
	}
	strippedNames := make([]string, 0, len(gateResult.stripped))
	for _, tc := range gateResult.stripped {
		strippedNames = append(strippedNames, tc.Function.Name)
	}
	preservedNames := make([]string, 0, len(gateResult.remaining))
	for _, tc := range gateResult.remaining {
		preservedNames = append(preservedNames, tc.Function.Name)
	}
	log.Printf("[coding-gate] activated (iter=%d): stripped=%v preserved=%v reason=%s", iteration, strippedNames, preservedNames, gateConfig.reason)
	if h.traceService != nil && ctx.RunID != "" {
		h.appendTraceEvent(ctx, "gate.coding_tool_stripped", "warn",
			"Coding tool gate stripped tools",
			fmt.Sprintf("iteration=%d stripped=%v preserved=%v", iteration, strippedNames, preservedNames), "", "")
	}

	choice.Message.ToolCalls = gateResult.remaining
	syncAssistantTurnToolCalls(assistantMsg, conversation, history, msgContent, msgReasoning, gateResult.remaining)

	if len(gateResult.remaining) > 0 {
		result.Conversation = conversation
		result.History = history
		return result
	}
	result.Handled = true
	if iteration == 0 && strings.TrimSpace(msgContent) == "" {
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": "[Coding workflow] Generate the requirements document first and wait for user confirmation before coding.",
		})
		recordSystemMessages(systemMessagesStart, conversation)
		result.Conversation = conversation
		result.History = history
		result.ContinueLoop = true
		return result
	}
	if strings.TrimSpace(msgContent) != "" {
		log.Printf("[coding-gate] iter=%d: coding tools stripped after doc output, force-returning for user confirmation", iteration)
		if phase != nil {
			phase.Stage = agentStageFinalize
		}
		finalResp := &IMAgentResponse{Text: stripThinkingTags(msgContent)}
		h.emitCodingGateForceReturnDocUpdate(userID, platform, finalResp.Text, steeringDetector)
		attachLLMTelemetry(finalResp)
		attachPendingVisibleArtifacts(finalResp)
		h.saveConversationHistoryTimed(userID, history, finalResp)
		result.Response = finalResp
		result.Conversation = conversation
		result.History = history
		return result
	}

	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": "[Coding workflow] Coding tools were blocked. Complete the requirements document and wait for user confirmation before using coding tools.",
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	result.History = history
	result.ContinueLoop = true
	return result
}

func syncAssistantTurnToolCalls(assistantMsg map[string]interface{}, conversation []interface{}, history []agent.ConversationEntry, msgContent, msgReasoning string, remaining []llm.ToolCall) {
	if len(remaining) == 0 {
		delete(assistantMsg, "tool_calls")
	} else {
		assistantMsg["tool_calls"] = remaining
	}
	if len(conversation) > 0 {
		if m, ok := conversation[len(conversation)-1].(map[string]interface{}); ok {
			if len(remaining) == 0 {
				delete(m, "tool_calls")
			} else {
				m["tool_calls"] = remaining
			}
		}
	}
	if len(history) == 0 {
		return
	}
	if len(remaining) == 0 {
		history[len(history)-1] = agent.ConversationEntry{Role: "assistant", Content: msgContent, ReasoningContent: msgReasoning}
		return
	}
	entry := history[len(history)-1]
	entry.ToolCalls = remaining
	history[len(history)-1] = entry
}

func (h *IMMessageHandler) emitCodingGateForceReturnDocUpdate(userID, platform, strippedContent string, steeringDetector *SteeringWorkflowDetector) {
	if !normalizeIMMessagePlatformKind(platform).IsDesktop() || h.getWorkflowEngine() == nil {
		return
	}
	trimmedStripped := strings.TrimSpace(strippedContent)
	emitted := false
	if steeringDetector != nil {
		steeringDetector.interceptTextOutput(trimmedStripped, func(phaseID, content string) {
			if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
				_ = adapter.EmitDocUpdate(userID, phaseID, content)
				log.Printf("[SteeringWorkflow] emitted doc_update from gate force-return for user=%s phase=%s len=%d", userID, phaseID, len(content))
				emitted = true
			}
		})
	}
	if emitted || len(trimmedStripped) < 50 {
		return
	}
	if ws := h.getWorkflowEngine().GetActiveWorkflow(userID); ws != nil {
		if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
			_ = adapter.EmitDocUpdate(userID, ws.CurrentPhase, trimmedStripped)
			log.Printf("[WorkflowEngine] emitted doc_update from gate force-return for user=%s phase=%s len=%d", userID, ws.CurrentPhase, len(trimmedStripped))
		}
	}
}

func shouldSkipCodingGate(ctx *LoopContext, gateConfig codingToolGateConfig) bool {
	return ctx.SkipNeedsConfirmGate && gateConfig.intent != intentCoding
}

func (h *IMMessageHandler) restoreToolsAfterSkillRecover(userID string, baseTools []map[string]interface{}, phase agentLoopPhase, gateConfig codingToolGateConfig, skipCodingGate bool, orchestratorActive func() bool) ([]map[string]interface{}, int, bool) {
	tools := baseTools
	directModeToolsFiltered := false
	if gateConfig.active && !skipCodingGate && !orchestratorActive() {
		gateFiltered := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			name := tool.ExtractToolName(t)
			if !codingToolBlocklist[name] || deliveryToolAllowlist[name] {
				gateFiltered = append(gateFiltered, t)
			}
		}
		tools = gateFiltered
	}
	if orchestratorActive() {
		orchInst := h.taskOrchestratorRegistry.Get(userID)
		if orchInst != nil && orchInst.CurrentExecutionMode() == TaskExecModeDirect {
			tools = filterDirectModeAllowedTools(tools)
			directModeToolsFiltered = true
		}
	}
	if len(phase.TruncationBlockedTools) > 0 {
		var truncFiltered []map[string]interface{}
		for _, t := range tools {
			name := tool.ExtractToolName(t)
			if !phase.TruncationBlockedTools[name] {
				truncFiltered = append(truncFiltered, t)
			}
		}
		if len(truncFiltered) < len(tools) {
			log.Printf("[agent-loop] re-applied truncation block after baseTools reset: removed %d tools", len(tools)-len(truncFiltered))
			tools = truncFiltered
		}
	}
	return tools, estimateToolsTokens(tools), directModeToolsFiltered
}

func filterDirectModeAllowedTools(tools []map[string]interface{}) []map[string]interface{} {
	var filtered []map[string]interface{}
	for _, t := range tools {
		name := tool.ExtractToolName(t)
		if !isDirectModeBlockedTool(name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (h *IMMessageHandler) activateSteeringWorkflowDetector(userID, userText, platform string, ctx *LoopContext, gateConfig codingToolGateConfig, workflowOff bool) *SteeringWorkflowDetector {
	if ctx.Kind == LoopKindBackground {
		return nil
	}
	detector := NewSteeringWorkflowDetector(userID)
	shouldActivate := gateConfig.active || h.conversationHasCodingContext()
	if workflowOff {
		shouldActivate = false
	}
	if !shouldActivate {
		log.Printf("[SteeringWorkflow] detector NOT activated: shouldActivate=false gateActive=%v gateReason=%q user=%s", gateConfig.active, gateConfig.reason, userID)
		return nil
	}
	hasEngineWorkflow := false
	if h.getWorkflowEngine() != nil {
		hasEngineWorkflow = h.getWorkflowEngine().HasActiveWorkflow(userID)
	}
	if hasEngineWorkflow {
		log.Printf("[SteeringWorkflow] detector NOT activated: engine has active workflow for user=%s gateActive=%v", userID, gateConfig.active)
		return nil
	}
	log.Printf("[SteeringWorkflow] detector activated for user=%s task=%q gateActive=%v", userID, truncateRunes(userText, 60), gateConfig.active)
	if gateConfig.active && normalizeIMMessagePlatformKind(platform).IsDesktop() && h.getWorkflowEngine() != nil {
		if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
			if adapter.GetWorkingDir() == "" {
				projectPath := strings.TrimSpace(h.getCurrentProjectPath())
				if projectPath != "" {
					adapter.SetWorkingDir(userID, projectPath)
				}
			}
			adapter.EmitSuggestMaximize(userID, "coding")
			detector.suggestMaximizeEmitted = true
			log.Printf("[SteeringWorkflow] emitted early suggest_maximize for desktop user=%s (classifier confirmed new_project)", userID)
		}
	}
	return detector
}
