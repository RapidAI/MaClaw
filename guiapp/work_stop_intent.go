package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// A stop while work is running is not a phrase list. The list is only an
// instant path. Anything else is classified by the foreground model, and the
// chat loop also has stop_all_work for when that call does not return.
const (
	workStopIntentStop     = "stop"
	workStopIntentPause    = "pause"
	workStopIntentContinue = "continue_other"

	workStopClassifyTimeout = 2 * time.Second
	workStopClaimTimeout    = 1500 * time.Millisecond
)

const workStopClassifyPrompt = `Work is already running for this person. Classify the new message. Reply with one JSON object and nothing else: {"intent":"stop"} or {"intent":"pause"} or {"intent":"continue_other"}.

stop: they want that running work cancelled and not continued. Any language, including indirect phrasing. Examples of stop: "算了别弄了", "never mind that", "yamete", "hold on, don't post that".
pause: they want the work held, or it is ambiguous whether they mean the running work or something inside it. Prefer pause over continue_other when you are unsure.
continue_other: a new instruction, a correction that should keep going, or "stop" is about the content rather than the job. Examples of continue_other: "stop using bold text", "stop the music in the video I'm editing".

Do not explain.`

const workStopClaimPrompt = `Does this assistant message claim that the person's running work was stopped, cancelled, or will not continue? Reply with one JSON object and nothing else: {"claim":true} or {"claim":false}.
claim true only for a confirmation that the job itself stopped. claim false when they are changing the content of the work, such as stopping bold text or stopping music inside a video. If you are unsure, claim false.`

func workStopClassifyRequest(text string) LLMClassifyRequest {
	return LLMClassifyRequest{
		SystemPrompt:    workStopClassifyPrompt,
		UserMessage:     truncateRunes(strings.TrimSpace(text), 500),
		TimeoutSec:      2,
		Tag:             "work-stop-intent",
		Role:            maclawLLMRoleForeground,
		NoThinking:      true,
		MaxOutputTokens: 40,
	}
}

func parseWorkStopIntent(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty stop intent")
	}
	var payload struct {
		Intent string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start >= 0 && end > start {
			_ = json.Unmarshal([]byte(text[start:end+1]), &payload)
		}
	}
	intent := strings.ToLower(strings.TrimSpace(payload.Intent))
	if intent == "" {
		intent = strings.ToLower(text)
	}
	switch intent {
	case workStopIntentStop, "cancel", "abort":
		return workStopIntentStop, nil
	case workStopIntentPause, "hold", "unsure", "ambiguous":
		return workStopIntentPause, nil
	case workStopIntentContinue, "continue", "other", "none":
		return workStopIntentContinue, nil
	default:
		return "", fmt.Errorf("unrecognized stop intent %q", truncateRunes(text, 80))
	}
}

func (h *IMMessageHandler) classifyWorkStopIntent(ctx context.Context, sessionKey, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("empty message")
	}
	if h != nil && h.workStopClassifier != nil {
		raw, err := h.workStopClassifier(text)
		if err != nil {
			return "", err
		}
		return parseWorkStopIntent(raw)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, workStopClassifyTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "work-stop-intent", OwnerID: sessionKey})
	result, err := h.LLMClassify(ctx, workStopClassifyRequest(text))
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("empty stop classification")
	}
	intent, err := parseWorkStopIntent(result.Text)
	if err != nil {
		log.Printf("[work-stop-intent] unreadable result: %v", err)
	}
	return intent, err
}

