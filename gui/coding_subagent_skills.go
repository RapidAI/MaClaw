package main

// coding_subagent_skills.go implements task-aware skill selection for the
// CodingSubAgent. When executing a coding task, the SubAgent can optionally
// call installed skills (e.g. UI optimization, lint fixing) via manage_skill.
//
// Selection mechanism:
//   1. Scan all active installed skills
//   2. Score each skill's (name + description + triggers) against the task description
//      using character n-gram overlap (works for CJK without word segmentation)
//   3. Take top-K (K = codingSubAgentMaxSkills, default 3) with score >= threshold
//   4. Inject matched skill summaries into system prompt + add manage_skill tool definition
//
// This avoids:
//   - Requiring skills to self-declare a "category" field (ecosystem incompatible)
//   - Maintaining a hardcoded keyword list (workaround, not mechanism)
//   - Injecting all 50+ skills into context (token waste + noise)

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

const (
	// codingSubAgentMaxSkills is the maximum number of skills injected into
	// the SubAgent's context per task. Keeps token budget bounded (~100 token
	// for the list + ~400 for manage_skill definition = ~500 total).
	codingSubAgentMaxSkills = 3

	// Full coding environment (create-task / Claude Code–aligned) allows a
	// broader skill surface so capability is not gated too aggressively.
	codingSubAgentFullEnvMaxSkills = 12

	// codingSubAgentSkillScoreThreshold is the minimum relevance score for a
	// skill to be considered relevant to the current task.
	codingSubAgentSkillScoreThreshold = 0.15

	// Full-env threshold is lower so more installed skills remain callable.
	codingSubAgentFullEnvSkillScoreThreshold = 0.05

	// codingSubAgentEmbeddingBaseline is subtracted from raw embedding cosine
	// before comparing with other signals. Short texts produce non-zero cosine
	// similarity even when semantically unrelated (Gemma 300M baseline ~0.2).
	// Without calibration, unrelated skills would exceed the score threshold.
	codingSubAgentEmbeddingBaseline = 0.2

	// codingSubAgentDynamicRequiredArgsMax bounds dynamic skill/MCP prompt
	// metadata so schemas with many required fields do not dominate context.
	codingSubAgentDynamicRequiredArgsMax = 6
)

// codingSubAgentSkillMatch is a skill that matched the current task.
type codingSubAgentSkillMatch struct {
	Name string
	// QualifiedID is an identity the host skill resolver matches exactly, or ""
	// for a skill that only has a display name. See
	// codingSubAgentSkillQualifiedID.
	QualifiedID  string
	Description  string
	Score        float64
	RequiredArgs []string // parameter names the skill expects (e.g. "input", "output")
	// Binding fields are a snapshot of the selected installed Skill. They are
	// revalidated immediately before a request-local alias executes; a changed
	// package must replan rather than inherit the old alias authority.
	StableID       string
	Version        string
	ContentDigest  string
	ContractDigest string
}

func codingSubAgentSkillContentDigest(def NLSkillDefinition) string {
	return codingSubAgentBindingDigest(struct {
		Name        string
		Description string
		Steps       []corelib.NLSkillStep
		Content     string
		Source      string
	}{
		Name:        strings.TrimSpace(def.Name),
		Description: def.Description,
		Steps:       def.Steps,
		Content:     def.Content,
		Source:      def.Source,
	})
}

func codingSubAgentSkillContractDigest(def NLSkillDefinition) string {
	return codingSubAgentBindingDigest(struct {
		RequiredArgs []string
		Params       []corelib.NLSkillParam
		Capabilities []string
		Requires     []string
	}{
		RequiredArgs: def.RequiredArgs,
		Params:       def.Params,
		Capabilities: def.Capabilities,
		Requires:     def.RequiresTools,
	})
}

