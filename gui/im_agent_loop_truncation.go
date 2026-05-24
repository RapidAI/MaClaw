package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const maxTruncationRetries = 3

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
	if engine := h.getWorkflowEngine(); engine != nil {
		applyFilter := ctx == nil || shouldApplyWorkflowFilter(ctx.SkipNeedsConfirmGate, engine.IsAwaitingReview(userID), ctx.WorkflowAgentLoop, engine.IsPhaseExecutionBlocked(userID))
		if applyFilter {
			catalog = h.applyWorkflowToolFilter(userID, catalog)
		}
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
	alternatives := []string{"Split large file content across smaller writes."}
	if toolListContainsName(tools, "craft_tool") {
		alternatives = append(alternatives, "craft_tool is available, so you may use it to generate a local script.")
	}
	if toolListContainsName(tools, "bash") {
		alternatives = append(alternatives, "bash is available, so a script/heredoc is also acceptable.")
	}
	if len(alternatives) == 1 {
		alternatives = append(alternatives, "Use only tools available in the current tool list.")
	}
	return fmt.Sprintf("[system hint] Tool call arguments were incomplete or truncated: %s. %s", truncatedList, strings.Join(alternatives, " "))
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
