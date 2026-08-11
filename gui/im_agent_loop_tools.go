package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
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
	profile := ExecutionProfile{}
	requestID := ""
	if ctx != nil {
		profile = ctx.Runtime.Execution
		requestID = ctx.Runtime.RequestID
	}
	// ACP Mode B: skip route-intent rewrite + full UIC (often 5s+ tree timeout).
	skipHeavy := isACPProgrammingRequestID(requestID)
	baseTools := h.routeToolsForUser(userID, userText, allTools, skipHeavy)
	tools := baseTools

	var browserSessionPinned bool
	if h.toolRouter != nil {
		browserSessionPinned = h.toolRouter.IsSessionPinnedForSession(userID, "browser")
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
	if isIMManagementRequest(userText) {
		// Explicit delivery intent stays authoritative after light-profile
		// filtering; otherwise send_to_im/list_targets can disappear here.
		tools = ensureIMManagementToolsRouted(tools, allTools, userText)
	}
	if profile.IsLight() || isLightPromptProfile(profile.PromptProfile) {
		tool.WriteToolExposureLog("execution_profile", userText, requestID, userID, profile.Layer, profile.TaskType, beforeProfileFilter, agentLoopToolNamesForLog(tools))
		log.Printf("[exec-profile] layer=%s prompt=%s task=%s request_id=%q user=%q confidence=%.2f reason=%q tool_budget=%d iteration_budget=%d routed_before=%d routed_after=%d tools=%q",
			profile.Layer, profile.PromptProfile, profile.TaskType, requestID, userID, profile.Confidence, profile.Reason, profile.ToolBudget, profile.IterationBudget, beforeProfileFilter, len(tools), executionProfileToolNames(tools))
	}

	// Computer Use: when intent/session active, force computer_* tools and
	// demote raw gui_click/type (text-primary OmniParser path).
	cuActive, cuFresh := h.gateComputerUse(userText)
	if cuActive {
		if cuFresh {
			// New desktop task: lift a stale hard-stop so the injected tools are
			// not blocked by a previous Stop. Guarded per request — a stopped
			// in-flight turn re-gating while cancel lags keeps its Stop.
			liftComputerUseStopForFreshRequest(requestID)
		}
		tools = ensureComputerUseTools(tools, allTools, true)
	} else if localFileWorkBlocksComputerUse(userText) {
		// The regular intent router can return a broad list while an attachment
		// is being parsed. Keep Computer Use unavailable on this turn even when
		// the generic route happens to include its definitions; otherwise their
		// presence alone can lure the model into opening a terminal or Explorer.
		tools = removeComputerUseTools(tools)
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

	// Expert session: apply the expert's tool allow-list last so no pipeline
	// stage above can re-introduce tools outside the whitelist.
	tools = h.filterToolsForExpertUser(userID, tools)
	if ctx != nil && ctx.LansengerGroupPermissions != nil {
		// Routing may choose only a network tool for a general support question.
		// Restore the group-safe retrieval primitives from the complete catalog
		// before applying the allow-list. An expert's explicit allow-list remains
		// authoritative and is never bypassed here.
		tools = h.ensureLansengerGroupMemoryRecallTool(userID, tools)
		if ctx.LansengerGroupPermissions.allowsKnowledge() {
			tools = h.ensureLansengerGroupKnowledgeSearchTool(userID, tools)
		}
		tools = filterToolsForLansengerGroupPermissions(tools, *ctx.LansengerGroupPermissions)
	}
	// Large tool previews carry a lossless [tool_result_handle]. Keep its
	// companion reader available through light, skill, workflow and recovery
	// filters whenever the host policy permits it. This adds no user-facing step;
	// the model calls it only when omitted details are actually needed.
	readerOwnerID := userID
	if strings.TrimSpace(policyOwnerID) != "" {
		readerOwnerID = policyOwnerID
	}
	tools = h.ensureToolResultReader(readerOwnerID, tools, allTools)
	// Client-implemented tools are scoped to the originating turn and appended
	// after host routing/policy filters. They never enter the global registry and
	// therefore cannot leak to another client or be dropped by intent routing.
	tools = appendClientToolsForAgent(tools, ctx)

	// ESP32 hardware replies are synthesized automatically by the gateway after
	// the terminal text has reached the result page. Exposing tts here lets the
	// model play speech early and then add narration such as "播报完毕" to its
	// final answer. Remove both host- and client-declared variants, including the
	// recovery catalog used by later loop iterations.
	platform := ""
	if ctx != nil {
		platform = ctx.Platform
	}
	tools = filterToolsForHardwareAutoSpeech(tools, platform)
	baseTools = filterToolsForHardwareAutoSpeech(baseTools, platform)
	// Keep the local-file fence last: client-declared tools and future pipeline
	// stages above must not be able to reintroduce computer_* after routing has
	// correctly selected local document handling.
	tools = filterComputerUseToolsForLocalFileWork(ctx, userText, tools)

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

// removeComputerUseTools removes every desktop-automation definition for a
// local-file turn. Besides the ref-based computer_* surface, the legacy gui_*
// tools can click/type/screenshot the host directly; leaving those advertised
// gives a model the same confusing Explorer/terminal detour through a different
// tool family. Keep this classifier shared with the execution gate so a stale
// or hallucinated legacy call is rejected before it can touch the desktop.
func removeComputerUseTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if isComputerUseToolDefinition(extractToolName(def)) {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

func isComputerUseToolDefinition(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return computeruse.IsComputerUseTool(name) ||
		strings.HasPrefix(name, "computer_") ||
		strings.HasPrefix(name, "gui_")
}

func localFileWorkBlocksComputerUse(userText string) bool {
	return hasCurrentLocalFileWork(userText) && !hasExplicitComputerUseRequest(userText)
}

// filterComputerUseToolsForLocalFileWork is the final defense for every path
// that composes a tool list. The context flag carries the decision after the
// initial turn text has been replaced by a steering or recovery message.
func filterComputerUseToolsForLocalFileWork(ctx *LoopContext, userText string, tools []map[string]interface{}) []map[string]interface{} {
	if localFileWorkBlocksComputerUseExecution(ctx, userText, "computer_observe") {
		return removeComputerUseTools(tools)
	}
	return tools
}

// localFileWorkBlocksComputerUseExecution is the enforcement predicate shared
// by prompt routing and tool execution. A context fence is authoritative for
// the active turn; the initial router only sets it when the original request
// lacked explicit Computer Use intent, so a later injection cannot silently
// change the original turn's permission.
func localFileWorkBlocksComputerUseExecution(ctx *LoopContext, userText, toolName string) bool {
	if !isComputerUseToolDefinition(toolName) {
		return false
	}
	if ctx != nil && ctx.ComputerUseBlockedForLocalFileWork {
		return true
	}
	if localFileWorkBlocksComputerUse(userText) {
		return true
	}
	return false
}

func filterToolsForHardwareAutoSpeech(tools []map[string]interface{}, platform string) []map[string]interface{} {
	if !isThirdPartyVoicePlatform(platform) || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.ExtractToolName(def)), "tts") {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

func appendClientToolsForAgent(tools []map[string]interface{}, ctx *LoopContext) []map[string]interface{} {
	if ctx == nil || ctx.ClientToolContext == nil || len(ctx.ClientTools) == 0 {
		return tools
	}
	seen := make(map[string]bool, len(tools))
	for _, def := range tools {
		if name := tool.ExtractToolName(def); name != "" {
			seen[name] = true
		}
	}
	for _, clientTool := range ctx.ClientTools {
		name := strings.TrimSpace(clientTool.Name)
		if name == "" || seen[name] {
			continue // host tools always win a name collision
		}
		parameters := cloneClientToolSchema(clientTool.InputSchema)
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": name, "description": strings.TrimSpace(clientTool.Description), "parameters": parameters,
			},
		})
		seen[name] = true
	}
	return tools
}

func cloneClientToolSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	copySchema := make(map[string]any, len(schema))
	for key, value := range schema {
		copySchema[key] = value
	}
	if _, ok := copySchema["type"]; !ok {
		copySchema["type"] = "object"
	}
	return copySchema
}

func clientToolForLoop(ctx *LoopContext, name string) (agent.ClientToolDefinition, bool) {
	if ctx == nil || ctx.ClientToolContext == nil {
		return agent.ClientToolDefinition{}, false
	}
	for _, candidate := range ctx.ClientTools {
		if strings.TrimSpace(candidate.Name) == strings.TrimSpace(name) {
			return candidate, true
		}
	}
	return agent.ClientToolDefinition{}, false
}

func containsAgentLoopToolNamed(tools []map[string]interface{}, name string) bool {
	for _, def := range tools {
		if tool.ExtractToolName(def) == name {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) ensureToolResultReader(userID string, tools, allTools []map[string]interface{}) []map[string]interface{} {
	const name = "read_tool_result"
	if h == nil || len(tools) == 0 || containsAgentLoopToolNamed(tools, name) || expertDefForUserID(userID) != nil {
		return tools
	}
	if !h.isWorkflowToolAllowedForOwner(userID, name) {
		return tools
	}
	for _, def := range allTools {
		if tool.ExtractToolName(def) == name {
			return append(tools, def)
		}
	}
	if h.registry != nil {
		if registered, ok := h.registry.Get(name); ok && registered != nil {
			return append(tools, registeredToolToDef(*registered))
		}
	}
	return tools
}

// ensureLansengerGroupMemoryRecallTool keeps the group-scoped memory recall
// primitive visible even when intent routing narrows a support query to a
// web-only subset. The later group filter replaces its schema with the
// recall-only view, and execution independently enforces the same restriction.
func (h *IMMessageHandler) ensureLansengerGroupMemoryRecallTool(userID string, tools []map[string]interface{}) []map[string]interface{} {
	if h == nil || containsAgentLoopToolNamed(tools, "memory") || expertDefForUserID(userID) != nil {
		return tools
	}
	for _, def := range h.getTools() {
		if tool.ExtractToolName(def) == "memory" {
			return append(tools, def)
		}
	}
	if h.registry != nil {
		if registered, ok := h.registry.Get("memory"); ok && registered != nil {
			return append(tools, registeredToolToDef(*registered))
		}
	}
	return tools
}

// ensureLansengerGroupKnowledgeSearchTool keeps the authorised retrieval
// primitive visible even when routing or later tool augmentation narrowed the
// list to a web-only subset. A configured expert allow-list still wins.
func (h *IMMessageHandler) ensureLansengerGroupKnowledgeSearchTool(userID string, tools []map[string]interface{}) []map[string]interface{} {
	if h == nil || containsAgentLoopToolNamed(tools, "knowledge_search") || expertDefForUserID(userID) != nil {
		return tools
	}
	for _, def := range h.getTools() {
		if tool.ExtractToolName(def) == "knowledge_search" {
			return append(tools, def)
		}
	}
	if h != nil && h.registry != nil {
		if registered, ok := h.registry.Get("knowledge_search"); ok && registered != nil {
			return append(tools, registeredToolToDef(*registered))
		}
	}
	return tools
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
			if policy := h.workflowReviewPhaseToolFilter(policyOwnerID); policy == v2.ToolFilterPlanning {
				return policyOwnerID, policy, true
			}
			return policyOwnerID, v2.ToolFilterDocOnly, true
		}
		return policyOwnerID, v2.ToolFilterNone, true
	}
	if h.shouldConstrainCodingWorkflowImplementationMainLoop(policyOwnerID) {
		return policyOwnerID, v2.ToolFilterFull, true
	}
	if h != nil && h.app != nil && h.app.workflowEngine != nil {
		if policy := h.app.workflowEngine.GetActivePhaseToolFilter(policyOwnerID); policy != v2.ToolFilterNone {
			return policyOwnerID, policy, true
		}
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
	return policyOwnerID, v2.ToolFilterNone, false
}

func (h *IMMessageHandler) workflowReviewPhaseToolFilter(userID string) v2.ToolFilterPolicy {
	if h == nil || h.app == nil || h.app.workflowEngine == nil {
		return v2.ToolFilterNone
	}
	ws := h.app.workflowEngine.GetActiveWorkflow(userID)
	if ws == nil || ws.PendingReviewPhaseID == "" {
		return v2.ToolFilterNone
	}
	registry := h.app.workflowEngine.GetRegistry()
	if registry == nil {
		return v2.ToolFilterNone
	}
	tmpl := registry.Match(ws.Type)
	if tmpl == nil {
		return v2.ToolFilterNone
	}
	for _, phase := range tmpl.Phases {
		if phase.ID == ws.PendingReviewPhaseID {
			if ws.Type == v2.WorkflowCoding && phase.ID == v2.PhaseCodingTaskBreakdown {
				return v2.ToolFilterPlanning
			}
			return phase.ToolPolicy
		}
	}
	return v2.ToolFilterNone
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
		return []string{"bash", "read_file", "list_directory", "send_file", "send_to_im"}
	case v2.ToolFilterPlanning:
		return []string{"read_file", "list_directory", "write_file", "send_file", "send_to_im"}
	case v2.ToolFilterOpsControlled:
		return []string{"read_file", "list_directory", "send_file", "send_to_im", "bash", "ssh"}
	case v2.ToolFilterFull:
		return []string{"read_file", "list_directory", "send_file", "send_to_im", "bash", "write_file", "edit_file", "task"}
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
