package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Expert optimization ("专家优化"): distill the user's interaction experience
// from an expert session into a new, independent optimized expert.
//
// Invariants:
//   - The source expert is never overwritten; the draft always targets a new
//     expert or the one existing optimized expert of this source.
//   - Each expert has at most one direct optimized expert
//     (ExpertDefinition.OptimizedFromID == source id). Re-running optimization
//     re-distills from the latest session and updates that existing expert.
//   - Optimized experts are independent: they can be shared and optimized
//     again (chained lineage, one direct child per expert).
// ---------------------------------------------------------------------------

// expertOptimizeMaxMessages / expertOptimizeMaxTranscriptRunes bound the
// session transcript fed to the distillation meta prompt.
const (
	expertOptimizeMaxMessages        = 30
	expertOptimizeMaxTranscriptRunes = 12000
)

// expertOptimizeSystemPrompt is the meta prompt for session distillation.
const expertOptimizeSystemPrompt = `你是一名"AI 专家优化师"。用户会给你一位专家的现有配置，以及用户与该专家的最近会话记录。
你的任务：从会话中提炼用户反复强调的要求、纠正、偏好和约束，把它们固化进专家的系统提示词，产出一份"优化版专家"配置。

要求：
- 保留原 system_prompt 的核心人格、专长与工作流程，不要推倒重写。
- 把会话中用户新增的约束/规则/输出格式偏好，明确地写进 system_prompt 的合适小节；没有合适小节就补充到"边界约束"或新增"用户偏好"小节。
- 如果会话中专家暴露出理解偏差或被用户纠正，把纠正后的规则写清楚，避免再犯。
- 不要捏造会话中没有出现过的要求。

只输出一个严格的 JSON 对象，不要输出任何其他文字、解释或 Markdown 代码块标记：
{
  "name": "优化版专家名称，不超过 10 个汉字，必须与原专家名称不同",
  "description": "一句话卡片简介，不超过 40 个汉字",
  "icon": "一个最能代表该专家的 emoji（单个字符）",
  "system_prompt": "完整的优化版系统提示词（中文，结构化，含 角色定位/工作流程/输出格式/边界约束 小节）"
}`

// expertOptimizeDraft is the payload returned to the frontend editor. It is
// ExpertDefinition-shaped so the editor can prefill directly and SaveExpert
// can persist it as-is; UpdateExisting tells the editor this draft updates
// the source's existing optimized expert rather than creating a new one.
type expertOptimizeDraft struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Icon            string   `json:"icon"`
	SystemPrompt    string   `json:"system_prompt"`
	Tools           []string `json:"tools"`
	Skills          []string `json:"skills"`
	OptimizedFromID string   `json:"optimized_from_id"`
	About           string   `json:"about,omitempty"`
	UpdateExisting  bool     `json:"update_existing"`
	SourceName      string   `json:"source_name"`
}

// expertOptimizeLLMOutput validates the distillation output shape.
type expertOptimizeLLMOutput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	SystemPrompt string `json:"system_prompt"`
}

// OptimizeExpertFromSession distills the expert session history into an
// optimized-expert draft JSON for the frontend editor to review. Nothing is
// persisted here — saving is the user's explicit action in the editor.
func (a *App) OptimizeExpertFromSession(expertID string) (string, error) {
	expertID = strings.TrimSpace(expertID)
	if expertID == "" {
		return "", fmt.Errorf("expert id is required")
	}
	source := loadExpertDefByID(expertID)
	if source == nil {
		return "", fmt.Errorf("expert not found: %s", expertID)
	}

	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", errors.New("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.memory == nil {
		return "", errors.New("message handler unavailable")
	}

	transcript := buildExpertOptimizeTranscript(handler, expertSessionUserID(expertID))
	if transcript == "" {
		return "", fmt.Errorf("还没有会话经验可提炼，请先与该专家对话")
	}

	userMsg := buildExpertOptimizeUserMessage(source, transcript)
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "expert-optimize", OwnerID: desktopUserID})
	result, err := handler.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      expertOptimizeSystemPrompt,
		UserMessage:       userMsg,
		PreferLightweight: false,
		TimeoutSec:        90,
		Tag:               "expert-optimize",
	})
	if err != nil {
		return "", fmt.Errorf("expert optimization failed: %w", err)
	}
	output, err := parseExpertOptimizeResponse(result.Text)
	if err != nil {
		log.Printf("[expert-optimize] parse failed: %v (raw_len=%d)", err, len([]rune(result.Text)))
		return "", fmt.Errorf("expert optimization returned unparseable output: %w", err)
	}

	// One optimized expert per source: re-distillation updates the existing one.
	var existing *ExpertDefinition
	if found, ok, ferr := defaultExpertStore.FindOptimizedFor(source.ID); ferr != nil {
		log.Printf("[expert-optimize] find optimized for %q failed: %v", source.ID, ferr)
	} else if ok {
		cp := found
		existing = &cp
	}
	draft := buildExpertOptimizeDraft(source, output, existing)

	out, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// buildExpertOptimizeDraft assembles the editor draft from the distillation
