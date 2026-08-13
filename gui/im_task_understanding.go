package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// taskUnderstandingResult is a best-effort, reviewable summary for the legacy
// execution confirmation card. It is never required to start the main agent.
type taskUnderstandingResult struct {
	TaskType            string   `json:"task_type"`
	Summary             string   `json:"summary"`
	Goals               []string `json:"goals,omitempty"`
	Constraints         []string `json:"constraints,omitempty"`
	ExecutionPlan       []string `json:"execution_plan,omitempty"`
	EnhancedInstruction string   `json:"enhanced_instruction"`
}

// Keep this prompt deliberately short: the result only enriches a UI card.
const taskUnderstandingSimplifiedPrompt = `理解用户任务，输出 JSON：{"task_type":"类别","summary":"一句话摘要","execution_plan":["步骤1","步骤2"],"enhanced_instruction":"清晰执行指令"}
只输出 JSON。`

// understandTaskWithLLM uses one lightweight, bounded attempt. A slow or
// unavailable model must not delay the first visible response merely to build
// a confirmation card; failure deliberately falls through to the normal agent.
func (h *IMMessageHandler) understandTaskWithLLM(userID, text string, intent taskIntentResult) *taskUnderstandingResult {
	if h == nil || h.app == nil {
		return nil
	}

	userMsg := "用户请求：" + strings.TrimSpace(text)
	if projectPath := strings.TrimSpace(h.effectiveWorkingDirForUser(userID)); projectPath != "" {
		userMsg += "\n当前工作目录：" + projectPath
	}
	if label := confirmationTaskLabel(intent.Intent); label != "" {
		userMsg += "\n初步分类：" + label
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-understanding-fast", OwnerID: userID})
	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      taskUnderstandingSimplifiedPrompt,
		UserMessage:       userMsg,
		TimeoutSec:        2,
		Tag:               "task-understanding-fast",
		PreferLightweight: true,
	})
	if err != nil {
		log.Printf("[task-understanding] fast attempt failed for user %s: %v", userID, err)
		return nil
	}
	parsed, err := parseTaskUnderstandingResponse(result.Text)
	if err != nil {
		log.Printf("[task-understanding] fast attempt parse failed for user %s: %v (raw_len=%d)", userID, err, len([]rune(result.Text)))
		return nil
	}
	log.Printf("[task-understanding] user=%s type=%q summary_len=%d plan=%d steps input=%d output=%d latency=%.1fs",
		userID, parsed.TaskType, len([]rune(parsed.Summary)), len(parsed.ExecutionPlan), result.InputTokens, result.OutputTokens, result.Latency.Seconds())
	return parsed
}

func parseTaskUnderstandingResponse(raw string) (*taskUnderstandingResult, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty response")
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	var result taskUnderstandingResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("JSON parse: %w", err)
	}
	if !hasDisplayableTaskUnderstanding(&result) {
		return nil, fmt.Errorf("task understanding contains no usable content")
	}
	return &result, nil
}

func formatTaskUnderstandingSummary(r *taskUnderstandingResult, projectPath string) string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if value := strings.TrimSpace(r.TaskType); value != "" {
		b.WriteString("任务类型：" + value + "\n")
	}
	if value := firstNonEmptyTaskUnderstandingText(r.Summary, r.EnhancedInstruction); value != "" {
		b.WriteString("任务理解：" + value + "\n")
	}
	if len(r.Goals) > 0 {
		b.WriteString("目标：\n")
		for _, value := range r.Goals {
			if value = strings.TrimSpace(value); value != "" {
				b.WriteString("  - " + value + "\n")
			}
		}
	}
	if len(r.Constraints) > 0 {
		b.WriteString("约束/要求：\n")
		for _, value := range r.Constraints {
			if value = strings.TrimSpace(value); value != "" {
				b.WriteString("  - " + value + "\n")
			}
		}
	}
	if len(r.ExecutionPlan) > 0 {
		b.WriteString("执行计划：\n")
		for i, value := range r.ExecutionPlan {
			if value = strings.TrimSpace(value); value != "" {
				fmt.Fprintf(&b, "  %d. %s\n", i+1, value)
			}
		}
	}
	if projectPath = strings.TrimSpace(projectPath); projectPath != "" {
		b.WriteString("默认工作目录：" + projectPath)
	}
	return strings.TrimSpace(b.String())
}

func hasDisplayableTaskUnderstanding(r *taskUnderstandingResult) bool {
	return r != nil && (firstNonEmptyTaskUnderstandingText(r.Summary, r.EnhancedInstruction) != "" || len(r.Goals) > 0 || len(r.Constraints) > 0 || len(r.ExecutionPlan) > 0)
}

func firstNonEmptyTaskUnderstandingText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func formatEnhancedInstruction(r *taskUnderstandingResult) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.EnhancedInstruction)
}
