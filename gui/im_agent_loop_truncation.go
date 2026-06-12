package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const maxTruncationRetries = 3

const maxEssentialTruncationHints = 1

const (
	maxAgentLoopInlineWriteFileContentRunes = 3000
	maxAgentLoopInlineBashCommandRunes      = 4000
	maxAgentLoopInlineSSHCommandRunes       = 4000
)

type agentLoopTruncationRecoveryResult struct {
	Conversation []interface{}
	Tools        []map[string]interface{}
	ContinueLoop bool
}

func logAgentLoopPartialTruncation(choice llm.Choice) {
	if len(choice.TruncatedToolNames) == 0 {
		return
	}
	log.Printf("[agent-loop] partial truncation: %d tool call(s) removed (%s), %d valid call(s) proceeding",
		len(choice.TruncatedToolNames), strings.Join(choice.TruncatedToolNames, ", "), len(choice.Message.ToolCalls))
}

func resetAgentLoopTruncationRecoveryAfterToolCalls(phase *agentLoopPhase, choice llm.Choice) {
	if phase == nil || len(choice.Message.ToolCalls) == 0 || len(choice.TruncatedToolNames) > 0 {
		return
	}
	if phase.TruncationRetries == 0 && phase.EssentialTruncationHints == 0 {
		return
	}
	log.Printf("[agent-loop] reset truncation recovery counters after valid tool call branch (retries=%d essential_hints=%d)", phase.TruncationRetries, phase.EssentialTruncationHints)
	phase.TruncationRetries = 0
	phase.EssentialTruncationHints = 0
}

func (h *IMMessageHandler) handleAgentLoopTruncatedToolCalls(
	iteration int,
	choice llm.Choice,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	fallbackCatalog []map[string]interface{},
	recordSystemMessages func(int, []interface{}),
) agentLoopTruncationRecoveryResult {
	result := agentLoopTruncationRecoveryResult{Conversation: conversation, Tools: tools}
	if len(choice.TruncatedToolNames) == 0 || phase == nil {
		return result
	}
	result.ContinueLoop = true
	if allTruncatedToolsBlockSafe(choice.TruncatedToolNames) {
		return h.handleAgentLoopEssentialTruncatedToolCalls(iteration, choice, phase, conversation, tools, recordSystemMessages)
	}
	if phase.TruncationRetries < maxTruncationRetries {
		phase.TruncationRetries++
		phase.ConsecutiveNoTool = 0
		truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
		log.Printf("[agent-loop] truncated tool call recovery (retry %d/%d, iter=%d, tools=%s), injecting hint as system message",
			phase.TruncationRetries, maxTruncationRetries, iteration, truncatedList)
		hint := buildTruncationRetryHint(truncatedList, tools)
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": hint,
		})
		recordSystemMessages(systemMessagesStart, conversation)
		result.Conversation = conversation
		return result
	}

	if phase.TruncationBlockedTools == nil {
		phase.TruncationBlockedTools = make(map[string]bool)
	}
	var newlyBlocked []string
	for _, tn := range choice.TruncatedToolNames {
		if classifyAgentToolKind(tn).IsTruncationBlockSafe() {
			log.Printf("[agent-loop] skipping truncation block for essential tool %q (iter=%d)", tn, iteration)
			continue
		}
		if !phase.TruncationBlockedTools[tn] {
			phase.TruncationBlockedTools[tn] = true
			newlyBlocked = append(newlyBlocked, tn)
		}
	}
	if len(newlyBlocked) == 0 {
		log.Printf("[agent-loop] truncated tool call: no new tools to block (tools=%v, already_blocked=%v, iter=%d)",
			choice.TruncatedToolNames, phase.TruncationBlockedTools, iteration)
		result.ContinueLoop = false
		return result
	}

	phase.ConsecutiveNoTool = 0
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, td := range tools {
		name := tool.ExtractToolName(td)
		if !phase.TruncationBlockedTools[name] {
			filtered = append(filtered, td)
		}
	}
	filtered = ensureTruncationFallbackTools(filtered, fallbackCatalog, newlyBlocked, phase.TruncationBlockedTools)
	result.Tools = filtered
	blockedList := strings.Join(newlyBlocked, ", ")
	log.Printf("[agent-loop] truncation retries exhausted: blocking tools [%s] from LLM tool list (iter=%d, remaining_tools=%d)",
		blockedList, iteration, len(filtered))

	hint := fmt.Sprintf("[system hint] %sBlocked tools are not available now; use an alternate tool path.", buildTruncationBlockAlternativeInstructions(newlyBlocked, filtered))
	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": hint,
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	return result
}

func allTruncatedToolsBlockSafe(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if !classifyAgentToolKind(name).IsTruncationBlockSafe() {
			return false
		}
	}
	return true
}

func (h *IMMessageHandler) handleAgentLoopEssentialTruncatedToolCalls(
	iteration int,
	choice llm.Choice,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	recordSystemMessages func(int, []interface{}),
) agentLoopTruncationRecoveryResult {
	result := agentLoopTruncationRecoveryResult{Conversation: conversation, Tools: tools}
	if phase.EssentialTruncationHints >= maxEssentialTruncationHints {
		log.Printf("[agent-loop] essential tool truncation hint already injected; allowing no-tool recovery path (iter=%d)", iteration)
		return result
	}
	phase.EssentialTruncationHints++
	phase.ConsecutiveNoTool = 0
	truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
	log.Printf("[agent-loop] essential tool truncation recovery (hint %d/%d, iter=%d, tools=%s)", phase.EssentialTruncationHints, maxEssentialTruncationHints, iteration, truncatedList)
	hint := buildEssentialTruncationRecoveryHint(choice.TruncatedToolNames)
	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": hint,
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	result.ContinueLoop = true
	return result
}

