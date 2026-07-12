package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// handleGoalCommand processes /goal slash commands.
//
// Usage:
//
//	/goal <objective>          — create a new goal
//	/goal status               — show current goal status
//	/goal pause                — pause active goal
//	/goal resume               — resume paused goal
//	/goal cancel               — cancel and clear goal
//	/goal                      — show help
func (h *IMMessageHandler) handleGoalCommand(msg IMUserMessage, trimmed string) *IMAgentResponse {
	// Parse sub-command: strip "/goal" prefix
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "/goal"))

	if body == "" {
		return &IMAgentResponse{Text: goalCommandHelp()}
	}

	// Sub-commands
	lower := strings.ToLower(body)
	switch {
	case lower == "status" || lower == "get":
		return &IMAgentResponse{Text: h.goalGet()}
	case lower == "pause" || lower == "暂停":
		return &IMAgentResponse{Text: h.goalPause()}
	case lower == "resume" || lower == "继续" || lower == "恢复":
		resp := h.goalResume()
		// After resume, schedule continuation if goal is active
		if h.app != nil && h.app.goalContinuation != nil {
			g := h.getGoalStore().Get(msg.UserID)
			if g != nil && g.Status == goal.StatusActive {
				h.app.goalContinuation.scheduleDelayed(msg.UserID, g.GoalID)
			}
		}
		return &IMAgentResponse{Text: resp}
	case lower == "cancel" || lower == "clear" || lower == "取消":
		// Cancel pending continuation first
		if h.app != nil && h.app.goalContinuation != nil {
			h.app.goalContinuation.CancelPending(msg.UserID)
		}
		return &IMAgentResponse{Text: h.goalClear()}
	default:
		// Treat everything else as the objective for a new goal
		return h.handleGoalCreate(msg, body)
	}
}

// handleGoalCreate creates a new goal from the /goal command body.
func (h *IMMessageHandler) handleGoalCreate(msg IMUserMessage, objective string) *IMAgentResponse {
	store := h.getGoalStore()

	// Check for existing non-terminal goal
	existing := store.Get(msg.UserID)
	if existing != nil && !existing.IsTerminal() {
		return &IMAgentResponse{
			Text: fmt.Sprintf("已有活跃目标：%s\n状态：%s\n\n如需创建新目标，请先取消当前目标：/goal cancel",
				existing.Objective, existing.Status),
		}
	}

	// Create the goal
	g, err := store.Set(msg.UserID, objective)
	if err != nil {
		return &IMAgentResponse{Error: fmt.Sprintf("创建目标失败: %v", err)}
	}

	// Schedule the first continuation (after the response is sent to user)
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.scheduleDelayed(msg.UserID, g.GoalID)
	}

	var b strings.Builder
	b.WriteString("目标已创建，系统将自动开始推进\n\n")
	b.WriteString(fmt.Sprintf("目标: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("最大轮次: %d\n", g.MaxTurns))
	b.WriteString("\n命令:\n")
	b.WriteString("  /goal status — 查看进度\n")
	b.WriteString("  /goal pause  — 暂停\n")
	b.WriteString("  /goal cancel — 取消\n")
	return &IMAgentResponse{Text: b.String()}
}

func goalCommandHelp() string {
	return `/goal — 持久化长时间运行目标

用法:
  /goal <目标描述>     创建新目标并自动开始推进
  /goal status        查看当前目标状态
  /goal pause         暂停目标（停止自动续接）
  /goal resume        恢复暂停的目标
  /goal cancel        取消并清除目标

示例:
  /goal 实现用户登录注册功能，包含JWT认证和邮箱验证
  /goal 搜集AI Agent最新论文并整理成综述报告

创建目标后，系统将自动持续推进直到目标达成或预算耗尽。
每轮之间有2秒冷却期，你可以随时发消息插入操作。`
}