func codingSubAgentBindingDigest(value interface{}) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// codingSubAgentSkillQualifiedID returns an identity that MatchesQualifiedID
// resolves exactly, or "" when the skill carries only a display name.
//
// This is deliberately not DynamicSkillStableID: that one falls back to
// "legacy:<name>" for skills with no stable identity, and no resolver in the
// run path matches that form. A skill without a qualified identity has to keep
// travelling as its display name.
func codingSubAgentSkillQualifiedID(def NLSkillDefinition) string {
	if id := strings.TrimSpace(def.SkillID); id != "" {
		return id
	}
	if hub := strings.TrimSpace(def.HubSkillID); hub != "" {
		return hub
	}
	if pub := strings.TrimSpace(def.Publisher); pub != "" {
		if name := strings.TrimSpace(def.Name); name != "" {
			return pub + ":" + name
		}
	}
	return ""
}

// selectRelevantSkillsForTask returns up to codingSubAgentMaxSkills skills
// whose description/triggers are relevant to the given task description.
// Returns nil if no skills match or the skill executor is unavailable.
func (c *codingSubAgentCallbacks) selectRelevantSkillsForTask(taskDescription string) []codingSubAgentSkillMatch {
	if c.subagent == nil || c.subagent.handler == nil {
		return nil
	}
	exec := c.subagent.handler.getSkillExecutor()
	if exec == nil {
		return nil
	}

	allSkills := exec.List()
	if len(allSkills) == 0 {
		return nil
	}
	// A coding turn only draws on the coding experience pool. Skills learned
	// from general chat stay out entirely, so no amount of scoring can put a
	// past conversation in front of the model as a capability.
	registrySize := len(allSkills)
	allSkills = filterSkillsForExperienceDomain(corelib.SkillDomainCoding, allSkills)
	skippedByExperienceDomain := registrySize - len(allSkills)
	if len(allSkills) == 0 {
		return nil
	}

	fullEnv := c.subagent.isFullEnvironment()
	maxK := codingSubAgentMaxSkills
	threshold := codingSubAgentSkillScoreThreshold
	if fullEnv {
		maxK = codingSubAgentFullEnvMaxSkills
		threshold = codingSubAgentFullEnvSkillScoreThreshold
	}

	// Filter to active, executable skills that the GUI runner can actually run.
	// Auto-summary now rejects incompatible drafts before persistence, but users
	// may already have learned skills created by older builds. Do this at the
	// selection boundary as well: surfacing a legacy web_search/manage_skill
	// recipe to the CodingSubAgent only makes it attempt a run that the runner
	// will reject during preflight.
	type candidate struct {
		name           string
		qualifiedID    string
		description    string
		doc            string   // scoring input: name + description + triggers
		requiredArgs   []string // params the skill expects
		stableID       string
		version        string
		contentDigest  string
		contractDigest string
	}
	var candidates []candidate
	skippedByTaskFit := 0
	skippedByRunnerCompatibility := 0
	for _, s := range allSkills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if skill.IsKnowledgeSkillType(s.Type) || skill.IsInstructionOnlySkillType(s.Type) {
			continue // documentation and app containers are not directly executable
		}
		entry := &corelib.NLSkillEntry{
			Name:        s.Name,
			Description: s.Description,
			Type:        s.Type,
			Source:      s.Source,
			Steps:       s.Steps,
			SkillDir:    s.SkillDir,
		}
		if report := skill.AssessRunnerCompatibility(entry, skill.RunnerBackendGUI); !report.Runnable {
			skippedByRunnerCompatibility++
			continue
		}
		doc := s.Name + " " + s.Description + " " + strings.Join(s.Triggers, " ")
		if !codingSubAgentSkillFitsTask(taskDescription, doc) {
			skippedByTaskFit++
			continue
		}
		candidates = append(candidates, candidate{
			name:           s.Name,
			qualifiedID:    codingSubAgentSkillQualifiedID(s),
			description:    s.Description,
			doc:            doc,
			requiredArgs:   s.RequiredArgs,
			stableID:       codingSubAgentSkillQualifiedID(s),
			version:        strings.TrimSpace(s.HubVersion),
			contentDigest:  codingSubAgentSkillContentDigest(s),
			contractDigest: codingSubAgentSkillContractDigest(s),
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	taskForScore := strings.TrimSpace(taskDescription)
	if taskForScore == "" {
		if !fullEnv {
			return nil
		}
		// Full workbench: still surface skills when task text is thin.
		taskForScore = "software development coding implementation testing debugging refactor"
	}

	// Build document strings for scoring.
	docs := make([]string, len(candidates))
	for i, cand := range candidates {
		docs[i] = cand.doc
	}

	// Three-signal scoring via shared infrastructure.
	emb := getSubAgentEmbedder(c.subagent.handler)
	scored := scoreAndSelectTopK(taskForScore, docs, emb, maxK, threshold)

	// Every injected skill must have earned its slot. A learned skill's
	// description is the raw request of the session it was distilled from, so
	// padding unfilled slots with unscored candidates puts an unrelated past
	// task ("北京天气") in front of the model as if it were the current one.
	// A full environment widens maxK and lowers the threshold; it does not
	// waive the threshold.
	if len(scored) == 0 {
		log.Printf("[coding-subagent] skill selection: task=%q full_env=%v candidates=%d skipped_other_domain=%d skipped_task_fit=%d skipped_runner_incompatible=%d matched=none threshold=%.2f",
			truncateLogText(taskDescription, 60), fullEnv, len(candidates), skippedByExperienceDomain, skippedByTaskFit, skippedByRunnerCompatibility, threshold)
		return nil
	}

	results := make([]codingSubAgentSkillMatch, len(scored))
	for i, s := range scored {
		cand := candidates[s.Idx]
		desc := cand.description
		if len([]rune(desc)) > 80 {
			desc = string([]rune(desc)[:80]) + "..."
		}
		results[i] = codingSubAgentSkillMatch{
			Name:           cand.name,
			QualifiedID:    cand.qualifiedID,
			Description:    desc,
			Score:          s.Score,
			RequiredArgs:   cand.requiredArgs,
			StableID:       cand.stableID,
			Version:        cand.version,
			ContentDigest:  cand.contentDigest,
			ContractDigest: cand.contractDigest,
		}
	}

	// Log selection results for diagnostics. Names and scores make it possible
	// to tell an intentional match from context bleed when a coding session
	// reasons about an unrelated topic.
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = fmt.Sprintf("%s(%.2f)", r.Name, r.Score)
	}
	log.Printf("[coding-subagent] skill selection: task=%q full_env=%v registry=%d candidates=%d skipped_other_domain=%d skipped_task_fit=%d skipped_runner_incompatible=%d threshold=%.2f matched=%s",
		truncateLogText(taskDescription, 60), fullEnv, registrySize, len(candidates), skippedByExperienceDomain, skippedByTaskFit, skippedByRunnerCompatibility, threshold, strings.Join(names, ", "))

	return results
}

func codingSubAgentSkillFitsTask(taskDescription, skillDoc string) bool {
	task := strings.ToLower(strings.TrimSpace(taskDescription))
	doc := strings.ToLower(strings.TrimSpace(skillDoc))
	if task == "" || doc == "" {
		return true
	}
	// Only an explicit document request in the task admits document/office
	// skills. Short or unrecognized task text ("push", "continue") used to fall
	// through here and admit every installed skill, which is how document and
	// learned-from-chat skills reached a coding turn.
	if codingSubAgentTextHasAny(task, codingSubAgentDocumentIntentMarkers()) {
		return true
	}
	if codingSubAgentTextHasAny(doc, codingSubAgentSoftwareSkillMarkers()) {
		return true
	}
	return !codingSubAgentTextHasAny(doc, codingSubAgentDocumentSkillMarkers())
}

func codingSubAgentTextHasAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func codingSubAgentSoftwareSkillMarkers() []string {
	return []string{
		"code", "coding", "program", "software", "driver", "kernel", "lint", "eslint",
		"format", "refactor", "test", "tests", "playwright", "frontend", "backend", "ui",
		"代码", "编程", "开发", "驱动", "实现", "修复", "测试", "前端", "后端", "界面",
	}
}

func codingSubAgentDocumentSkillMarkers() []string {
	return []string{
		"ppt", "pptx", "powerpoint", "presentation", "slide", "slides", "deck",
		"doc", "docx", "xls", "xlsx", "excel", "spreadsheet",
		"pdf", "word", "docx", "document", "contract", "文档", "合同", "演示", "幻灯片", "简报",
	}
}

func codingSubAgentDocumentIntentMarkers() []string {
	return []string{
		"ppt", "pptx", "powerpoint", "presentation", "slide", "slides", "deck",
		"doc", "docx", "xls", "xlsx", "excel", "spreadsheet",
		"pdf", "word", "docx", "document", "contract", "文档", "合同", "演示", "幻灯片", "简报",
		"报告", "说明书", "开发文档", "设计文档",
	}
}

// extractBigrams returns the set of character bigrams from text.
// For CJK text without spaces, bigrams provide effective fuzzy matching.
func extractBigrams(text string) map[string]bool {
	runes := []rune(text)
	if len(runes) < 2 {
		return nil
	}
	set := make(map[string]bool, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		bigram := string(runes[i : i+2])
		set[bigram] = true
	}
	return set
}

// bigramJaccard computes Jaccard similarity between two bigram sets.
// Returns 0 if either set is empty.
func bigramJaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for bg := range a {
		if b[bg] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

// buildSkillSection builds the system prompt section listing available skills.
// Returns empty string if no skills matched.
func buildCodingSubAgentSkillSection(skills []codingSubAgentSkillMatch) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	injected := make([]string, 0, len(skills))
	b.WriteString("\n## 可用 Skill\n")
	b.WriteString("以下条目是本机已安装的能力清单，不是用户的请求，不要把它们当成当前任务。仅在当前任务确实需要时，调用本轮工具列表中与该条目对应的 Skill 函数；函数别名和 Skill 身份由宿主绑定，不能自行传入或猜测。\n")
	for _, s := range skills {
		if len(s.RequiredArgs) > 0 {
			b.WriteString(fmt.Sprintf("- **%s**: %s（参数: %s）\n", s.Name, s.Description, compactCodingSubAgentRequiredArgs(s.RequiredArgs)))
		} else {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, s.Description))
		}
		injected = append(injected, fmt.Sprintf("%s(%.2f)", s.Name, s.Score))
	}
	log.Printf("[coding-subagent] skill prompt injection: count=%d skills=%s", len(skills), strings.Join(injected, ", "))
	b.WriteString("\n调用规则：\n")
	b.WriteString("- 每个 Skill 函数只执行已经绑定的一个 Skill，禁止 install/uninstall/upload/patch/status 或传入 Skill 名称\n")
	b.WriteString("- 如果 Skill 需要输入文件，先用 write_file 准备好文件，再传路径给 Skill\n")
	b.WriteString("- Skill 执行失败时不要反复重试，改用 bash + 手动命令完成任务\n")
	return b.String()
}

func compactCodingSubAgentRequiredArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	shown := len(args)
	if shown > codingSubAgentDynamicRequiredArgsMax {
		shown = codingSubAgentDynamicRequiredArgsMax
	}
	parts := make([]string, 0, shown+1)
	for _, arg := range args[:shown] {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			parts = append(parts, arg)
		}
	}
	if remaining := len(args) - shown; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 项未展开", remaining))
	}
	return strings.Join(parts, ", ")
}

// buildCodingSkillInvocationDefinition renders a request-local alias for one
// already-selected Skill. The alias accepts business arguments only; the model
// never controls the Skill identity or a run record ID.
func buildCodingSkillInvocationDefinition(alias string, skill codingSubAgentSkillMatch) map[string]interface{} {
	description := "执行本轮已绑定的 Skill"
	if name := strings.TrimSpace(skill.Name); name != "" {
		description += "（" + name + "）"
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        alias,
			"description": description,
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"args": map[string]interface{}{
						"type":        "object",
						"description": "Skill 运行参数。Skill 命令中的 {{key}} 占位符会被替换为 args 中对应的值。",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "输入参数（兼容旧调用）",
					},
					"output": map[string]interface{}{
						"type":        "string",
						"description": "输出路径（可选）",
					},
				},
			},
		},
	}
}

