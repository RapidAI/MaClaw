package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ---------------------------------------------------------------------------
// Task Understanding — lightweight LLM call to generate a structured
// understanding of the user's request for the execution confirmation card.
//
// Instead of parroting the user's raw text ("我理解你想让我处理这项任务：<原文>"),
// the LLM produces a structured summary that demonstrates genuine
// comprehension: task type, target, key dimensions, execution plan, etc.
//
// After user confirms, the structured instruction replaces the raw text as
// the agent loop input, giving the LLM a clearer directive.
//
// Token budget: ~400 input + ~200 output. Timeout: 12s.
// On failure: falls back to raw-text summary (current behavior).
// ---------------------------------------------------------------------------

// taskUnderstandingResult holds the LLM-generated structured understanding.
type taskUnderstandingResult struct {
	// TaskType is a short label for the task category (e.g. "信息搜集", "代码修改", "文件操作").
	TaskType string `json:"task_type"`

	// Summary is a one-sentence structured summary of the user's intent.
	Summary string `json:"summary"`

	// Goals are the extracted goals/objectives.
	Goals []string `json:"goals,omitempty"`

	// Constraints are any constraints or requirements mentioned.
	Constraints []string `json:"constraints,omitempty"`

	// ExecutionPlan is a brief ordered list of steps to accomplish the task.
	ExecutionPlan []string `json:"execution_plan,omitempty"`

	// EnhancedInstruction is a clear, actionable rewrite of the user's request
	// that the agent can use as its primary directive.
	EnhancedInstruction string `json:"enhanced_instruction"`
}

const taskUnderstandingSystemPrompt = `你是一个任务理解助手。用户提交了一个任务请求，你需要：

1. 理解用户的真实意图
2. 用结构化的方式重新表述，证明你理解了任务
3. 生成一个清晰的执行指令，供 AI 助手后续执行

输出严格 JSON 格式：
{
  "task_type": "任务类别（如：信息搜集、代码开发、文件处理、远程操作、数据分析、内容创作等）",
  "summary": "一句话结构化摘要，不要复述原文，要提炼核心意图",
  "goals": ["目标1", "目标2"],
  "constraints": ["约束或要求（如有）"],
  "execution_plan": ["步骤1", "步骤2", "步骤3"],
  "enhanced_instruction": "重写后的清晰执行指令，包含所有关键信息，供 AI 助手直接执行"
}

规则：
- summary 不要复述用户原文，要用结构化语言重新表述
- enhanced_instruction 要比用户原文更清晰、更具体、更可执行
- execution_plan 列出 2-5 个关键步骤
- 如果用户意图模糊，在 constraints 中标注"⚠️ 待确认：xxx"
- 只输出 JSON，不要输出其他内容`

// understandTaskWithLLM calls the LLM to generate a structured understanding
// of the user's task request. Returns nil on any failure (caller should
// fall back to raw-text summary).
func (h *IMMessageHandler) understandTaskWithLLM(userID, text string, intent taskIntentResult) *taskUnderstandingResult {
	if h == nil || h.app == nil {
		return nil
	}

	// Build user message with context.
	projectPath := strings.TrimSpace(h.app.GetCurrentProjectPath())

	userMsg := fmt.Sprintf("用户请求：%s", strings.TrimSpace(text))
	if projectPath != "" {
		userMsg += fmt.Sprintf("\n当前工作目录：%s", projectPath)
	}
	if label := confirmationTaskLabel(intent.Intent); label != "" {
		userMsg += fmt.Sprintf("\n初步分类：%s", label)
	}

	ctx := context.Background()
	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt: taskUnderstandingSystemPrompt,
		UserMessage:  userMsg,
		TimeoutSec:   12,
		Tag:          "task-understanding",
	})
	if err != nil {
		log.Printf("[task-understanding] LLM call failed for user %s: %v", userID, err)
		return nil
	}

	parsed, err := parseTaskUnderstandingResponse(result.Text)
	if err != nil {
		log.Printf("[task-understanding] parse failed for user %s: %v (raw=%q)", userID, err, truncateForLogGUI(result.Text, 100))
		return nil
	}

	log.Printf("[task-understanding] user=%s type=%q summary=%q plan=%d steps input=%d output=%d latency=%.1fs",
		userID, parsed.TaskType, truncateForLogGUI(parsed.Summary, 40),
		len(parsed.ExecutionPlan), result.InputTokens, result.OutputTokens, result.Latency.Seconds())

	return parsed
}

// parseTaskUnderstandingResponse extracts the structured understanding from
// the LLM's JSON response.
func parseTaskUnderstandingResponse(raw string) (*taskUnderstandingResult, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown code fences if present.
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Extract JSON object.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	raw = raw[start : end+1]

	var result taskUnderstandingResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("JSON parse: %w", err)
	}

	// Validate: at minimum we need a summary or enhanced_instruction.
	if strings.TrimSpace(result.Summary) == "" && strings.TrimSpace(result.EnhancedInstruction) == "" {
		return nil, fmt.Errorf("both summary and enhanced_instruction are empty")
	}

	return &result, nil
}

// formatTaskUnderstandingSummary formats the LLM understanding result into
// a human-readable summary for the confirmation card.
func formatTaskUnderstandingSummary(r *taskUnderstandingResult, projectPath string) string {
	if r == nil {
		return ""
	}

	var b strings.Builder

	// Task type + summary
	if r.TaskType != "" {
		fmt.Fprintf(&b, "任务类型：%s\n", r.TaskType)
	}
	if r.Summary != "" {
		fmt.Fprintf(&b, "任务理解：%s\n", r.Summary)
	}

	// Goals
	if len(r.Goals) > 0 {
		b.WriteString("目标：\n")
		for _, g := range r.Goals {
			fmt.Fprintf(&b, "  • %s\n", g)
		}
	}

	// Constraints
	if len(r.Constraints) > 0 {
		b.WriteString("约束/要求：\n")
		for _, c := range r.Constraints {
			fmt.Fprintf(&b, "  • %s\n", c)
		}
	}

	// Execution plan
	if len(r.ExecutionPlan) > 0 {
		b.WriteString("执行计划：\n")
		for i, step := range r.ExecutionPlan {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
		}
	}

	// Project path
	if projectPath != "" {
		fmt.Fprintf(&b, "默认工作目录：%s", projectPath)
	}

	return strings.TrimSpace(b.String())
}

// formatEnhancedInstruction builds the enhanced instruction text that
// replaces the raw user text after confirmation.
func formatEnhancedInstruction(r *taskUnderstandingResult) string {
	if r == nil || strings.TrimSpace(r.EnhancedInstruction) == "" {
		return ""
	}
	return strings.TrimSpace(r.EnhancedInstruction)
}
