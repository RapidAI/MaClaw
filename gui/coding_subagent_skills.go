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
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const (
	// codingSubAgentMaxSkills is the maximum number of skills injected into
	// the SubAgent's context per task. Keeps token budget bounded (~100 token
	// for the list + ~400 for manage_skill definition = ~500 total).
	codingSubAgentMaxSkills = 3

	// codingSubAgentSkillScoreThreshold is the minimum relevance score for a
	// skill to be considered relevant to the current task.
	codingSubAgentSkillScoreThreshold = 0.15

	// codingSubAgentEmbeddingBaseline is subtracted from raw embedding cosine
	// before comparing with other signals. Short texts produce non-zero cosine
	// similarity even when semantically unrelated (Gemma 300M baseline ~0.2).
	// Without calibration, unrelated skills would exceed the score threshold.
	codingSubAgentEmbeddingBaseline = 0.2
)

// codingSubAgentSkillMatch is a skill that matched the current task.
type codingSubAgentSkillMatch struct {
	Name         string
	Description  string
	Score        float64
	RequiredArgs []string // parameter names the skill expects (e.g. "input", "output")
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

	// Filter to active, executable skills only.
	type candidate struct {
		name         string
		description  string
		doc          string   // scoring input: name + description + triggers
		requiredArgs []string // params the skill expects
	}
	var candidates []candidate
	for _, s := range allSkills {
		if s.Status != "active" {
			continue
		}
		if s.Type == "knowledge" {
			continue // knowledge skills are reference docs, not executable
		}
		doc := s.Name + " " + s.Description + " " + strings.Join(s.Triggers, " ")
		candidates = append(candidates, candidate{
			name:         s.Name,
			description:  s.Description,
			doc:          doc,
			requiredArgs: s.RequiredArgs,
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	if len(strings.TrimSpace(taskDescription)) == 0 {
		return nil
	}

	// Build document strings for scoring.
	docs := make([]string, len(candidates))
	for i, cand := range candidates {
		docs[i] = cand.doc
	}

	// Three-signal scoring via shared infrastructure.
	emb := getSubAgentEmbedder(c.subagent.handler)
	scored := scoreAndSelectTopK(taskDescription, docs, emb, codingSubAgentMaxSkills, codingSubAgentSkillScoreThreshold)

	if len(scored) == 0 {
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
			Name:         cand.name,
			Description:  desc,
			Score:        s.Score,
			RequiredArgs: cand.requiredArgs,
		}
	}

	// Log selection results for diagnostics.
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = fmt.Sprintf("%s(%.2f)", r.Name, r.Score)
	}
	log.Printf("[coding-subagent] skill selection: task=%q candidates=%d matched=%s",
		truncateLogText(taskDescription, 60), len(candidates), strings.Join(names, ", "))

	return results
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
	b.WriteString("\n## 可用 Skill\n")
	b.WriteString("以下 Skill 可通过 manage_skill(action=\"run\", name=\"...\", args={...}) 调用：\n")
	for _, s := range skills {
		if len(s.RequiredArgs) > 0 {
			b.WriteString(fmt.Sprintf("- **%s**: %s（参数: %s）\n", s.Name, s.Description, strings.Join(s.RequiredArgs, ", ")))
		} else {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, s.Description))
		}
	}
	b.WriteString("\n调用规则：\n")
	b.WriteString("- 只允许 action=\"run\" 和 action=\"status\"，禁止 install/uninstall/upload/patch\n")
	b.WriteString("- 如果 Skill 需要输入文件，先用 write_file 准备好文件，再传路径给 Skill\n")
	b.WriteString("- Skill 执行失败时不要反复重试，改用 bash + 手动命令完成任务\n")
	return b.String()
}

// buildManageSkillToolDefinition returns the manage_skill tool definition
// scoped to run/status actions only (SubAgent cannot install/uninstall/patch).
func buildManageSkillToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "manage_skill",
			"description": "调用已安装的 Skill 执行辅助任务（如 UI 分析、代码格式化等）。仅支持 run 和 status 操作。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "操作类型：run（执行 Skill）或 status（查询执行状态）",
						"enum":        []string{"run", "status"},
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill 名称（run 时必填）",
					},
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
					"run_id": map[string]interface{}{
						"type":        "string",
						"description": "运行 ID（status 时必填）",
					},
				},
				"required": []string{"action"},
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


// executeManageSkill handles manage_skill calls from the SubAgent with
// action restriction (only run/status allowed) and skill name validation.
func (c *codingSubAgentCallbacks) executeManageSkill(args map[string]interface{}) codingToolExecutionResult {
	// Guard: manage_skill is only available when skills were matched for this task.
	if len(c.matchedSkills) == 0 {
		log.Printf("[coding-subagent] manage_skill blocked: no matched skills for this task")
		return codingToolExecutionResult{
			Text:    "manage_skill is not available for this task (no relevant skills found)",
			Outcome: codingToolOutcomeBlocked,
		}
	}

	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))

	// Restrict to allowed actions.
	if !codingSubAgentAllowedSkillActions[action] {
		log.Printf("[coding-subagent] manage_skill blocked: action=%q not allowed", action)
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("manage_skill action=%q is not allowed in coding SubAgent (allowed: run, status)", action),
			Outcome: codingToolOutcomeBlocked,
		}
	}

	// For "run" action, validate skill name against matched skills.
	if action == "run" {
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return codingToolExecutionResult{
				Text:    "manage_skill(action=run) requires 'name' parameter",
				Outcome: codingToolOutcomeFailed,
			}
		}
		if !c.isMatchedSkill(name) {
			allowed := make([]string, len(c.matchedSkills))
			for i, s := range c.matchedSkills {
				allowed[i] = s.Name
			}
			log.Printf("[coding-subagent] manage_skill blocked: skill=%q not in matched set %v", name, allowed)
			return codingToolExecutionResult{
				Text:    fmt.Sprintf("skill %q is not available for this task (available: %s)", name, strings.Join(allowed, ", ")),
				Outcome: codingToolOutcomeBlocked,
			}
		}
	}

	// Delegate to the host handler's toolManageSkill.
	h := c.subagent.handler
	if h == nil {
		return codingToolExecutionResult{
			Text:    "manage_skill: host handler unavailable",
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

	skillName, _ := args["name"].(string)
	log.Printf("[coding-subagent] manage_skill: action=%s name=%q", action, skillName)

	result := h.toolManageSkill(args, progressCB)

	// Classify outcome based on known failure prefixes from toolManageSkill.
	// Do NOT use substring matching on the result body — skill output may
	// legitimately contain words like "error" (e.g. "fixed 3 errors").
	// toolManageSkill returns specific prefix patterns for failures.
	outcome := codingToolOutcomeSuccess
	if result == "" {
		outcome = codingToolOutcomeFailed
	} else if strings.HasPrefix(result, "manage_skill failed:") ||
		strings.HasPrefix(result, "Skill Executor") ||
		strings.HasPrefix(result, "❌") ||
		strings.HasPrefix(result, "skill not found") ||
		strings.HasPrefix(result, "Skill 未找到") ||
		strings.HasPrefix(result, "参数解析失败") {
		outcome = codingToolOutcomeFailed
	}
	return codingToolExecutionResult{Text: result, Outcome: outcome}
}

// isMatchedSkill checks if a skill name was selected for this task.
func (c *codingSubAgentCallbacks) isMatchedSkill(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range c.matchedSkills {
		if strings.ToLower(s.Name) == lower {
			return true
		}
	}
	return false
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
