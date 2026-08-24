package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

type agentLoopToolSet struct {
	Tools            []map[string]interface{}
	BaseTools        []map[string]interface{}
	LegacyPlanBacked bool
	ClientToolNames  []string
	ToolsTokenBudget int
	PreparationTime  time.Duration
	WorkflowDecision workflowToolFilterDecision
	BrowserBeforeWF  int
}

func (h *IMMessageHandler) prepareAgentLoopTools(userID, userText string, ctx *LoopContext, phase agentLoopPhase) agentLoopToolSet {
	startedAt := time.Now()
	if loopContextBlocksLegacyToolRouter(ctx) {
		requestID := ""
		if ctx != nil {
			requestID = ctx.Runtime.RequestID
		}
		log.Printf("[semantic-routing] skip name-router prepareAgentLoopTools on managed turn request_id=%q", requestID)
		return agentLoopToolSet{PreparationTime: time.Since(startedAt)}
	}
	// Dynamic gateways must be absent before candidate ranking and budgeting,
	// not merely removed from the final list. Post-route removal otherwise lets
	// a gateway consume a scarce slot and leaves the model with an incomplete
	// surface after it is stripped.
	allTools := removeLegacyModelManageSkillGateway(removeLegacyModelMCPGateway(h.getTools()))
	profile := ExecutionProfile{}
	requestID := ""
	if ctx != nil {
		profile = ctx.Runtime.Execution
		requestID = ctx.Runtime.RequestID
	}
	// Gate 7 already rewrote LoopContext to unknown. The leftover router must
	// not ClassifyEmbeddingOnly the raw typo and pin web_search at 0.50.
	// The turn's governed classification (computed upstream with full context)
	// travels with the route call, so a usable SemanticIntent activates its
	// conditional tools without any new classification call.
	var preResolved *intent.ClassificationResult
	if ctx != nil {
		preResolved = ctx.Runtime.SemanticIntent
	}
	baseTools := h.routeSessionTools(userID, userText, allTools, loopContextHasChatProjection(ctx) || loopContextHasRoutingMissFallback(ctx), preResolved)
	tools := baseTools

	BrowserDiagCP1_Route(userText, tools)

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
	tools = mergeAmbientRetrievalTools(tools, allTools)
	tools = applyRoutingMissLeftoverTools(tools, allTools, ctx)
	baseTools = applyRoutingMissLeftoverTools(baseTools, allTools, ctx)
	if profile.IsLight() || isLightPromptProfile(profile.PromptProfile) {
		tool.WriteToolExposureLog("execution_profile", userText, requestID, userID, profile.Layer, profile.TaskType, beforeProfileFilter, agentLoopToolNamesForLog(tools))
		log.Printf("[exec-profile] layer=%s prompt=%s task=%s request_id=%q user=%q confidence=%.2f reason=%q tool_budget=%d iteration_budget=%d routed_before=%d routed_after=%d tools=%q",
			profile.Layer, profile.PromptProfile, profile.TaskType, requestID, userID, profile.Confidence, profile.Reason, profile.ToolBudget, profile.IterationBudget, beforeProfileFilter, len(tools), executionProfileToolNames(tools))
	}

	// Computer Use: when intent/session active, force computer_* tools and
	// demote raw gui_click/type (text-primary OmniParser path).
	cuGateText := userText
	if ctx != nil && strings.TrimSpace(ctx.ComputerUseRoutingText) != "" {
		cuGateText = ctx.ComputerUseRoutingText
	}
	var cuActive, cuFresh bool
	if ctx != nil {
		syncComputerUseTurn(h, ctx, userID, userText)
		cuActive, cuFresh = ctx.ComputerUseActive, ctx.ComputerUseFresh
	} else {
		cuActive, cuFresh = recordComputerUseGate(h, nil, cuGateText)
	}
	if cuActive && cuFresh && loopContextHasRoutingMissFallback(ctx) {
		// A miss is not a new desktop task. UIC often guesses computer_use
		// for 「图上有什么」; injecting capture tools here re-expands the
		// surface we just bounded.
		tools = removeComputerUseTools(tools)
	} else if cuActive {
		if cuFresh {
			// New desktop task: lift a stale hard-stop so the injected tools are
			// not blocked by a previous Stop. Guarded per request — a stopped
			// in-flight turn re-gating while cancel lags keeps its Stop.
			liftComputerUseStopForFreshRequest(requestID)
		}
		tools = ensureComputerUseTools(tools, allTools, true)
	} else if localFileWorkBlocksComputerUse(cuGateText) {
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
	// A generic MCP gateway lets the model select arbitrary provider identity
	// and target at execution time. Dynamic MCP calls must instead be published
	// through a managed semantic surface with an observed provider binding.
	// Keep the host-side gateway for explicit AgentView/skill/sub-agent calls;
	// it is not a legacy model capability.
	tools = removeLegacyModelMCPGateway(tools)
	baseTools = removeLegacyModelMCPGateway(baseTools)
	// manage_skill is likewise a merged dynamic gateway. Legacy model turns
	// cannot bind a Skill, package, Hub, or run record through its arguments;
	// retain no definition here and leave the narrow list-only compatibility
	// action as execution defense for stale responses. Installed skills are
	// rendered only by the managed dynamic semantic catalog.
	tools = removeLegacyModelManageSkillGateway(tools)
	baseTools = removeLegacyModelManageSkillGateway(baseTools)
	// Keep the local-file fence last: client-declared tools and future pipeline
	// stages above must not be able to reintroduce computer_* after routing has
	// correctly selected local document handling.
	tools = filterComputerUseToolsForLocalFileWork(ctx, userText, tools)

	toolsForLLM := stripExecutionContractMetadataForLLM(tools)
	baseToolsForLLM := stripExecutionContractMetadataForLLM(baseTools)
	plannedTools, clientToolNames, legacyPlanBacked, planErr := h.renderClosedLegacyReplacementSurface(userText, ctx, toolsForLLM)
	if planErr != nil {
		// Never revive the raw compatibility slice after an adapter-plan error.
		// Returning an empty surface keeps static chat usable while making a
		// missing catalog contract observable instead of silently executable.
		log.Printf("[legacy-adapter] closed replacement rejected request_id=%q user=%q reason=%v", requestID, userID, planErr)
		plannedTools = nil
		clientToolNames = nil
		legacyPlanBacked = false
	}
	return agentLoopToolSet{
		Tools:            plannedTools,
		BaseTools:        baseToolsForLLM,
		LegacyPlanBacked: legacyPlanBacked,
		ClientToolNames:  clientToolNames,
		ToolsTokenBudget: estimateToolsTokens(plannedTools),
		PreparationTime:  time.Since(startedAt),
		WorkflowDecision: workflowFilterPolicy,
		BrowserBeforeWF:  browserBeforeWF,
	}
}

func removeLegacyModelMCPGateway(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, definition := range tools {
		if strings.TrimSpace(extractToolName(definition)) == "call_mcp_tool" {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

func removeLegacyModelManageSkillGateway(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, definition := range tools {
		if strings.TrimSpace(extractToolName(definition)) == "manage_skill" {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

// visionFallthroughExecutionToolNames is the no-search surface used when a
// staged image suppressed lookup grants. The name router stays closed so
// web_search cannot steal the photo; file/command tools stay available so a
// vision critique of a generated artifact can still rewrite and rerun it.
var visionFallthroughExecutionToolNames = []string{
	"write_file", "edit_file", "bash", "read_file", "list_directory", "read_tool_result",
}

var visionFallthroughExecutionToolSet = func() map[string]bool {
	out := make(map[string]bool, len(visionFallthroughExecutionToolNames))
	for _, name := range visionFallthroughExecutionToolNames {
		out[name] = true
	}
	return out
}()

func (h *IMMessageHandler) attachVisionFallthroughExecutionTools(ctx *LoopContext, tools []map[string]interface{}, hostReject *IMAgentResponse, userID, userText string, history []agent.ConversationEntry) []map[string]interface{} {
	if hostReject != nil || len(tools) > 0 || !loopContextIsVisionFallthrough(ctx) {
		return tools
	}
	if !visionFallthroughWantsExecutionTools(userText, history) {
		return tools
	}
	attached := h.visionFallthroughExecutionTools(userID)
	if ctx != nil && ctx.LansengerGroupPermissions != nil {
		attached = filterToolsForLansengerGroupPermissions(attached, *ctx.LansengerGroupPermissions)
	}
	if len(attached) == 0 {
		return tools
	}
	requestID := ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
	}
	log.Printf("[semantic-routing] vision fallthrough attaching no-search execution tools request_id=%q count=%d names=%q",
		requestID, len(attached), agentLoopToolNamesForLog(attached))
	return attached
}

func visionFallthroughWantsExecutionTools(userText string, history []agent.ConversationEntry) bool {
	intentText := semanticUserIntentText(userText)
	if visionFallthroughLooksLikeArtifactRevision(intentText) {
		return true
	}
	if visionFallthroughLooksLikeImageLookup(intentText) {
		return false
	}
	return conversationUsedFileExecution(history)
}

func conversationUsedFileExecution(history []agent.ConversationEntry) bool {
	start := 0
	if n := len(history); n > 12 {
		start = n - 12
	}
	for _, entry := range history[start:] {
		if visionFallthroughExecutionToolSet[strings.TrimSpace(entry.ToolName)] {
			return true
		}
		if toolCallsMentionFileExecution(entry.ToolCalls) {
			return true
		}
	}
	return false
}

func toolCallsMentionFileExecution(raw interface{}) bool {
	switch calls := raw.(type) {
	case []llm.ToolCall:
		for _, call := range calls {
			if visionFallthroughExecutionToolSet[strings.TrimSpace(call.Function.Name)] {
				return true
			}
		}
	case []interface{}:
		for _, item := range calls {
			switch call := item.(type) {
			case llm.ToolCall:
				if visionFallthroughExecutionToolSet[strings.TrimSpace(call.Function.Name)] {
					return true
				}
			case map[string]interface{}:
				if visionFallthroughExecutionToolSet[toolNameFromCallMap(call)] {
					return true
				}
			case map[string]string:
				if visionFallthroughExecutionToolSet[strings.TrimSpace(call["name"])] {
					return true
				}
			}
		}
	case []map[string]interface{}:
		for _, m := range calls {
			if visionFallthroughExecutionToolSet[toolNameFromCallMap(m)] {
				return true
			}
		}
	case []map[string]string:
		for _, m := range calls {
			if visionFallthroughExecutionToolSet[strings.TrimSpace(m["name"])] {
				return true
			}
		}
	}
	return false
}

func toolNameFromCallMap(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	switch fn := m["function"].(type) {
	case map[string]interface{}:
		if name, ok := fn["name"].(string); ok {
			return strings.TrimSpace(name)
		}
	case map[string]string:
		return strings.TrimSpace(fn["name"])
	}
	if name, ok := m["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	return ""
}

func visionFallthroughLooksLikeArtifactRevision(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if containsAnyMarker(text, []string{
		"堆在一起", "叠在一起", "挤在一起", "重叠",
		"重新生成", "再生成", "改一下", "修一下", "重做",
		"regenerate",
	}) {
		return true
	}
	if containsAnyMarker(text, []string{"太乱", "不清晰"}) && !visionFallthroughLooksLikeImageLookup(text) {
		return true
	}
	return false
}

func containsAnyMarker(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func visionFallthroughLooksLikeImageLookup(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	return containsAnyMarker(text, []string{"天气", "weather", "图中有什么", "这是什么", "what is this", "what's in"})
}

func (h *IMMessageHandler) visionFallthroughExecutionTools(userID string) []map[string]interface{} {
	if h == nil {
		return nil
	}
	var out []map[string]interface{}
	for _, def := range h.getTools() {
		name := extractToolName(def)
		if !visionFallthroughExecutionToolSet[name] {
			continue
		}
		out = append(out, def)
	}
	out = h.filterToolsForExpertUser(userID, out)
	return stripExecutionContractMetadataForLLM(out)
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
	return append(tools, clientToolDefinitionsForAgent(ctx, tools)...)
}

// clientToolDefinitionsForAgent materializes the request-scoped client tools
// against an immutable host name set. It never mutates the host surface and is
// deliberately called after the host LegacyAdapterPlan renderer.
func clientToolDefinitionsForAgent(ctx *LoopContext, hostTools []map[string]interface{}) []map[string]interface{} {
	if ctx == nil || ctx.ClientToolContext == nil || len(ctx.ClientTools) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(hostTools))
	for _, def := range hostTools {
		if name := tool.ExtractToolName(def); name != "" {
			seen[name] = true
		}
	}
	clientDefinitions := make([]map[string]interface{}, 0, len(ctx.ClientTools))
	for _, clientTool := range ctx.ClientTools {
		name := strings.TrimSpace(clientTool.Name)
		if name == "" || seen[name] {
			continue // host tools always win a name collision
		}
		parameters := cloneClientToolSchema(clientTool.InputSchema)
		clientDefinitions = append(clientDefinitions, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": name, "description": strings.TrimSpace(clientTool.Description), "parameters": parameters,
			},
		})
		seen[name] = true
	}
	return clientDefinitions
}

func cloneClientToolSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	copySchema := agent.CloneToolDefinitionMap(schema)
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
