package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
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
	case appLanguageEnglish, appLanguageMongolian:
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
	return desktopChatCopy("", chinese, "Stopped.", "停了。")
}

// desktopChatCopy keeps the Chinese sentence when the turn is Chinese, and
// otherwise selects English or Mongolian. An empty lang stays English.
func desktopChatCopy(lang string, chinese bool, en, hans string) string {
	if chinese {
		return hans
	}
	return pickUI(lang, en, hans, hans)
}

func desktopChatNothingRunningText(chinese bool) string {
	return desktopChatCopy("", chinese, "Nothing is running.", "没在做事。")
}

func desktopChatStillStoppingText(chinese bool) string {
	return desktopChatCopy("", chinese, "Still stopping.", "还在停。")
}

func desktopChatFollowUpText(chinese bool) string {
	return desktopChatCopy("", chinese, "OK.", "好。")
}

// looksLikeDesktopChatStop is the instant path only. A short line made of
// stop words cancels without a model call. Every other phrasing is classified
// while work is active, and the chat loop can still call stop_all_work.
func looksLikeDesktopChatStop(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" || utf8.RuneCountInString(raw) > 24 {
		return false
	}
	trimmed := strings.ToLower(raw)
	trimmed = strings.Trim(trimmed, " \t\r\n.。!！?？,~～")
	switch trimmed {
	case "stop", "stop it", "stop all", "stop everything", "cancel", "cancel all", "cancel everything", "abort", "never mind", "nevermind", "forget it",
		"停", "停下", "停止", "停掉", "别做了", "别弄了", "不要了", "取消", "都取消", "全部停止", "全部停下", "都停下", "算了", "不用了", "别跑了", "停止所有", "取消所有", "都停", "全停", "停一下", "先停", "停止吧", "取消吧", "别继续了", "不要继续了", "不干了", "不做了", "别干了":
		return true
	default:
		return desktopChatStopParts(trimmed)
	}
}

// desktopChatStopParts accepts a short line made only of stop words, so
// 「停下，不干了。」 is a stop and is not left for the model to answer.
func desktopChatStopParts(text string) bool {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '.', '。', '!', '！', '?', '？', ',', '，', '、', '~', '～', '…', ':', '：', ';', '；', '\'', '"', '“', '”':
			return -1
		default:
			return r
		}
	}, strings.ToLower(strings.TrimSpace(text)))
	if compact == "" || utf8.RuneCountInString(compact) > 16 {
		return false
	}
	parts := []string{
		"stopeverything", "canceleverything", "nevermind", "forgetit", "stopall", "stopit", "cancelall",
		"全部停止", "全部停下", "停止所有", "取消所有", "别继续了", "不要继续了", "都停下", "都取消",
		"别做了", "别弄了", "不要了", "不干了", "不做了", "别干了", "别跑了", "不用了", "停一下", "停止吧", "取消吧",
		"先停", "算了", "停下", "停止", "停掉", "都停", "全停", "取消", "abort", "stop", "cancel", "停",
	}
	rest := compact
	used := false
	for rest != "" {
		matched := ""
		for _, part := range parts {
			if strings.HasPrefix(rest, part) && len(part) > len(matched) {
				matched = part
			}
		}
		if matched == "" {
			return false
		}
		rest = strings.TrimPrefix(rest, matched)
		used = true
	}
	return used
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
	if visible, ok := agent.LLMRetryLiveStatusText(text); ok {
		return truncateChatTaskText(visible, 240), true
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "[status]") || strings.Contains(lower, "agent reasoning") || strings.Contains(lower, "命令仍在执行") || strings.Contains(lower, "running command") || strings.Contains(lower, "auto-extended") || strings.Contains(lower, "tool_started") {
		return "", false
	}
	if strings.HasPrefix(text, "$ ") || strings.HasPrefix(lower, "bash ") || strings.HasPrefix(lower, "running ") || strings.HasPrefix(lower, "executing ") {
		return "", false
	}
	// The progress pusher's acknowledgement, elapsed heartbeat, and
	// "this is taking a while" notice are silence, not a step. Recording
	// them replaces the last real change and then freezes the card.
	if chatTaskProgressIsSilence(text) {
		return "", false
	}
	return truncateChatTaskText(chatTaskProgressStep(text), 240), true
}

