package guiapp

import (
	"fmt"
	"log"
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
		// The counter lives on the stored row. The composer may spell the
		// same session with the other slash.
		h.app.goalContinuation.resetActiveGoalNoToolCounter(h.queuedGoalTimerKey(msg.UserID))
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
	// Drop the row's timer and a timer left under the other spelling.
	// OnUserMessage also resets the no-tool counter on the row it is given.
	chatID := strings.TrimSpace(msg.UserID)
	goalKey := h.queuedGoalTimerKey(chatID)
	h.app.goalContinuation.OnUserMessage(goalKey)
	if goalKey != chatID {
		h.app.goalContinuation.CancelPending(chatID)
	}
}

// suspendQueuedGoalTurn parks the cooldown timer for one side command.
// The returned function puts that turn back when the same goal should still
// continue. /cancel and /reset move the schedule generation, so the parked
// turn is not re-armed over the stop.
func (h *IMMessageHandler) suspendQueuedGoalTurn(userID string) func() {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return func() {}
	}
	parkKey, turn, ok := h.parkQueuedGoalTurn(userID)
	if !ok {
		return func() {}
	}
	engine := h.app.goalContinuation
	return func() { engine.restoreSuspendedGoalTurn(parkKey, turn) }
}

// queuedGoalTimerKey is the id whose cooldown timer belongs to this chat.
// Scheduling follows the stored goal row, so a forward-slash composer and a
// backslash store key do not share a map entry. Session fences stay on the
// chat id.
func (h *IMMessageHandler) queuedGoalTimerKey(chatUserID string) string {
	chatUserID = strings.TrimSpace(chatUserID)
	goalKey, _ := h.continuationGoalKey(chatUserID)
	if goalKey == "" {
		return chatUserID
	}
	return goalKey
}

// parkQueuedGoalTurn stops the cooldown timer for this chat. The stored row's
// timer is the one to put back. A timer still armed under the other spelling
// is dropped, so the two spellings cannot both fire. When only the chat id
// has a timer, that one is parked.
func (h *IMMessageHandler) parkQueuedGoalTurn(chatUserID string) (string, suspendedGoalTurn, bool) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return "", suspendedGoalTurn{}, false
	}
	chatUserID = strings.TrimSpace(chatUserID)
	goalKey := h.queuedGoalTimerKey(chatUserID)
	engine := h.app.goalContinuation
	if turn, ok := engine.suspendQueuedTurn(goalKey); ok {
		if goalKey != chatUserID {
			engine.CancelPending(chatUserID)
		}
		return goalKey, turn, true
	}
	if goalKey != chatUserID {
		if turn, ok := engine.suspendQueuedTurn(chatUserID); ok {
			return chatUserID, turn, true
		}
	}
	return goalKey, suspendedGoalTurn{}, false
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
	chatID := strings.TrimSpace(msg.UserID)
	engine.resetActiveGoalNoToolCounter(h.queuedGoalTimerKey(chatID))
	parkKey, turn, ok := h.parkQueuedGoalTurn(chatID)
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
		engine.restoreSuspendedGoalTurn(parkKey, turn)
	}
}

type goalSlashVerb int

const (
	goalSlashHelp goalSlashVerb = iota
	goalSlashStatus
	goalSlashPause
	goalSlashResume
	goalSlashCancel
	goalSlashCreate
)

// splitGoalCommand separates the slash verb from the objective body.
// Help and status only produce a reply. The other verbs change the goal row.
func splitGoalCommand(trimmed string) (verb goalSlashVerb, body string) {
	body = strings.TrimSpace(trimmed)
	if len(body) >= 5 && strings.EqualFold(body[:5], "/goal") {
		body = strings.TrimSpace(body[5:])
	}
	switch strings.ToLower(body) {
	case "":
		return goalSlashHelp, ""
	case "status", "get":
		return goalSlashStatus, body
	case "pause", "暂停":
		return goalSlashPause, body
	case "resume", "继续", "恢复":
		return goalSlashResume, body
	case "cancel", "clear", "取消":
		return goalSlashCancel, body
	default:
		return goalSlashCreate, body
	}
}

func goalSlashMutates(verb goalSlashVerb) bool {
	return verb == goalSlashPause || verb == goalSlashResume || verb == goalSlashCancel || verb == goalSlashCreate
}