// codingSubAgentAllowedSkillActions defines which manage_skill actions the
// SubAgent is permitted to invoke. All other actions are rejected.
var codingSubAgentAllowedSkillActions = map[string]bool{
	"run":    true,
	"status": true,
}

// executeManageSkill is retained for explicit host-maintenance callers while
// the legacy transport is removed. It is not a model-dispatch entry point:
// model calls must use the durable bound-selection bridge, which does not
// accept a Skill selector from arguments.
func (c *codingSubAgentCallbacks) executeManageSkill(args map[string]interface{}) codingToolExecutionResult {
	// Explicit-maintenance guard: legacy gateway callers may use only matches
	// supplied by their host. Model callers are rejected before they reach here.
	if len(c.matchedSkills) == 0 {
		log.Printf("[coding-subagent] manage_skill blocked: no matched skills for this task")
		msg := "manage_skill is not available for this task (no relevant skills found)"
		return codingToolExecutionResult{
			Text:    c.rejectToolCall("manage_skill", args, msg),
			Outcome: codingToolOutcomeBlocked,
		}
	}

	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))

	// Restrict to allowed actions.
	if !codingSubAgentAllowedSkillActions[action] {
		log.Printf("[coding-subagent] manage_skill blocked: action=%q not allowed", action)
		msg := fmt.Sprintf("manage_skill action=%q is not allowed in coding SubAgent (allowed: run, status)", action)
		return codingToolExecutionResult{
			Text:    c.rejectToolCall("manage_skill", args, msg),
			Outcome: codingToolOutcomeBlocked,
		}
	}

	// For "run" action, validate skill name against matched skills.
	if action == "run" {
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return missingCodingSubAgentRequiredArgumentResult("manage_skill", "name")
		}
		candidates := c.matchedSkillCandidates(name)
		switch len(candidates) {
		case 1:
		case 0:
			allowed := make([]string, len(c.matchedSkills))
			for i, s := range c.matchedSkills {
				allowed[i] = s.Name
			}
			log.Printf("[coding-subagent] manage_skill blocked: skill=%q not in matched set %v", name, allowed)
			msg := fmt.Sprintf("skill %q is not available for this task (available: %s)", name, strings.Join(allowed, ", "))
			return codingToolExecutionResult{
				Text:    c.rejectToolCall("manage_skill", args, msg),
				Outcome: codingToolOutcomeBlocked,
			}
		default:
			refs := make([]string, len(candidates))
			for i, s := range candidates {
				refs[i] = codingSubAgentSkillBoundIdentity(s)
			}
			log.Printf("[coding-subagent] manage_skill blocked: skill=%q is ambiguous across %v", name, refs)
			msg := fmt.Sprintf("skill %q is ambiguous for this task: it names %d matched skills (%s). Reissue manage_skill with name set to one of those identifiers.", name, len(candidates), strings.Join(refs, ", "))
			return codingToolExecutionResult{
				Text:    c.rejectToolCall("manage_skill", args, msg),
				Outcome: codingToolOutcomeBlocked,
			}
		}
		return c.executeBoundCodingSkill("manage_skill", args, candidates[0])
	}
	return codingToolExecutionResult{Text: c.rejectToolCall("manage_skill", args, "status is unavailable without a request-local run binding"), Outcome: codingToolOutcomeBlocked}
}