// chatTaskProgressStep is the step name the card shows. The 〔进度〕 wrapper
// and the "仍在执行中" heartbeat prefix are the channel, not the step.
func chatTaskProgressStep(text string) string {
	body := stripChatTaskProgressLabel(text)
	for _, lang := range []string{"zh", "en"} {
		prefix := chatTaskFormatPrefix(i18n.T(i18n.MsgProgressStillWorking, lang))
		if prefix != "" && strings.HasPrefix(body, prefix) {
			if rest := strings.TrimSpace(strings.TrimPrefix(body, prefix)); rest != "" {
				return rest
			}
		}
	}
	if body != "" {
		return body
	}
	return strings.TrimSpace(text)
}

// chatTaskProgressIsSilence reports a progress-pusher line that does not name
// a step. A heartbeat that still carries a real summary is kept.
func chatTaskProgressIsSilence(text string) bool {
	body := stripChatTaskProgressLabel(text)
	if body == "" {
		return true
	}
	for _, lang := range []string{"zh", "en"} {
		if body == i18n.T(i18n.MsgProgressAck, lang) || body == i18n.T(i18n.MsgProgressLongTask, lang) {
			return true
		}
		prefix := chatTaskFormatPrefix(i18n.T(i18n.MsgProgressStillWorking, lang))
		rest := body
		if prefix != "" && strings.HasPrefix(body, prefix) {
			rest = strings.TrimSpace(strings.TrimPrefix(body, prefix))
		}
		if rest != body && chatTaskElapsedFiller(rest, lang) {
			return true
		}
		if chatTaskElapsedFiller(body, lang) {
			return true
		}
	}
	return false
}

func stripChatTaskProgressLabel(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range []string{
		i18n.T(i18n.MsgProgressStatusLabel, "zh"),
		i18n.T(i18n.MsgProgressStatusLabel, "en"),
		"〔进度〕", "[进度]", "【进度】", "〔Progress〕", "[Progress]", "【Progress】",
	} {
		if prefix != "" && strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return trimmed
}

func chatTaskFormatPrefix(template string) string {
	if i := strings.Index(template, "%s"); i >= 0 {
		return template[:i]
	}
	return ""
}

// interruptedChatTaskProgress is the step still worth showing after a restart.
// The interrupted status has its own explanation on the card. A pusher silence
// line, or a copy of that explanation stored in this field, is not a step.
func interruptedChatTaskProgress(progress string) string {
	progress = strings.TrimSpace(progress)
	if progress == "" || chatTaskProgressIsSilence(progress) || interruptedProgressExplanation(progress) {
		return ""
	}
	step := strings.TrimSpace(chatTaskProgressStep(progress))
	if step == "" || chatTaskProgressIsSilence(step) || interruptedProgressExplanation(step) {
		return ""
	}
	return step
}

func interruptedProgressExplanation(text string) bool {
	switch strings.TrimSpace(text) {
	case "Was running when the app stopped. It was not resumed.",
		"This was still running when the app stopped. It was not resumed.":
		return true
	default:
		return false
	}
}

func chatTaskElapsedFiller(text, lang string) bool {
	template := i18n.T(i18n.MsgMilestoneProcessingElapsed, lang)
	i := strings.Index(template, "%s")
	if i < 0 {
		return text == template
	}
	head, tail := template[:i], template[i+2:]
	if head == "" || !strings.HasPrefix(text, head) || !strings.HasSuffix(text, tail) {
		return false
	}
	return len(text) > len(head)+len(tail)
}

const (
	chatTaskToolDispatch     = "dispatch_background_task"
	chatTaskToolSteer        = "steer_background_task"
	chatTaskToolStop         = "stop_all_work"
	chatTaskToolRequestInput = "chat_task_request_input"
	// chatTaskHandoffRequiredMessage is the admission text once a desktop chat
	// turn has spent its inline lookups and has not handed the work off yet.
	chatTaskHandoffRequiredMessage = "This chat turn already did its quick lookups. Call dispatch_background_task with the remaining work, reply with one plain sentence, and stop."
	// chatTaskAlreadyHandedOffMessage is the admission text after dispatch or
	// steer has already succeeded. Telling the model to dispatch again starts
	// a second task.
	chatTaskAlreadyHandedOffMessage = "This chat turn already handed the work off. Reply with one plain sentence and stop."
	// chatTaskComputerUseHandoffMessage refuses desktop control on the chat
	// loop. The computer-use gate starts the helper. This refusal must not
	// close the rest of the turn.
	chatTaskComputerUseHandoffMessage = "Desktop control does not run in this chat turn. A desktop helper starts on its own when the request needs it. Keep doing the rest of the task here. Do not stop, and do not start another task for this refusal."
)

func (c *LoopContext) markChatTaskHandoffSent() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ChatTaskHandoffSent = true
	c.mu.Unlock()
}

