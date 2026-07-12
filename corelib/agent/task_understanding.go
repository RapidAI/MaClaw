package agent

// Task Understanding — standalone types, prompts, and pure functions migrated
// from gui/im_task_understanding.go as part of the agent-unification plan.
//
// The method understandTaskWithLLM (which depends on *IMMessageHandler) stays
// in gui/ as a thin wrapper.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TaskUnderstandingResult holds the LLM-generated structured understanding.
type TaskUnderstandingResult struct {
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

// TaskUnderstandingSystemPrompt is the full system prompt for the first attempt.
const TaskUnderstandingSystemPrompt = `你是一个任务理解助手。用户提交了一个任务请求，你需要：

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
- 如果用户意图模糊，在 constraints 中标注"待确认：xxx"
- 只输出 JSON，不要输出其他内容`

// TaskUnderstandingSimplifiedPrompt is a shorter, more forgiving prompt used
// as a retry when the first attempt fails (timeout or parse error).
const TaskUnderstandingSimplifiedPrompt = `理解用户任务，输出 JSON：
{"task_type":"类别","summary":"一句话摘要","execution_plan":["步骤1","步骤2"],"enhanced_instruction":"清晰执行指令"}
只输出 JSON。`

// ParseTaskUnderstandingResponse extracts the structured understanding from
// the LLM's JSON response.
func ParseTaskUnderstandingResponse(raw string) (*TaskUnderstandingResult, error) {
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

	var result TaskUnderstandingResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("JSON parse: %w", err)
	}

	// Validate: at minimum we need a summary or enhanced_instruction.
	if strings.TrimSpace(result.Summary) == "" && strings.TrimSpace(result.EnhancedInstruction) == "" {
		return nil, fmt.Errorf("both summary and enhanced_instruction are empty")
	}

	return &result, nil
}

// FormatTaskUnderstandingSummary formats the LLM understanding result into
// a human-readable summary for the confirmation card.
func FormatTaskUnderstandingSummary(r *TaskUnderstandingResult, projectPath string) string {
	if r == nil {
		return ""
	}

	var b strings.Builder

	if r.TaskType != "" {
		fmt.Fprintf(&b, "任务类型：%s\n", r.TaskType)
	}
	if r.Summary != "" {
		fmt.Fprintf(&b, "任务理解：%s\n", r.Summary)
	}

	if len(r.Goals) > 0 {
		b.WriteString("目标：\n")
		for _, g := range r.Goals {
			fmt.Fprintf(&b, "  • %s\n", g)
		}
	}

	if len(r.Constraints) > 0 {
		b.WriteString("约束/要求：\n")
		for _, c := range r.Constraints {
			fmt.Fprintf(&b, "  • %s\n", c)
		}
	}

	if len(r.ExecutionPlan) > 0 {
		b.WriteString("执行计划：\n")
		for i, step := range r.ExecutionPlan {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
		}
	}

	if projectPath != "" {
		fmt.Fprintf(&b, "项目目录：%s", projectPath)
	}

	return strings.TrimSpace(b.String())
}

// FormatEnhancedInstruction builds the enhanced instruction text that
// replaces the raw user text after confirmation.
func FormatEnhancedInstruction(r *TaskUnderstandingResult) string {
	if r == nil || strings.TrimSpace(r.EnhancedInstruction) == "" {
		return ""
	}
	return strings.TrimSpace(r.EnhancedInstruction)
}
