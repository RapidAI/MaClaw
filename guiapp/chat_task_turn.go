package guiapp

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

func (r *chatTaskRegistry) waitSessionIdle(sessionKey string, timeout time.Duration) {
	if r == nil {
		return
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		if r.activeCount(sessionKey) == 0 {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *chatTaskRegistry) openTaskForSteer(sessionKey, taskID, title string) (*chatTaskRecord, error) {
	if r == nil {
		return nil, fmt.Errorf("chat tasks are unavailable")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	taskID = strings.TrimSpace(taskID)
	title = strings.TrimSpace(title)
	r.mu.Lock()
	defer r.mu.Unlock()
	if taskID != "" {
		task := r.tasks[taskID]
		if task == nil || task.SessionKey != sessionKey || task.Status.terminal() {
			return nil, fmt.Errorf("that work is not running")
		}
		return task, nil
	}
	var matches []*chatTaskRecord
	for _, task := range r.tasks {
		if task.SessionKey != sessionKey || task.Status.terminal() {
			continue
		}
		if title == "" || strings.EqualFold(strings.TrimSpace(task.Title), title) {
			matches = append(matches, task)
		}
	}
	switch len(matches) {
	case 0:
		if title != "" {
			return nil, fmt.Errorf("no running work titled %s", title)
		}
		return nil, fmt.Errorf("nothing is running")
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, task := range matches {
			names = append(names, task.Title)
		}
		return nil, fmt.Errorf("more than one piece of work is running (%s); ask which one", strings.Join(names, ", "))
	}
}

func (h *IMMessageHandler) desktopChatTurn(msg IMUserMessage) bool {
	if h == nil || msg.IsBackground || isChatTaskMemoryOwner(msg.UserID) {
		return false
	}
	if platform := strings.TrimSpace(msg.Platform); platform != "" && platform != desktopPlatform {
		return false
	}
	sessionKey := strings.TrimSpace(msg.UserID)
	if sessionKey == "" {
		sessionKey = desktopUserID
	}
	return chatTaskSessionAllowed(sessionKey, h.sessionIsCodingWorkbench(sessionKey))
}

func desktopChatSessionKey(msg IMUserMessage) string {
	if key := strings.TrimSpace(msg.UserID); key != "" {
		return key
	}
	return desktopUserID
}

func desktopChatChinese(lang, text string) bool {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return false
	case appLanguageZhHans, appLanguageZhHant:
		return true
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func desktopChatStoppedText(chinese bool) string {
	if chinese {
		return "已停下。"
	}
	return "Stopped."
}

func desktopChatNothingRunningText(chinese bool) string {
	if chinese {
		return "没有正在做的事。"
	}
	return "Nothing is running."
}

func desktopChatStillStoppingText(chinese bool) string {
	if chinese {
		return "还在停，没有开始新的工作。"
	}
	return "Still stopping. Nothing new was started."
}

func desktopChatFollowUpText(chinese bool) string {
	if chinese {
		return "好，按这个补充继续。"
	}
	return "Okay, I'll keep going with that."
}

func looksLikeDesktopChatStop(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" || utf8.RuneCountInString(raw) > 24 {
		return false
	}
	trimmed := strings.ToLower(raw)
	trimmed = strings.Trim(trimmed, " \t\r\n.。!！?？,~～")
	switch trimmed {
	case "stop", "stop it", "stop all", "stop everything", "cancel", "cancel all", "cancel everything", "abort", "never mind", "nevermind", "forget it",
		"停", "停下", "停止", "停掉", "别做了", "不要了", "取消", "都取消", "全部停止", "全部停下", "都停下", "算了", "不用了", "别跑了", "停止所有", "取消所有", "都停", "全停", "停一下", "先停", "停止吧", "取消吧", "别继续了", "不要继续了":
		return true
	default:
		return false
	}
}

func looksLikeChatTaskFollowUp(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" || utf8.RuneCountInString(raw) > 80 {
		return false
	}
	if looksLikeDesktopChatStop(raw) || looksLikeChatTaskStatusQuestion(raw) {
		return false
	}
	lower := strings.ToLower(raw)
	for _, needle := range []string{"写一", "做一", "生成", "调研", "研究一下", "write a", "create a", "make a new", "research", "new task", "新任务"} {
		if strings.Contains(lower, needle) {
			return false
		}
	}
	for _, needle := range []string{"改成", "改为", "换成", "改一下", "不对", "错了", "补充", "再加上", "继续", "调整", "缩短", "别用", "不要用", "instead", "change it", "change that", "make it", "revise", "update it", "update that", "add that", "don't use", "keep it"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func chatTaskProgressMilestone(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || text == imHeartbeatMsg || strings.HasPrefix(text, "__") {
		return "", false
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "[status]") || strings.Contains(lower, "agent reasoning") || strings.Contains(lower, "命令仍在执行") || strings.Contains(lower, "running command") || strings.Contains(lower, "auto-extended") || strings.Contains(lower, "tool_started") {
		return "", false
	}
	if strings.HasPrefix(text, "$ ") || strings.HasPrefix(lower, "bash ") || strings.HasPrefix(lower, "running ") || strings.HasPrefix(lower, "executing ") {
		return "", false
	}
	return truncateChatTaskText(text, 240), true
}

func (h *IMMessageHandler) rejectForegroundWorkTool(policyUserID, name string) string {
	if h == nil {
		return ""
	}
	name = strings.TrimSpace(name)
	switch name {
	case "dispatch_background_task", "steer_background_task", "chat_task_request_input":
		return ""
	}
	loop := h.getSessionLoopCtx(policyUserID)
	if loop == nil || !loop.OfferChatTaskDispatch || strings.TrimSpace(loop.ChatTaskID) != "" {
		return ""
	}
	if isComputerUseToolDefinition(name) {
		return "Desktop or vision work cannot run in this chat turn. Call dispatch_background_task with the user's goal, reply with one plain sentence, and stop."
	}
	loop.mu.Lock()
	loop.ForegroundToolRounds++
	rounds := loop.ForegroundToolRounds
	loop.mu.Unlock()
	if rounds > desktopChatInlineToolRounds {
		return "This chat turn already did its quick lookups. Call dispatch_background_task with the remaining work, reply with one plain sentence, and stop."
	}
	return ""
}

func (h *IMMessageHandler) tryDesktopChatStopReply(msg IMUserMessage) (*IMAgentResponse, bool) {
	if !h.desktopChatTurn(msg) || !looksLikeDesktopChatStop(msg.Text) {
		return nil, false
	}
	sessionKey := desktopChatSessionKey(msg)
	if h.peekMailboxDraftID(sessionKey) != "" && mailbox.IsCancelText(msg.Text) {
		return nil, false
	}
	chinese := desktopChatChinese(msg.Lang, msg.Text)
	reg := h.ensureChatTasks()
	running := 0
	if reg != nil {
		running += reg.activeCount(sessionKey)
	}
	if loop := h.getSessionLoopCtx(sessionKey); loop != nil && !loop.IsCancelled() && strings.TrimSpace(loop.ChatTaskID) == "" {
		running++
	}
	_ = h.cancelCurrentTaskForUser(sessionKey, msg.Lang)
	left := 0
	if reg != nil {
		left = reg.activeCount(sessionKey)
	}
	var text string
	switch {
	case left > 0:
		text = desktopChatStillStoppingText(chinese)
	case running == 0:
		text = desktopChatNothingRunningText(chinese)
	default:
		text = desktopChatStoppedText(chinese)
	}
	return &IMAgentResponse{
		Text:           text,
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_stop",
	}, true
}

func (h *IMMessageHandler) tryChatTaskFollowUp(msg IMUserMessage) (*IMAgentResponse, bool) {
	if !h.desktopChatTurn(msg) || !looksLikeChatTaskFollowUp(msg.Text) {
		return nil, false
	}
	sessionKey := desktopChatSessionKey(msg)
	reg := h.ensureChatTasks()
	if reg == nil {
		return nil, false
	}
	open := 0
	for _, task := range reg.List(sessionKey) {
		if chatTaskStatusOpen(task.Status) {
			open++
		}
	}
	if open != 1 {
		return nil, false
	}
	task, err := reg.openTaskForSteer(sessionKey, "", "")
	if err != nil {
		return nil, false
	}
	if err := reg.Steer(task.ID, strings.TrimSpace(msg.Text)); err != nil {
		return nil, false
	}
	return &IMAgentResponse{
		Text:           desktopChatFollowUpText(desktopChatChinese(msg.Lang, msg.Text)),
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_follow_up",
	}, true
}