// executeBoundCodingSkill is an explicit host-maintenance helper. The durable
// model path must reach a fixed bridge only after ResolveAlias, Validate and
// Admit; it must never route here from a request-local in-memory alias map.
func (c *codingSubAgentCallbacks) executeBoundCodingSkill(invocationName string, args map[string]interface{}, matchedSkill codingSubAgentSkillMatch) codingToolExecutionResult {
	if isCodingDynamicInvocationAlias(invocationName) && !c.codingSkillBindingIsCurrent(matchedSkill) {
		return codingToolExecutionResult{Text: "[system rejected] skill_binding_stale; request a managed replan", Outcome: codingToolOutcomeBlocked}
	}
	if result, rejected := rejectMissingCodingSubAgentSkillRequiredArguments(matchedSkill, args); rejected {
		return result
	}
	boundArgs := cloneCodingDynamicArguments(args)
	boundArgs["action"] = "run"
	boundArgs["name"] = codingSubAgentSkillBoundIdentity(matchedSkill)

	// Delegate to the host handler's toolManageSkill.
	h := c.subagent.handler
	if h == nil {
		msg := "manage_skill: host handler unavailable"
		return codingToolExecutionResult{
			Text:    c.rejectToolCall(invocationName, boundArgs, msg),
			Outcome: codingToolOutcomeFailed,
		}
	}

	// Pass SubAgent's progress callback so long-running skills report status.
	var progressCB func(string)
	if c.subagent.onProgress != nil {
		progressCB = func(text string) {
			c.subagent.onProgress(text)
		}
	}

	skillName, _ := boundArgs["name"].(string)
	log.Printf("[coding-subagent] dynamic skill: alias=%s name=%q", invocationName, skillName)

	result := h.toolManageSkill(context.Background(), boundArgs, progressCB)

	// Classify outcome based on known failure prefixes from toolManageSkill.
	// Do NOT use substring matching on the result body — skill output may
	// legitimately contain words like "error" (e.g. "fixed 3 errors").
	// toolManageSkill returns specific prefix patterns for failures.
	outcome := codingToolOutcomeSuccess
	if isCodingSubAgentDynamicToolFailure(result) ||
		strings.HasPrefix(result, "manage_skill failed:") ||
		strings.HasPrefix(result, "Skill Executor") ||
		strings.HasPrefix(result, "skill not found") ||
		strings.HasPrefix(result, "Skill \u672a\u627e\u5230") ||
		strings.HasPrefix(result, "\u53c2\u6570\u89e3\u6790\u5931\u8d25") ||
		// Legacy failure rows may start with U+274C (cross mark).
		(len(result) > 0 && []rune(result)[0] == 0x274C) {
		outcome = codingToolOutcomeFailed
	}
	c.trackDynamicToolResult(invocationName, skillName, result, outcome == codingToolOutcomeSuccess)
	return codingToolExecutionResult{Text: result, Outcome: outcome}
}