// output. Pure function for testability. Rules:
//   - a new optimized expert inherits tools/skills from its source (they are
//     not re-picked by the LLM);
//   - an existing optimized expert keeps its id, its (possibly user-renamed)
//     name, its tools/skills and its about text — only description/icon/
//     system_prompt refresh from the new distillation;
//   - the optimized expert's name must differ from its source's name.
func buildExpertOptimizeDraft(source *ExpertDefinition, output expertOptimizeLLMOutput, existing *ExpertDefinition) expertOptimizeDraft {
	draft := expertOptimizeDraft{
		Name:            strings.TrimSpace(output.Name),
		Description:     strings.TrimSpace(output.Description),
		Icon:            strings.TrimSpace(output.Icon),
		SystemPrompt:    strings.TrimSpace(output.SystemPrompt),
		Tools:           source.Tools,
		Skills:          source.Skills,
		OptimizedFromID: source.ID,
		SourceName:      source.Name,
	}
	if existing != nil {
		draft.ID = existing.ID
		draft.UpdateExisting = true
		draft.Tools = existing.Tools
		draft.Skills = existing.Skills
		draft.About = existing.About
		if existing.Name != "" {
			draft.Name = existing.Name
		}
	}
	// The optimized expert's name must differ from its source.
	if draft.Name == "" || draft.Name == source.Name {
		draft.Name = source.Name + "·优化"
	}
	if draft.Icon == "" {
		draft.Icon = source.Icon
	}
	if draft.Description == "" {
		draft.Description = source.Description
	}
	return draft
}

// buildExpertOptimizeTranscript renders the tail of the expert session as a
// compact "role: content" transcript, capped to keep the meta prompt small.
func buildExpertOptimizeTranscript(h *IMMessageHandler, userID string) string {
	entries := h.memory.Load(userID)
	type line struct{ role, content string }
	lines := make([]line, 0, len(entries))
	for _, entry := range entries {
		role := strings.TrimSpace(strings.ToLower(entry.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(stringifyProjectConversationContent(entry.Content))
		if content == "" {
			continue
		}
		lines = append(lines, line{role: role, content: content})
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > expertOptimizeMaxMessages {
		lines = lines[len(lines)-expertOptimizeMaxMessages:]
	}
	var b strings.Builder
	for _, l := range lines {
		if l.role == "user" {
			b.WriteString("用户：")
		} else {
			b.WriteString("专家：")
		}
		b.WriteString(l.content)
		b.WriteString("\n")
	}
	transcript := b.String()
	if r := []rune(transcript); len(r) > expertOptimizeMaxTranscriptRunes {
		transcript = string(r[len(r)-expertOptimizeMaxTranscriptRunes:])
	}
	return strings.TrimSpace(transcript)
}

// buildExpertOptimizeUserMessage assembles the source config + transcript.
func buildExpertOptimizeUserMessage(source *ExpertDefinition, transcript string) string {
	var b strings.Builder
	b.WriteString("【原专家配置】\n名称：")
	b.WriteString(source.Name)
	b.WriteString("\n简介：")
	b.WriteString(source.Description)
	b.WriteString("\nsystem_prompt：\n")
	b.WriteString(source.SystemPrompt)
	b.WriteString("\n\n【最近会话记录】\n")
	b.WriteString(transcript)
	return b.String()
}

// parseExpertOptimizeResponse extracts and validates the distillation JSON.
// Mirrors parseExpertProfileResponse: strip code fences, first { to last }.
func parseExpertOptimizeResponse(raw string) (expertOptimizeLLMOutput, error) {
	var out expertOptimizeLLMOutput
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, fmt.Errorf("empty response")
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return out, fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return out, fmt.Errorf("JSON parse: %w", err)
	}
	if strings.TrimSpace(out.SystemPrompt) == "" {
		return out, fmt.Errorf("system_prompt is empty")
	}
	return out, nil
}
