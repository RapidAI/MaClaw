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

func processSkillPreferenceToolResults(phase *agentLoopPhase, toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) {
	if phase == nil || !shouldBypassSkillPreference(toolCalls) {
		return
	}
	phase.SkillAttempted = true
	if runID := extractSkillRunID(toolCalls, toolResults); strings.TrimSpace(runID) != "" {
		phase.PreferredSkillRunID = strings.TrimSpace(runID)
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
	if didSkillToolFail(toolCalls, toolOutcomes) {
		phase.SkillFailed = true
		phase.RemoteSearchExhausted = true
		phase.ForceSkillPreference = false
		phase.SkillMode = skillPreferenceFallbackAllowed
		if sn, se := extractFailedSkillInfo(toolCalls, toolResults, toolOutcomes); sn != "" {
			phase.FailedSkillName = sn
			phase.FailedSkillError = se
			log.Printf("[skill-workaround] skill %q failed, marking as pending workaround: %s", sn, truncateRunes(se, 120))
		}
		enterRecoverPhase(phase, agentRecoverSkillFailed, buildSkillRecoverPrompt(phase.PreferredSkillName, phase.PreferredSkillRunID))
		return
	}
	if shouldRestrictToSkillSearch(*phase) && hasSearchTool {
		phase.ForceSkillPreference = false
		phase.SkillMode = skillPreferenceFallbackAllowed
	}
}

func extractSkillRunID(toolCalls []llm.ToolCall, toolResults []string) string {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return ""
	}
	for i := len(toolCalls) - 1; i >= 0; i-- {
		if classifyAgentToolKind(toolCalls[i].Function.Name) != agentToolKindRunSkill {
			continue
		}
		result := strings.TrimSpace(toolResults[i])
		if result == "" {
			continue
		}
		if matches := regexp.MustCompile(`run_id[:=]\s*([A-Za-z0-9._-]+)`).FindStringSubmatch(result); len(matches) == 2 {
			return strings.TrimSpace(matches[1])
		}
		if matches := regexp.MustCompile(`run_id=([A-Za-z0-9._-]+)`).FindStringSubmatch(result); len(matches) == 2 {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

func buildSkillRecoverPrompt(skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	guidance := buildSkillProgressGuidance(skillName, runID)
	if skillName == "" {
		return "[Recover 闃舵]\n鏈湴 Skill 宸插皾璇曚笖澶辫触锛屽綋鍓嶈繘鍏?Recover 闃舵銆備笉瑕侀噸澶嶅悓涓€涓け璐?Skill銆傝鍩轰簬宸茬煡澶辫触鍘熷洜閲嶆柊瑙勫垝锛屾敼鐢ㄥ叾浠栫湡瀹炲伐鍏凤紙濡?send_file / craft_tool / bash锛夎蛋鏈€鐭氦浠樿矾寰勶紝骞剁户缁畬鎴愪换鍔°€俓n" + guidance + "\n[/Recover 闃舵]"
	}
	return fmt.Sprintf("[Recover]\nLocal Skill %q was attempted and failed. Do not call the same Skill again. Replan from the failure reason and use another real tool path, such as send_file, craft_tool, or bash.\n%s\n[/Recover]", skillName, guidance)
}