// codingSkillBindingIsCurrent re-reads the installed inventory at admission.
// Matching by the alias's captured identity plus content/contract digests keeps
// a changed package from inheriting authority granted to an earlier request.
func (c *codingSubAgentCallbacks) codingSkillBindingIsCurrent(binding codingSubAgentSkillMatch) bool {
	if c == nil || c.subagent == nil || c.subagent.handler == nil || strings.TrimSpace(binding.StableID) == "" ||
		strings.TrimSpace(binding.ContentDigest) == "" || strings.TrimSpace(binding.ContractDigest) == "" {
		return false
	}
	exec := c.subagent.handler.getSkillExecutor()
	if exec == nil {
		return false
	}
	for _, current := range exec.List() {
		if normalizeSkillEntryStatus(current.Status) != skillEntryStatusActive || codingSubAgentSkillQualifiedID(current) != binding.StableID {
			continue
		}
		if strings.TrimSpace(current.HubVersion) != strings.TrimSpace(binding.Version) {
			return false
		}
		return codingSubAgentSkillContentDigest(current) == binding.ContentDigest &&
			codingSubAgentSkillContractDigest(current) == binding.ContractDigest
	}
	return false
}

func cloneCodingDynamicArguments(args map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(args)+2)
	for key, value := range args {
		copy[key] = value
	}
	return copy
}
func isCodingSubAgentDynamicToolFailure(result string) bool {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"[error]",
		"error:",
		"错误:",
		"错误：",
		"失败:",
		"失败：",
		"failed:",
		"failure:",
		"exception:",
		"panic:",
		"tool error:",
		"skill failed",
		"[mcp error]",
		"mcp call failed:",
		"mcp tool error",
		"mcp 调用失败",
		"mcp 调用被拒绝",
		"mcp registry 未初始化",
		"本地 mcp manager 未初始化",
		"validation failed:",
		"arguments json ",
	} {
		if prefix != "" && strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// isMatchedSkill checks if a skill name was selected for this task.
func (c *codingSubAgentCallbacks) isMatchedSkill(name string) bool {
	_, ok := c.matchedSkill(name)
	return ok
}

// matchedSkill resolves a skill reference to the single matched entry it stands
// for. A reference covering no entry, or more than one, resolves to nothing:
// the caller must not guess which skill was meant.
func (c *codingSubAgentCallbacks) matchedSkill(name string) (codingSubAgentSkillMatch, bool) {
	candidates := c.matchedSkillCandidates(name)
	if len(candidates) != 1 {
		return codingSubAgentSkillMatch{}, false
	}
	return candidates[0], true
}

// matchedSkillCandidates returns every distinct matched entry a reference can
// stand for. A reference matches a display name or a qualified identity.
//
// Rows identical in both fields collapse to one. Two genuinely different
// skills that share a display name and have no qualified identity are
// indistinguishable here and also collapse; the host resolver reports those as
// ambiguous on its own, because neither can win its stable-identity pass.
func (c *codingSubAgentCallbacks) matchedSkillCandidates(name string) []codingSubAgentSkillMatch {
	if c == nil {
		return nil
	}
	want := strings.TrimSpace(name)
	if want == "" {
		return nil
	}
	var candidates []codingSubAgentSkillMatch
	seen := make(map[string]bool)
	for _, s := range c.matchedSkills {
		if !strings.EqualFold(strings.TrimSpace(s.Name), want) &&
			!strings.EqualFold(strings.TrimSpace(s.QualifiedID), want) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(s.QualifiedID)) + "\x00" + strings.ToLower(strings.TrimSpace(s.Name))
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, s)
	}
	return candidates
}

