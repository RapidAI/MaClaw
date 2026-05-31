package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// skillHintWordBoundaryRe matches a skill preference hint as a whole word,
// preventing false positives from substrings in domain names, file paths, or
// compound words (e.g. "paper" matching "mypapers.top").
var skillHintWordBoundaryRe = regexp.MustCompile(`(?i)\bpaper\b|\breport\b`)

func shouldPreferSkillForTask(text string) bool {
	result := classifyTaskIntent(text)
	if result.Intent == intentCoding || result.Intent == intentSSH {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}

	// Intent-capability pre-check: if the user's intent is clearly a
	// query/inspect action (缁熻/鎼滅储/鏌ユ壘/鍒楀嚭/鎵撳紑/璇诲彇), skills are
	// unlikely to help 鈥?the user wants to operate on existing files, not
	// generate new ones. Only enter the skill preference path when the
	// intent is compatible with generation (the dominant skill capability).
	//
	// This prevents "缁熻d鐩樹笂鐨刾df鏂囦欢" from triggering skill search
	// just because it contains "pdf". The user wants to COUNT files, not
	// CONVERT them.
	if !isIntentSkillPreferenceCompatible(text) {
		return false
	}

	// Substring hints 鈥?safe for multi-char Chinese phrases and distinctive
	// English terms that rarely appear as substrings of other words.
	substringHints := []string{
		"鎶ュ憡", "报告", "鏂囨。", "文档",
		"pdf", "鎶ュ憡", "鏂囨。", "缁艰堪", "markdown", "瀵煎嚭", "杞崲",
		"generate file", "send file", "daily papers",
	}
	for _, hint := range substringHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	// Word-boundary hints 鈥?common English words that frequently appear as
	// substrings in domain names / URLs (e.g. "mypapers.top", "report.csv").
	if skillHintWordBoundaryRe.MatchString(lower) {
		return true
	}
	return false
}

func (h *IMMessageHandler) consumePendingCapabilityGapContext(userID string) string {
	if h == nil {
		return ""
	}
	raw, ok := h.pendingCapabilityGap.LoadAndDelete(userID)
	if !ok {
		return ""
	}
	pending := raw.(*pendingCapabilityGapResult)
	if time.Since(pending.Timestamp) >= 10*time.Minute {
		return ""
	}
	if !pending.Success {
		log.Printf("[CapabilityGap] discarding failed async skill install for user %s: skill=%s result=%s", userID, pending.SkillName, truncateRunes(pending.Result, 100))
		return ""
	}
	log.Printf("[CapabilityGap] injecting async skill install result for user %s: skill=%s", userID, pending.SkillName)
	return fmt.Sprintf(
		"[Capability gap resolved] Skill %s was installed or updated asynchronously. Result:\n%s",
		pending.SkillName, pending.Result,
	)
}

func matchPreferredLocalSkill(exec *SkillExecutor, userText string) (string, string) {
	if exec == nil {
		return "", ""
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return "", ""
	}
	// Extract intent once, reuse for all skill comparisons.
	userIntent := extractUserIntentCategory(userText)
	bestName := ""
	bestReason := ""
	bestScore := 0
	for _, skill := range exec.List() {
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		score := 0
		for _, trigger := range skill.Triggers {
			trigger = strings.ToLower(strings.TrimSpace(trigger))
			if trigger == "" {
				continue
			}
			if strings.Contains(lower, trigger) {
				score += 3
			}
		}
		desc := strings.ToLower(strings.TrimSpace(skill.Description))
		if desc != "" {
			for _, token := range []string{"pdf", "鎶ュ憡", "鏂囨。", "缁艰堪", "markdown", "daily papers"} {
				if strings.Contains(lower, token) && strings.Contains(desc, token) {
					score += 2
				}
			}
			// Word-boundary tokens: require whole-word match in user text
			// to avoid false positives from domain names / URLs.
			if skillHintWordBoundaryRe.MatchString(lower) {
				for _, token := range []string{"paper", "report"} {
					if strings.Contains(desc, token) {
						score += 2
					}
				}
			}
		}
		// Intent-capability compatibility gate: even if topic tokens match,
		// reject the skill when the user's action verb is incompatible with
		// the skill's declared capability. This prevents "缁熻 PDF 鏂囦欢"
		// (query intent) from matching "xh-md-to-pdf" (generate capability).
		if score > 0 && !isIntentCategoryCompatibleWithSkill(userIntent, skill.Description) {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestName = skill.Name
			bestReason = firstNonEmptyTraceText(skill.Description, strings.Join(skill.Triggers, ", "))
		}
	}
	if bestScore <= 0 {
		return "", ""
	}
	return bestName, bestReason
}