// goalSlashSettled reports that the verb changed the row, or wrote a row that
// a second send would duplicate. A refusal such as "已有活跃目标" is not
// settled: the caller keeps the command so the person sees that reply.
func goalSlashSettled(verb goalSlashVerb, before, after *goal.Goal) bool {
	switch verb {
	case goalSlashPause:
		if after == nil || after.Status != goal.StatusPaused {
			return false
		}
		return before == nil || before.Status != goal.StatusPaused || before.GoalID != after.GoalID
	case goalSlashResume:
		return after != nil && after.Status == goal.StatusActive &&
			before != nil && before.Status == goal.StatusPaused && before.GoalID == after.GoalID
	case goalSlashCancel:
		return before != nil && after == nil
	case goalSlashCreate:
		if after == nil {
			return false
		}
		if before == nil || before.IsTerminal() || before.GoalID != after.GoalID {
			return true
		}
		return false
	default:
		return false
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
	verb, body := splitGoalCommand(trimmed)
	userID := strings.TrimSpace(msg.UserID)

	switch verb {
	case goalSlashHelp:
		return &IMAgentResponse{Text: goalCommandHelp()}
	case goalSlashStatus:
		return &IMAgentResponse{Text: h.goalGetForUser(userID)}
	case goalSlashPause:
		return &IMAgentResponse{Text: h.goalPauseForUser(userID)}
	case goalSlashResume:
		before := h.goalAddressFor(userID)
		wasPaused := before.goal != nil && before.goal.Status == goal.StatusPaused
		// Refuse before Resume. Leaving the goal active and unscheduled makes
		// the next /goal resume a no-op, because only a paused goal is armed.
		// The guard is the same one that refuses to schedule a continuation:
		// a leftover local project path is not an SSH session.
		if wasPaused {
			h.ensurePureCodingArmedForGoalContinuation(userID)
			if text, blocked := h.pauseGoalForUnarmedRemote(userID); blocked {
				return &IMAgentResponse{Text: "目标仍处于暂停。\n" + text}
			}
			h.noteGoalContinuePastUserStop(before.id)
		}
		respText := h.goalResumeForUser(userID)
		// Schedule only when this command actually resumed a paused goal.
		// An already active goal keeps the turn that is already queued.
		// The timer follows the stored row. The other spelling must not keep one.
		if wasPaused && h.app != nil && h.app.goalContinuation != nil {
			resumed := h.goalAddressFor(userID)
			if resumed.goal != nil && resumed.goal.Status == goal.StatusActive {
				h.ensurePureCodingArmedForGoalContinuation(userID)
				h.app.cancelOtherGoalBarTimers(resumed.id)
				h.scheduleGoalContinuationUnlessLive(resumed.id, resumed.goal.GoalID)
			}
		}
		return &IMAgentResponse{Text: respText}
	case goalSlashCancel:
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

	// The bar may already show this session under the other slash spelling.
	addr := h.goalAddressFor(userID)
	existing := addr.goal
	if existing != nil && !existing.IsTerminal() {
		return &IMAgentResponse{
			Error: fmt.Sprintf("已有活跃目标：%s\n状态：%s\n如需创建新目标，请先取消当前目标：/goal cancel",
				existing.Objective, existing.Status),
		}
	}
	writeID := userID
	if existing != nil && addr.store != nil && addr.id != "" {
		store = addr.store
		writeID = addr.id
	}

	// Create the goal
	g, err := store.Set(writeID, objective)
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
			store.UpdateStatus(writeID, g.GoalID, goal.StatusFailed, reason)
			if h.app != nil && h.app.goalContinuation != nil {
				h.app.cancelGoalBarTimers(writeID)
				if updated := store.Get(writeID); updated != nil {
					h.app.goalContinuation.emitGoalStateChanged(writeID, updated)
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
		h.app.goalContinuation.emitGoalStateChanged(writeID, g)
		h.scheduleGoalContinuationUnlessLive(writeID, g.GoalID)
	}

	// Progress, pause, continue, and cancel live on the chat goal bar.
	// The transcript keeps the goal checklist; this reply is only the ack.
	return &IMAgentResponse{Text: "目标已开始：" + strings.TrimSpace(g.Objective)}
}

// goalContinuationMustWait reports that this owner still holds a turn.
// A published loop is the usual case. The owner window also holds the
// session before that loop is published and after it is cleared, until
// the lock is released. A continuation fired into either gap waits on
// the owner lock until the lock timeout.
func (h *IMMessageHandler) goalContinuationMustWait(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || h.hasCancelledTaskBoundary(userID) {
		return false
	}
	if h.ownerTurnBlocksContinuation(userID) {
		return true
	}
	for _, id := range goalBarCandidateIDs(userID) {
		if id == userID || h.hasCancelledTaskBoundary(id) {
			continue
		}
		if h.ownerTurnBlocksContinuation(id) {
			return true
		}
	}
	return false
}

// ownerTurnBlocksContinuation reports that this exact session id still holds
// a turn. Pending text is the owner window: it is set when the lock is taken
// and cleared when the lock is released. A host continuation's own text does
// not count. Pause cancels that loop, and resume must still arm while the
// continuation unwinds.
func (h *IMMessageHandler) ownerTurnBlocksContinuation(userID string) bool {
	if h == nil {
		return false
	}
	if ctx := h.getSessionLoopCtx(userID); ctx != nil && !ctx.IsCancelled() {
		return true
	}
	if turn := h.inFlightTurnForUser(userID); turn != nil && turn.ctx != nil && !turn.ctx.IsCancelled() {
		return true
	}
	text := strings.TrimSpace(h.pendingForegroundText(userID))
	return text != "" && !goalContinuationTurn(text, "")
}

// scheduleGoalContinuationUnlessLive arms the next goal turn only when this
// owner is not inside a model loop. The owner lock wait is capped, so a
// continuation fired under a long coding turn times out instead of running.
// The live turn's post-loop schedule arms the goal when that turn ends.
func (h *IMMessageHandler) scheduleGoalContinuationUnlessLive(userID, goalID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	goalID = strings.TrimSpace(goalID)
	if userID == "" || goalID == "" {
		return
	}
	if h.goalContinuationMustWait(userID) {
		log.Printf("[goal] continuation waits for the live turn user=%s goal_id=%s", userID, goalID)
		return
	}
	h.app.goalContinuation.scheduleDelayed(userID, goalID)
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