// codingSubAgentSkillBoundIdentity returns the identity the host should resolve:
// the qualified identity when the skill has one, otherwise the display name.
// Both come from the matched entry, never from the model.
func codingSubAgentSkillBoundIdentity(match codingSubAgentSkillMatch) string {
	if id := strings.TrimSpace(match.QualifiedID); id != "" {
		return id
	}
	return strings.TrimSpace(match.Name)
}

func rejectMissingCodingSubAgentSkillRequiredArguments(skill codingSubAgentSkillMatch, args map[string]interface{}) (codingToolExecutionResult, bool) {
	if len(skill.RequiredArgs) == 0 {
		return codingToolExecutionResult{}, false
	}
	skillArgs, _ := args["args"].(map[string]interface{})
	if skillArgs == nil {
		skillArgs = make(map[string]interface{})
		args["args"] = skillArgs
	}
	for _, field := range skill.RequiredArgs {
		value, ok := skillArgs[field]
		if !ok {
			if topLevelValue, topLevelOK := args[field]; topLevelOK {
				value = topLevelValue
				ok = true
				skillArgs[field] = topLevelValue
			}
		}
		if !ok || value == nil {
			return missingCodingSubAgentSkillRequiredArgumentResult(skill, field), true
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			return missingCodingSubAgentSkillRequiredArgumentResult(skill, field), true
		}
	}
	return codingToolExecutionResult{}, false
}

func missingCodingSubAgentSkillRequiredArgumentResult(skill codingSubAgentSkillMatch, field string) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    fmt.Sprintf("Error: manage_skill target %q is missing required skill argument %q in args. The skill was not executed. Regenerate manage_skill with args.%s set.", skill.Name, field, field),
		Outcome: codingToolOutcomeFailed,
	}
}

// truncateLogText truncates text for log output, appending "..." if truncated.
func truncateLogText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

// getSubAgentEmbedder retrieves the local Gemma embedding model from the host
// handler's interrupt handler. Returns nil if embedder is unavailable or noop.
func getSubAgentEmbedder(handler *IMMessageHandler) embedding.Embedder {
	if handler == nil || handler.interruptHandler == nil {
		return nil
	}
	emb := handler.interruptHandler.EmbedderForSubAgent()
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	return emb
}

// cosine32 computes cosine similarity between two float32 vectors.
// Returns 0 if either vector is empty or has zero magnitude.
func cosine32(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float32
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (sqrt32(magA) * sqrt32(magB))
}

func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