func buildEssentialTruncationRecoveryHint(toolNames []string) string {
	truncatedList := strings.Join(toolNames, ", ")
	parts := []string{
		fmt.Sprintf("[system hint] Tool call arguments were truncated for essential tool(s): %s.", truncatedList),
		"Keep those tools available, but do not send oversized inline payloads.",
		agentLoopInlinePayloadLimitInstruction(),
	}
	if containsToolName(toolNames, "delegate_task") {
		parts = append(parts, "For delegate_task, keep request concise: reference the approved workflow task IDs and existing context instead of embedding long documents or file contents.")
	}
	if containsToolName(toolNames, "bash") {
		parts = append(parts, "Use bash only for short commands.")
	}
	return strings.Join(parts, " ")
}

func containsToolName(names []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, name := range names {
		if strings.TrimSpace(name) == want {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) truncationFallbackToolCatalog(ctx *LoopContext, userID string, phase *agentLoopPhase, baseTools []map[string]interface{}) []map[string]interface{} {
	if h == nil || len(baseTools) == 0 {
		return nil
	}
	catalog := append([]map[string]interface{}(nil), baseTools...)
	phaseValue := derefAgentLoopPhase(phase)
	if phaseValue.ForceSkillPreference {
		if shouldRestrictToSkillSearch(phaseValue) {
			catalog = filterToolsForRemoteSkillSearch(catalog)
		} else {
			catalog = filterToolsForSkillPreference(catalog)
		}
	}
	if policyOwnerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
		catalog = h.applyWorkflowToolFilter(policyOwnerID, catalog)
	}
	return catalog
}

func ensureTruncationFallbackTools(tools []map[string]interface{}, catalog []map[string]interface{}, newlyBlocked []string, blockedSet map[string]bool) []map[string]interface{} {
	want := truncationFallbackToolNames(newlyBlocked)
	if len(want) == 0 || len(catalog) == 0 {
		return tools
	}

	present := make(map[string]bool, len(tools))
	for _, td := range tools {
		if name := tool.ExtractToolName(td); name != "" {
			present[name] = true
		}
	}

	byName := make(map[string]map[string]interface{}, len(catalog))
	for _, td := range catalog {
		if name := tool.ExtractToolName(td); name != "" {
			byName[name] = td
		}
	}

	out := tools
	for _, name := range want {
		if blockedSet[name] {
			continue
		}
		if present[name] {
			continue
		}
		if td, ok := byName[name]; ok {
			out = append(out, td)
			present[name] = true
		}
	}
	return out
}

func truncationFallbackToolNames(blocked []string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, tn := range blocked {
		switch classifyAgentToolKind(tn) {
		case agentToolKindWriteFile, agentToolKindEditFile, agentToolKindGeneratePDF, agentToolKindOffice:
			add("craft_tool")
		}
	}
	return names
}

func buildTruncationRetryHint(truncatedList string, tools []map[string]interface{}) string {
	alternatives := []string{agentLoopInlinePayloadLimitInstruction()}
	if toolListContainsName(tools, "write_file") {
		alternatives = append(alternatives, "For large file content, use write_file once with mode=overwrite for the first chunk, then mode=append for later chunks.")
	}
	if toolListContainsName(tools, "craft_tool") {
		alternatives = append(alternatives, "craft_tool is available, so you may use it to generate a local script.")
	}
	if toolListContainsName(tools, "bash") {
		alternatives = append(alternatives, "bash is available only for short commands; do not embed generated file bodies in bash arguments.")
	}
	if len(alternatives) == 1 {
		alternatives = append(alternatives, "Use only tools available in the current tool list.")
	}
	return fmt.Sprintf("[system hint] Tool call arguments were incomplete or truncated: %s. %s", truncatedList, strings.Join(alternatives, " "))
}

func agentLoopInlinePayloadLimitInstruction() string {
	return fmt.Sprintf("Respect inline payload limits: write_file.content <= %d runes per call and bash.command <= %d runes per call.", maxAgentLoopInlineWriteFileContentRunes, maxAgentLoopInlineBashCommandRunes)
}

func buildTruncationBlockAlternativeInstructions(blocked []string, availableTools []map[string]interface{}) string {
	var instructions string
	for _, tn := range blocked {
		instructions += truncationBlockInstructionForAvailableTools(tn, availableTools)
	}
	return instructions
}

func truncationBlockInstructionForAvailableTools(toolName string, availableTools []map[string]interface{}) string {
	switch classifyAgentToolKind(toolName) {
	case agentToolKindWriteFile:
		return buildUnavailableToolInstruction(toolName, "file writes", availableTools)
	case agentToolKindEditFile:
		return buildUnavailableToolInstruction(toolName, "file edits", availableTools)
	default:
		return fmt.Sprintf("%s is temporarily blocked after repeated truncation. Use another currently available tool path.\n", toolName)
	}
}

func buildUnavailableToolInstruction(toolName, task string, availableTools []map[string]interface{}) string {
	var alternatives []string
	if toolListContainsName(availableTools, "craft_tool") {
		alternatives = append(alternatives, "prefer craft_tool to generate a local script")
	}
	if toolListContainsName(availableTools, "bash") {
		alternatives = append(alternatives, "use bash with an appropriate script")
	}
	if len(alternatives) == 0 {
		return fmt.Sprintf("%s is temporarily blocked after repeated truncation. Use another currently available tool path for %s.\n", toolName, task)
	}
	return fmt.Sprintf("%s is temporarily blocked after repeated truncation. For %s, %s.\n", toolName, task, strings.Join(alternatives, "; or "))
}

func toolListContainsName(tools []map[string]interface{}, want string) bool {
	for _, td := range tools {
		if tool.ExtractToolName(td) == want {
			return true
		}
	}
	return false
}
