package main

import "fmt"

const (
	defaultGoalText     = "完成用户请求"
	goalMaxChars        = 200
	goalTruncationMark  = "..."
	defaultAnchorTokens = 500
)

// GoalAnchor 在长程 Agent Loop 中周期性重新注入原始用户目标。
type GoalAnchor struct {
	originalGoal   string // 原始用户目标文本 (≤200 字符)
	anchorInterval int    // 锚定间隔 (默认 5 步)
	maxTokens      int    // 锚定内容 token 上限 (默认 500)
}

// NewGoalAnchor 从用户首条消息中提取目标。
// 空消息使用默认目标文本；超过 200 字符时截断并加省略标记。
func NewGoalAnchor(userText string, interval int) *GoalAnchor {
	goal := userText
	if goal == "" {
		goal = defaultGoalText
	}
	// 按 rune 截断以正确处理中文字符
	runes := []rune(goal)
	if len(runes) > goalMaxChars {
		goal = string(runes[:goalMaxChars]) + goalTruncationMark
	}
	if interval <= 0 {
		interval = 5
	}
	return &GoalAnchor{
		originalGoal:   goal,
		anchorInterval: interval,
		maxTokens:      defaultAnchorTokens,
	}
}

// ShouldAnchor 判断当前迭代是否需要锚定。
// 在 iteration > 0 且 iteration % N == 0 时返回 true。
func (g *GoalAnchor) ShouldAnchor(iteration int) bool {
	return iteration > 0 && iteration%g.anchorInterval == 0
}

// BuildAnchorContent 生成锚定内容，包含原始目标和进度摘要。
// progressSummary 来自 ProgressTracker，例如 "已完成 3/7 步 | 当前: 实现 DriftDetector | 剩余: 4 项"。
// 输出控制在 maxTokens (500) 以内。
func (g *GoalAnchor) BuildAnchorContent(progressSummary string) string {
	content := fmt.Sprintf("[🎯 目标锚定]\n原始目标: %s\n当前进度: %s\n[/目标锚定]", g.originalGoal, progressSummary)

	// Token 估算：混合中英文内容使用 len(content)/2 作为粗略估算
	estimatedTokens := len(content) / 2
	if estimatedTokens > g.maxTokens {
		// 截断 progressSummary 以满足 token 限制
		// 保留目标锚定框架和原始目标，压缩进度摘要
		maxSummaryBytes := (g.maxTokens * 2) - len("[🎯 目标锚定]\n原始目标: ") - len(g.originalGoal) - len("\n当前进度: \n[/目标锚定]")
		if maxSummaryBytes < 0 {
			maxSummaryBytes = 0
		}
		summaryRunes := []rune(progressSummary)
		if len(string(summaryRunes)) > maxSummaryBytes {
			// 按 rune 逐步截断
			truncated := ""
			for _, r := range summaryRunes {
				candidate := truncated + string(r)
				if len(candidate) > maxSummaryBytes {
					break
				}
				truncated = candidate
			}
			progressSummary = truncated + goalTruncationMark
		}
		content = fmt.Sprintf("[🎯 目标锚定]\n原始目标: %s\n当前进度: %s\n[/目标锚定]", g.originalGoal, progressSummary)
	}
	return content
}

// OriginalGoal 返回存储的原始目标文本（用于测试验证）。
func (g *GoalAnchor) OriginalGoal() string {
	return g.originalGoal
}