func shouldBypassSkillPreference(toolCalls []llm.ToolCall) bool {
	for _, tc := range toolCalls {
		if classifyAgentToolKind(tc.Function.Name).IsSkillTool() {
			return true
		}
	}
	return false
}

func isSkillSearchToolName(name string) bool {
	return classifyAgentToolKind(name).IsSkillSearchTool()
}

func isSkillProgressToolName(name string) bool {
	return classifyAgentToolKind(name).IsSkillProgressTool()
}

func shouldRestrictToSkillSearch(phase agentLoopPhase) bool {
	return phase.ForceSkillPreference && phase.SkillMode == skillPreferenceRemoteRequired && !phase.RemoteSearchExhausted
}

func filterToolsForSkillPreference(toolDefs []map[string]interface{}) []map[string]interface{} {
	if len(toolDefs) == 0 {
		return toolDefs
	}
	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, def := range toolDefs {
		name := extractToolName(def)
		if classifyAgentToolKind(name).IsBlockedBySkillPreference() {
			continue
		}
		filtered = append(filtered, def)
	}
	if len(filtered) == 0 {
		return toolDefs
	}
	return filtered
}

func filterToolsForRemoteSkillSearch(toolDefs []map[string]interface{}) []map[string]interface{} {
	if len(toolDefs) == 0 {
		return toolDefs
	}
	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, def := range toolDefs {
		name := extractToolName(def)
		if isSkillSearchToolName(name) || isSkillProgressToolName(name) {
			filtered = append(filtered, def)
		}
	}
	if len(filtered) == 0 {
		return filterToolsForSkillPreference(toolDefs)
	}
	return filtered
}

func buildSkillProgressGuidance(skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return fmt.Sprintf("If run_id is available, call get_skill_run(run_id=%q) first to observe progress; only switch tools after a clear failure.", runID)
	}
	if skillName != "" {
		return fmt.Sprintf("If run_id is available, call get_skill_run(run_id=...) first; otherwise call manage_skill(action=\"run\", name=%q) to start execution.", skillName)
	}
	return "If run_id is available and no clear success/failure is visible yet, call get_skill_run(run_id=...) first to observe progress."
}

func buildSkillPreferenceConvergePrompt(phase agentLoopPhase) string {
	if phase.Stage != agentStageOrient || !phase.ForceSkillPreference {
		return ""
	}
	if shouldRestrictToSkillSearch(phase) {
		return "[Skill preference]\nThis task should prefer a reusable Skill, but no matching local Skill is available. This round must search/install a reusable Skill before using craft_tool or bash.\n[/Skill preference]"
	}
	if phase.PreferredSkillName == "" {
		return ""
	}
	guidance := buildSkillProgressGuidance(phase.PreferredSkillName, phase.PreferredSkillRunID)
	prompt := fmt.Sprintf("[Skill preference]\nA reusable local Skill is available: %s. Prefer manage_skill(action=\"run\", name=%q) for this task before craft_tool or bash. %s If the Skill fails, switch to another real tool path.\n[/Skill preference]", phase.PreferredSkillName, phase.PreferredSkillName, guidance)
	if phase.PreferredSkillReason != "" {
		prompt += fmt.Sprintf("\nMatch reason: %s", truncateTraceText(phase.PreferredSkillReason, 160))
	}
	return prompt
}

func processSkillPreferenceToolResults(phase *agentLoopPhase, toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome, toolExecResults []toolExecutionResult) {
	if len(toolExecResults) == len(toolCalls) {
		processSkillPreferenceToolExecutions(phase, toolCalls, toolExecResults)
		return
	}
	processSkillPreferenceToolExecutions(phase, toolCalls, buildToolExecutionResults(toolCalls, toolResults, toolOutcomes))
}