func (c *LoopContext) chatTaskHandoffSent() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ChatTaskHandoffSent
}

func foregroundChatContinuationNote(userText string) string {
	minimal := "The rest continues; the result will come here."
	lang := "en"
	for _, r := range userText {
		if unicode.Is(unicode.Han, r) {
			minimal, lang = "剩下的接着做，做完发这里。", "zh-Hans"
			break
		}
	}
	return botNotice("chat-continuation", "notice", "The rest of this job keeps running in the background; the result will be posted in this chat when done.", userText, lang, minimal, 3*time.Second)
}

// foregroundChatNeedsHostHandoff is true when this desktop chat turn was
// closed on purpose and never started, steered, or stopped a task.
// A call count does not close it. A refused desktop-control call does not
// close it either: the computer-use gate starts that helper.
func (c *LoopContext) foregroundChatNeedsHostHandoff() bool {
	if c == nil || !c.OfferChatTaskDispatch || strings.TrimSpace(c.ChatTaskID) != "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.ChatTaskHandoffSent && c.ForegroundHandoffNeeded
}

// foregroundChatEndedUnfinished is a desktop chat turn that stopped while the
// task was still open. A full turn's iteration ceiling is that stop. A light
// lookup's ceiling is the end of the lookup. A reply that asks the user to
// send another message is unfinished on either profile. A worker does not
// start another worker.
func (c *LoopContext) foregroundChatEndedUnfinished(result agent.LoopResult) bool {
	if c == nil || !c.OfferChatTaskDispatch || strings.TrimSpace(c.ChatTaskID) != "" || c.chatTaskHandoffSent() {
		return false
	}
	if strings.TrimSpace(result.Error) == "max iterations reached" && !c.Runtime.Execution.IsLight() {
		if text := strings.TrimSpace(result.Text); text == "" || agent.AnswerHandsWorkBack(text) {
			return true
		}
	}
	return agent.AnswerHandsWorkBack(result.Text)
}

// chatTaskControlToolDefinitions is the conversation-control surface. These
// names are host actions, not capability grants, so a semantic plan must not
// be the thing that decides whether the model can see them.
func chatTaskControlToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef(chatTaskToolDispatch, "Hand work to a background task when it should keep running after this reply. After it succeeds, tell the user in one plain sentence and stop. A count of tool calls is not a reason to call this or to stop. Do not mention ids or tool names.",
			map[string]interface{}{
				"request": map[string]string{"type": "string", "description": "Self-contained task, including the user's goal and any constraints already known."},
				"title":   map[string]string{"type": "string", "description": "Short title the user can recognize. Optional."},
			}, []string{"request"}),
		toolDef(chatTaskToolSteer, "Send a follow-up or correction to work that is already running. Use this instead of starting another task. Omit task_id when only one piece of work is running.",
			map[string]interface{}{
				"note":    map[string]string{"type": "string", "description": "What changed, in the user's words."},
				"title":   map[string]string{"type": "string", "description": "Title of the running work when more than one is open. Optional."},
				"task_id": map[string]string{"type": "string", "description": "Internal id. Leave empty when title or the only running item is enough."},
			}, []string{"note"}),
		toolDef(chatTaskToolStop, "Stop every running task, desktop run, and foreground loop for this chat. Call it only when the person's own message asks to stop, cancel, or hold that work, in any language or phrasing, and call it before you say it stopped. Never call it because you are unsure how to continue, because a tool is missing, or merely to end your turn. Do not call it to change the content of the work, such as stopping bold text or stopping music inside a video being edited. If you are unsure whether the person meant the running work, not how to do it, call it with intent pause rather than continuing. The result has idle and say. Tell the user only say. Claim the work stopped only when idle is true.",
			map[string]interface{}{
				"intent": map[string]string{"type": "string", "description": "stop or pause. pause holds the work and asks what they meant. Omit it to stop."},
			}, nil),
		toolDef(chatTaskToolRequestInput, "Ask the user one question and wait. Only background work may call this, and only when a wrong guess would delete, send, purchase, or submit something, or when ambiguity would materially change the result. Otherwise decide and proceed. The answer continues the same work.",
			map[string]interface{}{
				"question": map[string]string{"type": "string", "description": "The one question the user must answer before the work can continue."},
			}, []string{"question"}),
	}
}

