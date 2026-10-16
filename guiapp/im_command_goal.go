package guiapp

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
//
// Session owner is msg.UserID so pure coding project tabs
// (desktop-user:<projectPath>) keep goals isolated per workbench.
func (h *IMMessageHandler) handleGoalCommand(msg IMUserMessage, trimmed string) *IMAgentResponse {
	// Parse sub-command: strip "/goal" prefix (case-insensitive).
	body := strings.TrimSpace(trimmed)
	if len(body) >= 5 && strings.EqualFold(body[:5], "/goal") {
		body = strings.TrimSpace(body[5:])
	}
	userID := strings.TrimSpace(msg.UserID)

	if body == "" {
		return &IMAgentResponse{Text: goalCommandHelp()}
	}

	// Sub-commands
	lower := strings.ToLower(body)
	switch {
	case lower == "status" || lower == "get":
		return &IMAgentResponse{Text: h.goalGetForUser(userID)}
	case lower == "pause" || lower == "暂停":
		return &IMAgentResponse{Text: h.goalPauseForUser(userID)}
	case lower == "resume" || lower == "继续" || lower == "恢复":
		respText := h.goalResumeForUser(userID)
		// After resume, schedule continuation if goal is active
		if h.app != nil && h.app.goalContinuation != nil {
			g := h.getGoalStore().Get(userID)
			if g != nil && g.Status == goal.StatusActive {
				h.ensurePureCodingArmedForGoalContinuation(userID)
				if h.isPureCodingWorkbenchSession(userID) && !h.hasPendingTemplateSubAgentExecution(userID) {
					mem := h.getStickyCodingWorkbenchMemory(userID)
					if strings.TrimSpace(mem.Kind) == "remote" {
						return &IMAgentResponse{Text: respText + "\n\n远程 SSH 未连接：请先重连远程编程环境后再 /goal resume。"}
					}
				}
				h.app.goalContinuation.scheduleDelayed(userID, g.GoalID)
			}
		}
		return &IMAgentResponse{Text: respText}
	case lower == "cancel" || lower == "clear" || lower == "取消":
		// Cancel pending continuation first
		if h.app != nil && h.app.goalContinuation != nil {
			h.app.goalContinuation.CancelPending(userID)
		}
		return &IMAgentResponse{Text: h.goalClearForUser(userID)}
	default:
		// Treat everything else as the objective for a new goal
		return h.handleGoalCreate(msg, body)
	}
}

// handleGoalCreate creates a new goal from the /goal command body.
func (h *IMMessageHandler) handleGoalCreate(msg IMUserMessage, objective string) *IMAgentResponse {
	store := h.getGoalStore()
	userID := strings.TrimSpace(msg.UserID)
	if userID == "" {
		return &IMAgentResponse{Error: "创建目标失败: 会话所有者为空"}
	}

	// Check for existing non-terminal goal
	existing := store.Get(userID)
	if existing != nil && !existing.IsTerminal() {
		return &IMAgentResponse{
			Error: fmt.Sprintf("已有活跃目标：%s\n状态：%s\n如需创建新目标，请先取消当前目标：/goal cancel",
				existing.Objective, existing.Status),
		}
	}

	// Create the goal
	g, err := store.Set(userID, objective)
	if err != nil {
		return &IMAgentResponse{Error: fmt.Sprintf("创建目标失败: %v", err)}
	}

	// Pure coding: keep session plan banner in sync with /goal objective.
	h.syncCodingWorkbenchSessionPlanFromGoal(userID, objective)
	// Ensure sticky pure-coding routing is still armed so the first continuation
	// does not fall through to the general agent loop after restart/history open.
	h.ensurePureCodingArmedForGoalContinuation(userID)

	// Remote pure coding with dead SSH cannot run the plan/execute loop.
	if h.isPureCodingWorkbenchSession(userID) && !h.hasPendingTemplateSubAgentExecution(userID) {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if strings.TrimSpace(mem.Kind) == "remote" {
			reason := "目标失败：远程 SSH 未连接。请先重连远程编程环境，再重新发送 /goal。"
			store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, reason)
			if h.app != nil && h.app.goalContinuation != nil {
				h.app.goalContinuation.CancelPending(userID)
				if updated := store.Get(userID); updated != nil {
					h.app.goalContinuation.emitGoalStateChanged(userID, updated)
				}
			}
			return &IMAgentResponse{Error: reason}
		}
	}

	// Schedule the first continuation (after the response is sent to user).
	// The first turn only decomposes the task. Later turns execute one step.
	// Continuation re-enters the pure-coding sticky path when armed, so long-running
	// goals execute in CodingSubAgent / RemoteCodingSubAgent rather than general chat.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.emitGoalStateChanged(userID, g)
		h.app.goalContinuation.scheduleDelayed(userID, g.GoalID)
	}

	var b strings.Builder
	b.WriteString("目标已创建，系统将先分解任务，再按步骤自动执行")
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("目标: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("最大轮次: %d\n", g.MaxTurns))
	if h.isPureCodingWorkbenchSession(userID) {
		b.WriteString("\n本目标已绑定到当前编程工作台，续接轮次将在编程环境中执行。\n")
		b.WriteString("会话目标（Session plan）已同步更新。\n")
	}
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

创建目标后，系统先分解任务，再按步骤自动执行，直到完成并给出结果，或因条件不足失败并返回错误。
在编程专用任务中同样支持：续接轮次走 CodingSubAgent / 远程编程工作台。
每轮之间有2秒冷却期，你可以随时发消息插入操作。`
}