func processSkillPreferenceToolExecutions(phase *agentLoopPhase, toolCalls []llm.ToolCall, toolExecResults []toolExecutionResult) {
	if phase == nil || !shouldBypassSkillPreference(toolCalls) {
		return
	}
	phase.SkillAttempted = true
	if runID := extractSkillRunID(toolCalls, nil, toolExecResults); strings.TrimSpace(runID) != "" {
		phase.PreferredSkillRunID = strings.TrimSpace(runID)
	}
	// Clear PreferredSkillRunID when the skill run has reached a terminal state.
	// A terminal run (status: success/failed/error/cancelled/timeout) does not
	// need further polling or follow-up. Keeping a stale run_id causes the
	// no-tool branch to inject [Execution requirement] indefinitely, forcing
	// the LLM to continue even after the task is complete.
	if phase.PreferredSkillRunID != "" && isSkillRunTerminalInResults(toolExecResults) {
		phase.PreferredSkillRunID = ""
	}
	if phase.SkillMode == skillPreferenceRemoteRequired {
		phase.RemoteSearchAttempted = true
	}
	hasSearchTool := false
	for _, tc := range toolCalls {
		if isSkillSearchToolName(tc.Function.Name) {
			hasSearchTool = true
			break
		}
	}
	if didSkillToolExecutionFail(toolExecResults) {
		phase.SkillFailed = true
		phase.RemoteSearchExhausted = true
		phase.ForceSkillPreference = false
		phase.SkillMode = skillPreferenceFallbackAllowed
		if sn, se := extractFailedSkillInfoFromExecutions(toolCalls, toolExecResults); sn != "" {
			phase.FailedSkillName = sn
			phase.FailedSkillError = se
			log.Printf("[skill-workaround] skill %q failed, marking as pending workaround: %s", sn, truncateRunes(se, 120))
		}
		recoverPrompt := buildSkillRecoverPrompt(phase.PreferredSkillName, phase.PreferredSkillRunID)
		phase.PreferredSkillRunID = "" // terminal: skill failed, no further polling needed
		enterRecoverPhase(phase, agentRecoverSkillFailed, recoverPrompt)
		return
	}
	if shouldRestrictToSkillSearch(*phase) && hasSearchTool {
		phase.ForceSkillPreference = false
		phase.SkillMode = skillPreferenceFallbackAllowed
	}
}

func extractSkillRunID(toolCalls []llm.ToolCall, toolResults []string, toolExecResults []toolExecutionResult) string {
	if len(toolCalls) == 0 {
		return ""
	}
	for i := len(toolCalls) - 1; i >= 0; i-- {
		if !isSkillRunStarterToolCall(toolCalls[i]) {
			continue
		}
		if i < len(toolExecResults) {
			if runID := strings.TrimSpace(toolExecResults[i].Metadata.SkillRunID); runID != "" {
				return runID
			}
		}
		if i >= len(toolResults) {
			continue
		}
		result := strings.TrimSpace(toolResults[i])
		if result == "" {
			continue
		}
		if runID := extractSkillRunIDFromToolText(result); runID != "" {
			return runID
		}
	}
	return ""
}

// isSkillRunTerminalInResults returns true if any skill-related tool execution
// result indicates the run has reached a terminal state. This is used to clear
// PreferredSkillRunID after a skill completes — preventing the no-tool branch
// from injecting [Execution requirement] on subsequent iterations.
func isSkillRunTerminalInResults(toolExecResults []toolExecutionResult) bool {
	for _, r := range toolExecResults {
		switch r.ToolKind {
		case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindManageSkill:
			if r.Metadata.SkillRunTerminal {
				return true
			}
		}
	}
	return false
}

func buildSkillRecoverPrompt(skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	guidance := buildSkillProgressGuidance(skillName, runID)
	if skillName == "" {
		return "[Recover 阶段]\n本地 Skill 已尝试且失败，当前进入 Recover 阶段。不要重复同一个失败的 Skill。请基于已知失败原因重新规划，改用其他真实工具（如 send_file / craft_tool / bash）走最短交付路径，并继续完成任务。\n" + guidance + "\n[/Recover 阶段]"
	}
	return fmt.Sprintf("[Recover 阶段]\n本地 Skill %q 已尝试且失败。不要再次调用同一个 Skill。请基于失败原因重新规划，改用其他真实工具路径，如 send_file、craft_tool 或 bash。\n%s\n[/Recover 阶段]", skillName, guidance)
}