// sessionWorkLeft reports work other than ignore. A chat turn that is itself
// the current loop is not work the person asked to stop.
func (h *IMMessageHandler) sessionWorkLeft(sessionKey string, ignore *LoopContext) bool {
	if h == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if reg := h.ensureChatTasks(); reg != nil && reg.activeCount(sessionKey) > 0 {
		return true
	}
	if loop := h.getSessionLoopCtx(sessionKey); loop != nil && loop != ignore && !loop.IsCancelled() && strings.TrimSpace(loop.ChatTaskID) == "" {
		return true
	}
	if h.app != nil {
		snap := h.app.GetDesktopBotBusy(sessionKey)
		if len(snap.Runs) > 0 || len(snap.Marks) > 0 {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) sessionWorkLive(sessionKey string) bool {
	return h.sessionWorkLeft(sessionKey, nil)
}

func (h *IMMessageHandler) cancelDesktopRunsForSession(sessionKey string) {
	if h == nil || h.app == nil {
		return
	}
	snap := h.app.GetDesktopBotBusy(sessionKey)
	seen := map[string]struct{}{}
	bots := append([]string{}, snap.Runs...)
	for _, mark := range snap.Marks {
		bots = append(bots, mark.BotID)
	}
	for _, botID := range bots {
		botID = strings.TrimSpace(botID)
		if botID == "" {
			continue
		}
		if _, ok := seen[botID]; ok {
			continue
		}
		seen[botID] = struct{}{}
		for _, run := range h.app.GetActiveDesktopRuns(botID) {
			if run == nil || strings.TrimSpace(run.RunID) == "" {
				continue
			}
			if err := h.app.CancelDesktopBotRun(botID, run.RunID); err != nil {
				log.Printf("[work-stop] cancel desktop run %s/%s: %v", botID, run.RunID, err)
			}
		}
		if err := h.app.ArmDesktopBotAdmitCancel(botID); err != nil {
			log.Printf("[work-stop] arm desktop cancel %s: %v", botID, err)
		}
	}
}

func (h *IMMessageHandler) sessionLoopUserText(userID string) string {
	if h == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	state := h.getSessionLoop(userID)
	state.stateMu.RLock()
	defer state.stateMu.RUnlock()
	return state.userText
}

func desktopChatPauseAskText(chinese bool) string {
	if chinese {
		return "我先停一下。是整个停下，还是只改正在做的内容？"
	}
	return "I've paused it. Should I stop the whole thing, or only change what's in it?"
}

// applyWorkStop cancels the session's work and speaks only from the verified
// state. ignore is the loop currently answering, which is not itself the work.
func (h *IMMessageHandler) applyWorkStop(msg IMUserMessage, intent string, ignore *LoopContext) (*IMAgentResponse, bool) {
	if h == nil {
		return nil, false
	}
	sessionKey := desktopChatSessionKey(msg)
	chinese := desktopChatChinese(msg.Lang, msg.Text)
	running := h.sessionWorkLeft(sessionKey, ignore)
	_ = h.cancelCurrentTaskForUser(sessionKey, msg.Lang)
	h.cancelDesktopRunsForSession(sessionKey)
	left := h.sessionWorkLeft(sessionKey, ignore)
	var text string
	switch {
	case left:
		text = desktopChatStillStoppingText(chinese)
	case !running:
		text = desktopChatNothingRunningText(chinese)
	case intent == workStopIntentPause:
		text = desktopChatPauseAskText(chinese)
	default:
		text = desktopChatStoppedText(chinese)
	}
	target := ignore
	if target == nil {
		target = h.getSessionLoopCtx(sessionKey)
	}
	if target != nil {
		target.WorkStopRan = true
		target.WorkStopIdle = !left
	}
	return &IMAgentResponse{
		Text:           text,
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_stop",
	}, true
}

func assistantClaimsStoppedFast(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" || utf8.RuneCountInString(raw) > 180 {
		return false
	}
	lower := strings.ToLower(raw)
	for _, skip := range []string{
		"stop using", "don't use", "do not use", "stop the music", "the music",
		"bold", "别用", "不要用", "粗体", "音乐", "别停", "不要停", "don't stop", "do not stop",
	} {
		if strings.Contains(lower, skip) {
			return false
		}
	}
	for _, hit := range []string{
		"已停下", "已经停", "不再继续", "停了",
		"i've stopped", "i have stopped", "i stopped", "work is stopped", "work has stopped",
		"cancelled the", "canceled the", "has been stopped", "isn't continuing", "is not continuing",
	} {
		if strings.Contains(lower, hit) {
			return true
		}
	}
	if lower == "stopped" || lower == "stopped." || strings.HasPrefix(lower, "stopped.") {
		return true
	}
	return false
}

func parseWorkStopClaim(raw string) (bool, error) {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, "`")
	var payload struct {
		Claim bool `json:"claim"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err == nil {
		return payload.Claim, nil
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start && json.Unmarshal([]byte(text[start:end+1]), &payload) == nil {
		return payload.Claim, nil
	}
	switch strings.ToLower(text) {
	case "true", "yes", "claim":
		return true, nil
	case "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("unrecognized stop claim %q", truncateRunes(text, 80))
	}
}

func (h *IMMessageHandler) assistantClaimsWorkStopped(text string) bool {
	if assistantClaimsStoppedFast(text) {
		return true
	}
	if utf8.RuneCountInString(strings.TrimSpace(text)) > 180 {
		return false
	}
	if h != nil && h.workStopClaimClassifier != nil {
		ok, err := h.workStopClaimClassifier(text)
		return err == nil && ok
	}
	if h == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), workStopClaimTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "work-stop-claim"})
	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:    workStopClaimPrompt,
		UserMessage:     truncateRunes(strings.TrimSpace(text), 500),
		TimeoutSec:      2,
		Tag:             "work-stop-claim",
		Role:            maclawLLMRoleForeground,
		NoThinking:      true,
		MaxOutputTokens: 30,
	})
	if err != nil || result == nil {
		return false
	}
	ok, err := parseWorkStopClaim(result.Text)
	return err == nil && ok
}

// correctUnbackedStopClaim replaces a reply that says the work stopped when
// this turn never cancelled it. The replacement is the verified idle state.
func (h *IMMessageHandler) correctUnbackedStopClaim(msg IMUserMessage, loop *LoopContext, resp *IMAgentResponse) *IMAgentResponse {
	if h == nil || resp == nil || loop == nil || !h.desktopChatTurn(msg) {
		return resp
	}
	if strings.TrimSpace(loop.ChatTaskID) != "" || loop.WorkflowAgentLoop {
		return resp
	}
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		return resp
	}
	if loop.WorkStopRan {
		if !loop.WorkStopIdle && h.assistantClaimsWorkStopped(text) {
			resp.Text = desktopChatStillStoppingText(desktopChatChinese(msg.Lang, msg.Text))
			resp.ResponseSource = "chat_task_stop"
		}
		return resp
	}
	if !loop.WorkLiveAtStart || !h.assistantClaimsWorkStopped(text) {
		return resp
	}
	corrected, ok := h.applyWorkStop(msg, workStopIntentStop, loop)
	if !ok || corrected == nil || strings.TrimSpace(corrected.Text) == "" {
		return resp
	}
	resp.Text = corrected.Text
	resp.ResponseSource = "chat_task_stop"
	return resp
}

func (h *IMMessageHandler) stopAllWorkTool(policyUserID string, args map[string]interface{}) toolExecutionResult {
	loop := h.getSessionLoopCtx(policyUserID)
	if loop != nil && strings.TrimSpace(loop.ChatTaskID) != "" {
		return chatTaskToolRejected("stop_all_work", "background work cannot cancel the chat; the chat calls stop_all_work")
	}
	intent := workStopIntentStop
	if strings.EqualFold(strings.TrimSpace(chatTaskArg(args, "intent")), workStopIntentPause) {
		intent = workStopIntentPause
	}
	sessionKey := strings.TrimSpace(policyUserID)
	if sessionKey == "" && loop != nil {
		sessionKey = strings.TrimSpace(loop.UserID)
	}
	text := h.sessionLoopUserText(sessionKey)
	lang := "en"
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			lang = "zh-Hans"
			break
		}
	}
	msg := IMUserMessage{
		UserID:   sessionKey,
		Platform: desktopPlatform,
		Text:     text,
		Lang:     lang,
	}
	resp, ok := h.applyWorkStop(msg, intent, loop)
	if !ok || resp == nil {
		return chatTaskToolRejected("stop_all_work", "stop did not run")
	}
	idle := loop != nil && loop.WorkStopIdle
	if loop == nil {
		idle = !h.sessionWorkLeft(sessionKey, nil)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"idle":   idle,
		"say":    resp.Text,
		"intent": intent,
	})
	if err != nil {
		payload = []byte(`{"idle":false,"say":"` + resp.Text + `"}`)
	}
	return toolExecutionResult{
		Text:     string(payload) + "\nTell the user only the say field, in their language. If idle is false, do not claim the work has stopped.",
		ToolName: "stop_all_work",
		Outcome:  toolOutcomeSucceeded,
	}
}

// ClassifyDesktopWorkStop is the desktop bot's fast check while a run is
// active. unknown means the call failed or timed out; the chat then continues
// and can still stop through the reading.
func (a *App) ClassifyDesktopWorkStop(text, lang string) string {
	text = strings.TrimSpace(text)
	if a == nil || text == "" {
		return "unknown"
	}
	hub := a.hubClient()
	if hub == nil {
		return "unknown"
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return "unknown"
	}
	if strings.TrimSpace(lang) != "" {
		text = "Language hint: " + strings.TrimSpace(lang) + "\nMessage:\n" + text
	}
	intent, err := handler.classifyWorkStopIntent(context.Background(), desktopUserID, text)
	if err != nil || intent == "" {
		return "unknown"
	}
	return intent
}
