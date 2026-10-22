package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// noteInboundUserMessageForGoal runs before an immediate command or the
// agent loop. It does not cancel the queued continuation. /goal only resets
// the active goal's no-tool counter. Pause and cancel stop the timer
// themselves. A rejected create, /help, and a short greeting leave the
// queued turn, including an unsent plan correction, in place. A message that
// passes the immediate replies parks that turn in beginQueuedGoalTurnHold.
func (h *IMMessageHandler) noteInboundUserMessageForGoal(msg IMUserMessage, trimmed string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	if msg.Platform == "goal-continuation" || msg.Platform == unattendedWorkflowPlatform || msg.Platform == sessionRoundContinuationPlatform {
		return
	}
	if classifyImmediateIMCommand(trimmed) == imCommandGoal {
		h.app.goalContinuation.resetActiveGoalNoToolCounter(msg.UserID)
	}
}

// preemptQueuedGoalTurn drops the queued continuation when the caller already
// knows this message keeps the session. The message handler parks first and
// restores on a reply that never enters the loop.
func (h *IMMessageHandler) preemptQueuedGoalTurn(msg IMUserMessage) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	if msg.Platform == "goal-continuation" || msg.Platform == unattendedWorkflowPlatform || msg.Platform == sessionRoundContinuationPlatform {
		return
	}
	h.app.goalContinuation.OnUserMessage(msg.UserID)
}

// suspendQueuedGoalTurn parks the cooldown timer for one side command.
// The returned function puts that turn back when the same goal should still
// continue. /cancel and /reset move the schedule generation, so the parked
// turn is not re-armed over the stop.
func (h *IMMessageHandler) suspendQueuedGoalTurn(userID string) func() {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return func() {}
	}
	turn, ok := h.app.goalContinuation.suspendQueuedTurn(userID)
	if !ok {
		return func() {}
	}
	engine := h.app.goalContinuation
	return func() { engine.restoreSuspendedGoalTurn(userID, turn) }
}

// beginQueuedGoalTurnHold parks the cooldown timer once a message has passed
// the immediate replies. The returned function puts that turn back unless
// keep is true. keep is true when schedulePostLoopSideEffects runs, or when
// this message joined a loop that will schedule the next turn itself.
// A preflight reply, a cache hit, a lock timeout, or any return before that
// schedule passes keep false, so the same goal, including an unsent plan
// correction, is armed again.
func (h *IMMessageHandler) beginQueuedGoalTurnHold(msg IMUserMessage) func(keep bool) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return func(bool) {}
	}
	if msg.Platform == "goal-continuation" || msg.Platform == unattendedWorkflowPlatform || msg.Platform == sessionRoundContinuationPlatform {
		return func(bool) {}
	}
	engine := h.app.goalContinuation
	engine.resetActiveGoalNoToolCounter(msg.UserID)
	turn, ok := engine.suspendQueuedTurn(msg.UserID)
	if !ok {
		return func(bool) {}
	}
	ended := false
	return func(keep bool) {
		if ended {
			return
		}
		ended = true
		if keep {
			return
		}
		engine.restoreSuspendedGoalTurn(msg.UserID, turn)
	}
}

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
		before := h.getGoalStore().Get(userID)
		wasPaused := before != nil && before.Status == goal.StatusPaused
		// Refuse before Resume. Leaving the goal active and unscheduled makes
		// the next /goal resume a no-op, because only a paused goal is armed.
		// The guard is the same one that refuses to schedule a continuation:
		// a leftover local project path is not an SSH session.
		if wasPaused {
			h.ensurePureCodingArmedForGoalContinuation(userID)
			if text, blocked := h.pauseGoalForUnarmedRemote(userID); blocked {
				return &IMAgentResponse{Text: "目标仍处于暂停。\n" + text}
			}
			h.noteGoalContinuePastUserStop(userID)
		}
		respText := h.goalResumeForUser(userID)
		// Schedule only when this command actually resumed a paused goal.
		// An already active goal keeps the turn that is already queued.
		if wasPaused && h.app != nil && h.app.goalContinuation != nil {
			g := h.getGoalStore().Get(userID)
			if g != nil && g.Status == goal.StatusActive {
				h.ensurePureCodingArmedForGoalContinuation(userID)
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

	// Progress, pause, continue, and cancel live on the chat goal bar.
	// The transcript keeps the goal checklist; this reply is only the ack.
	return &IMAgentResponse{Text: "目标已开始：" + strings.TrimSpace(g.Objective)}
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
