package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// skillHintWordBoundaryRe matches a skill preference hint as a whole word,
// preventing false positives from substrings in domain names, file paths, or
// compound words (e.g. "paper" matching "mypapers.top").
var skillHintWordBoundaryRe = regexp.MustCompile(`(?i)\bpaper\b|\breport\b`)

func shouldPreferSkillForTask(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	// Naming a skill ("使用 book-pdf skill") is the execution request. It must
	// not wait for a document-type hint such as "pdf" / "报告".
	if intent.ExplicitSkillInvocation(text) {
		return true
	}

	// Intent-capability pre-check: if the user's intent is clearly a
	// query/inspect action, skills are unlikely to help — the user wants to
	// operate on existing files, not generate new ones. Only enter the skill
	// preference path when the intent is compatible with generation.
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

// matchPreferredLocalSkill picks the local skill the loop should steer toward.
// agentDomain confines the choice to the caller's experience pool so the loop
// never steers a coding turn into a skill distilled from unrelated chat.
func matchPreferredLocalSkill(exec *SkillExecutor, userText, agentDomain string) (string, string) {
	name, reason, _ := matchPreferredLocalSkillMode(exec, userText, agentDomain)
	return name, reason
}

func matchPreferredLocalSkillMode(exec *SkillExecutor, userText, agentDomain string) (string, string, skillPreferenceMode) {
	if exec == nil {
		return "", "", skillPreferenceNone
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return "", "", skillPreferenceNone
	}
	candidates := filterSkillsForExperienceDomain(agentDomain, exec.List())
	userIntent := extractUserIntentCategory(userText)
	bestName := ""
	bestReason := ""
	bestScore := 0
	bestGuided := false
	for _, sk := range candidates {
		if strings.TrimSpace(sk.Name) == "" {
			continue
		}
		st := normalizeSkillEntryStatus(sk.Status)
		if st != skillEntryStatusActive && st != skillEntryStatusUnknown {
			continue
		}
		score, identity := preferredLocalSkillScore(sk, lower)
		if score <= 0 {
			continue
		}
		// Identity (name/trigger) is the user naming the skill. The generate-vs-query
		// gate is only for weak document-type overlap, or "看看 book-pdf" would lose
		// to remote search because the SKILL.md says 生成.
		if !identity && !isIntentCategoryCompatibleWithSkill(userIntent, sk.Description) {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestName = sk.Name
			bestReason = firstNonEmptyTraceText(sk.Description, strings.Join(sk.Triggers, ", "))
			bestGuided = nlSkillIsAgentGuided(sk)
		}
	}
	if bestScore <= 0 {
		return "", "", skillPreferenceNone
	}
	if bestGuided {
		return bestName, bestReason, skillPreferenceAgentGuided
	}
	return bestName, bestReason, skillPreferenceLocalOnly
}

// preferredLocalSkillScore ranks an installed skill against the current turn.
// Identity (name aliases, then triggers) outranks document-type token overlap
// so "book-pdf skill" cannot lose to a learned weather-PDF recipe. Agent-guided
// workflows only score on identity: volunteering them because SKILL.md mentions
// "pdf" is how an unrelated "生成pdf报告" hijacks Book-PDF.
func preferredLocalSkillScore(sk NLSkillDefinition, msgLower string) (score int, identity bool) {
	if skillDocNameAliasOccurs(sk.Name, msgLower) {
		score += 20
		identity = true
	}
	if countTriggerMatches(sk.Triggers, msgLower) > 0 {
		score += 10
		identity = true
	}
	if nlSkillIsAgentGuided(sk) {
		return score, identity
	}
	desc := strings.ToLower(strings.TrimSpace(sk.Description))
	if desc == "" {
		return score, identity
	}
	for _, token := range []string{"pdf", "报告", "文档", "综述", "markdown", "daily papers"} {
		if strings.Contains(msgLower, token) && strings.Contains(desc, token) {
			score += 2
		}
	}
	if skillHintWordBoundaryRe.MatchString(msgLower) {
		for _, token := range []string{"paper", "report"} {
			if strings.Contains(desc, token) {
				score += 2
			}
		}
	}
	return score, identity
}

func skillDocNameAliasOccurs(name, msgLower string) bool {
	for _, alias := range skillDocMatchAliases(name, nil) {
		if skillDocPhraseOccurs(msgLower, alias) {
			return true
		}
	}
	return false
}

func nlSkillIsAgentGuided(s NLSkillDefinition) bool {
	return cskill.IsAgentGuidedWorkflowSkill(skillDefinitionAsEntry(s))
}

// agentGuidedSkillOwnerID is the session owner used for sticky workflow
// memory. Prompt construction often passes PolicyOwnerID; the loop keys
// phase state by LoopContext.UserID. Mixing them made follow-up turns keep
// agent-guided tools but drop SKILL.md injection.
func agentGuidedSkillOwnerID(ctx *LoopContext, fallback string) string {
	if ctx != nil {
		if id := strings.TrimSpace(ctx.UserID); id != "" {
			return id
		}
	}
	return strings.TrimSpace(fallback)
}

func (h *IMMessageHandler) rememberAgentGuidedSkill(userID, name string) {
	if h == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	h.stickyAgentGuidedSkill.Store(userID, name)
}

func (h *IMMessageHandler) forgetAgentGuidedSkill(userID string) {
	if h == nil {
		return
	}
	h.stickyAgentGuidedSkill.Delete(userID)
}

func (h *IMMessageHandler) recallAgentGuidedSkill(userID string, exec *SkillExecutor) string {
	if h == nil || exec == nil {
		return ""
	}
	raw, ok := h.stickyAgentGuidedSkill.Load(userID)
	if !ok {
		return ""
	}
	name, _ := raw.(string)
	name = strings.TrimSpace(name)
	if name == "" {
		h.forgetAgentGuidedSkill(userID)
		return ""
	}
	for _, sk := range exec.List() {
		if sk.Name == name && nlSkillIsAgentGuided(sk) {
			st := normalizeSkillEntryStatus(sk.Status)
			if st == skillEntryStatusActive || st == skillEntryStatusUnknown {
				return name
			}
		}
	}
	h.forgetAgentGuidedSkill(userID)
	return ""
}

func looksLikeAgentGuidedContinuation(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, cue := range []string{
		"继续", "然后", "下一步", "大纲", "按这个", "开始写", "接着",
		"部分", "章节", "好的", "可以", "收到",
		"continue", "go ahead", "outline", "okay",
	} {
		if strings.Contains(t, cue) {
			return true
		}
	}
	return false
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
	if phase.SkillMode == skillPreferenceAgentGuided {
		return false
	}
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
	if phase.SkillMode == skillPreferenceAgentGuided && strings.TrimSpace(phase.PreferredSkillName) != "" {
		return fmt.Sprintf("[Skill preference]\n%q is an installed agent-guided workflow, not a tool name. Follow ## Skill 使用文档 with host tools (bash, read_file, write_file, edit_file). Do not call discover_tool or search_and_install_skill to look it up. Do not substitute generate_pdf. Do not call manage_skill.\n[/Skill preference]", phase.PreferredSkillName)
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

var agentGuidedSuppressedToolNames = map[string]bool{
	"discover_tool":            true,
	"search_and_install_skill": true,
	"search_skill_hub":         true,
	"install_skill_hub":        true,
	"generate_pdf":             true,
	"tools_search":             true,
}

var agentGuidedHostToolNames = []string{"bash", "read_file", "write_file", "edit_file", "list_directory"}

func applyAgentGuidedWorkflowSurface(tools, catalog []map[string]interface{}) []map[string]interface{} {
	return ensureAgentGuidedHostTools(filterToolsForAgentGuidedWorkflow(tools), catalog)
}

func isAgentGuidedHostToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "bash", "read_file", "write_file", "edit_file", "edit_lines", "list_directory":
		return true
	default:
		return false
	}
}

func filterToolsForAgentGuidedWorkflow(toolDefs []map[string]interface{}) []map[string]interface{} {
	if len(toolDefs) == 0 {
		return toolDefs
	}
	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, def := range toolDefs {
		if agentGuidedSuppressedToolNames[extractToolName(def)] {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

func ensureAgentGuidedHostTools(tools, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) == 0 {
		return tools
	}
	seen := make(map[string]bool, len(tools))
	for _, def := range tools {
		seen[extractToolName(def)] = true
	}
	out := tools
	need := make(map[string]bool, len(agentGuidedHostToolNames))
	for _, name := range agentGuidedHostToolNames {
		need[name] = true
	}
	for _, def := range allTools {
		name := extractToolName(def)
		if !need[name] || seen[name] {
			continue
		}
		out = append(out, def)
		seen[name] = true
	}
	return out
}

func processSkillPreferenceToolResults(phase *agentLoopPhase, toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome, toolExecResults []toolExecutionResult) {
	if len(toolExecResults) == len(toolCalls) {
		processSkillPreferenceToolExecutions(phase, toolCalls, toolExecResults)
		return
	}
	processSkillPreferenceToolExecutions(phase, toolCalls, buildToolExecutionResults(toolCalls, toolResults, toolOutcomes))
}

func processSkillPreferenceToolExecutions(phase *agentLoopPhase, toolCalls []llm.ToolCall, toolExecResults []toolExecutionResult) {
	if phase == nil {
		return
	}
	if phase.SkillMode == skillPreferenceAgentGuided {
		for _, tc := range toolCalls {
			if isAgentGuidedHostToolName(tc.Function.Name) {
				phase.SkillAttempted = true
				break
			}
		}
	}
	if !shouldBypassSkillPreference(toolCalls) {
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