func chatTaskForegroundHandoffDefinitions() []map[string]interface{} {
	all := chatTaskControlToolDefinitions()
	out := make([]map[string]interface{}, 0, len(all))
	for _, def := range all {
		if extractToolName(def) == chatTaskToolRequestInput {
			continue
		}
		out = append(out, def)
	}
	return out
}

func chatTaskWorkerControlDefinitions() []map[string]interface{} {
	for _, def := range chatTaskControlToolDefinitions() {
		if extractToolName(def) == chatTaskToolRequestInput {
			return []map[string]interface{}{def}
		}
	}
	return nil
}

// chatTaskAfterHandoffDefinitions is the surface after this turn has already
// dispatched or steered. A second dispatch is closed. Steer and stop stay,
// so a correction to the running work still has a call.
func chatTaskAfterHandoffDefinitions() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, 2)
	for _, def := range chatTaskForegroundHandoffDefinitions() {
		if extractToolName(def) == chatTaskToolDispatch {
			continue
		}
		out = append(out, def)
	}
	return out
}

func (h *IMMessageHandler) rejectForegroundWorkTool(policyUserID, name string) string {
	if h == nil {
		return ""
	}
	name = strings.TrimSpace(name)
	switch name {
	case chatTaskToolDispatch, chatTaskToolSteer, chatTaskToolRequestInput, chatTaskToolStop:
		return ""
	}
	loop := h.getSessionLoopCtx(policyUserID)
	if loop == nil || !loop.OfferChatTaskDispatch || strings.TrimSpace(loop.ChatTaskID) != "" {
		return ""
	}
	loop.mu.Lock()
	handedOff := loop.ChatTaskHandoffSent
	loop.mu.Unlock()
	if handedOff {
		return chatTaskAlreadyHandedOffMessage
	}
	// The desktop gate already starts the helper. Closing this turn here
	// rejects the later real tool calls and starts a second worker.
	if isComputerUseToolDefinition(name) {
		return chatTaskComputerUseHandoffMessage
	}
	return ""
}

func (h *IMMessageHandler) tryDesktopChatStopReply(msg IMUserMessage) (*IMAgentResponse, bool) {
	if h == nil || !h.desktopChatTurn(msg) {
		return nil, false
	}
	// Goal and session-round continuations replay the open task inside a host
	// prompt. The person did not type that line, so it is not a stop request.
	if isGoalSystemContinuation(msg.Text) || isSessionRoundContinuation(msg.Text) {
		return nil, false
	}
	sessionKey := desktopChatSessionKey(msg)
	if h.peekMailboxDraftID(sessionKey) != "" && mailbox.IsCancelText(msg.Text) {
		return nil, false
	}
	if looksLikeDesktopChatStop(msg.Text) {
		return h.applyWorkStop(msg, workStopIntentStop, nil)
	}
	// Nothing is running, so a line that is not an instant stop stays in the
	// chat. While work is active, a foreground classification decides. A
	// timeout or a bad answer falls through to the loop, which has stop_all_work.
	if !h.sessionWorkLive(sessionKey) {
		return nil, false
	}
	intent, err := h.classifyWorkStopIntent(context.Background(), sessionKey, msg.Text)
	if err != nil || intent == "" || intent == workStopIntentContinue {
		if err != nil {
			log.Printf("[work-stop] classify failed, leaving the line for the chat loop: %v", err)
		}
		return nil, false
	}
	return h.applyWorkStop(msg, intent, nil)
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
		Text:           h.chatBotReply("supplement", msg.Text, msg.Lang, "follow-up", task.Title, desktopChatCopy(msg.Lang, desktopChatChinese(msg.Lang, msg.Text), "OK.", "好。")),
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_follow_up",
	}, true
}
