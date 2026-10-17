package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func publishDesktopChatLoop(h *IMMessageHandler, loop *LoopContext, userText string) {
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.loopCtx = loop
	state.userText = userText
	state.stateMu.Unlock()
}

func startHeldChatTask(t *testing.T, h *IMMessageHandler) (release func()) {
	t.Helper()
	reg := h.ensureChatTasks()
	hold := make(chan struct{})
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-hold
		return chatTaskRunResult{Text: "should not be delivered"}
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "keep going", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, snap.ID, chatTaskRunning)
	return func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	}
}

func TestWorkStopFastPathSkipsTheClassifier(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	h.workStopClassifier = func(string) (string, error) {
		t.Fatal("an exact stop called the classifier")
		return "", nil
	}
	resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "都停下", Lang: "zh-Hans"})
	if !handled || resp == nil || resp.Text != "已停下。" {
		t.Fatalf("stop = handled:%v resp:%+v", handled, resp)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
		t.Fatal("tasks still active")
	}
}

func TestWorkStopClassifierHandlesPhrasingTheListMisses(t *testing.T) {
	classify := func(text string) (string, error) {
		switch strings.TrimSpace(text) {
		case "算了别弄了", "yamete", "hold on, don't post that":
			return `{"intent":"stop"}`, nil
		case "hold on":
			return `{"intent":"pause"}`, nil
		case "stop using bold text", "stop the music in the video I'm editing":
			return `{"intent":"continue_other"}`, nil
		default:
			return "", fmt.Errorf("unexpected %q", text)
		}
	}
	for _, text := range []string{"算了别弄了", "yamete", "hold on, don't post that"} {
		t.Run(text, func(t *testing.T) {
			h, _ := testChatTaskHandler(t)
			release := startHeldChatTask(t, h)
			defer release()
			h.workStopClassifier = classify
			resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: text, Lang: "zh-Hans"})
			if !handled || resp == nil || resp.Text != "已停下。" {
				t.Fatalf("handled:%v resp:%+v", handled, resp)
			}
			if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
				t.Fatal("tasks still active")
			}
		})
	}
	t.Run("pause", func(t *testing.T) {
		h, _ := testChatTaskHandler(t)
		release := startHeldChatTask(t, h)
		defer release()
		h.workStopClassifier = classify
		paused, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "hold on", Lang: "zh-Hans"})
		if !handled || paused == nil || paused.Text != desktopChatPauseAskText(true) || strings.Contains(paused.Text, "已停下") {
			t.Fatalf("handled:%v resp:%+v", handled, paused)
		}
		if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
			t.Fatal("pause left the task running")
		}
	})
	for _, text := range []string{"stop using bold text", "stop the music in the video I'm editing"} {
		t.Run(text, func(t *testing.T) {
			h, _ := testChatTaskHandler(t)
			release := startHeldChatTask(t, h)
			defer release()
			h.workStopClassifier = classify
			resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: text, Lang: "en"})
			if handled || resp != nil {
				t.Fatalf("treated as a stop: %+v", resp)
			}
			if h.ensureChatTasks().activeCount(desktopUserID) != 1 {
				t.Fatalf("content edit cancelled the task: %d", h.ensureChatTasks().activeCount(desktopUserID))
			}
		})
	}
}

func TestWorkStopClassifierErrorLeavesTheLineForTheLoop(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	h.workStopClassifier = func(string) (string, error) {
		return "", fmt.Errorf("timeout")
	}
	resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "算了别弄了", Lang: "zh-Hans"})
	if handled || resp != nil {
		t.Fatalf("error was handled: %+v", resp)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 1 {
		t.Fatal("a failed classification cancelled the task")
	}
}

func TestWorkStopClassifierStaysQuietWhenNothingIsRunning(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	called := false
	h.workStopClassifier = func(string) (string, error) {
		called = true
		return `{"intent":"stop"}`, nil
	}
	resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "yamete", Lang: "en"})
	if handled || resp != nil || called {
		t.Fatalf("idle classify handled:%v called:%v resp:%+v", handled, called, resp)
	}
}

func TestStopAllWorkToolCancelsAndReportsIdle(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	publishDesktopChatLoop(h, loop, "算了别弄了")
	result, ok := h.executeChatTaskToolIfAny(desktopUserID, "stop_all_work", nil)
	if !ok || result.Outcome != toolOutcomeSucceeded || result.ToolName != "stop_all_work" {
		t.Fatalf("tool = ok:%v %+v", ok, result)
	}
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(strings.SplitN(result.Text, "\n", 2)[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["idle"] != true || payload["say"] != "已停下。" || payload["intent"] != "stop" {
		t.Fatalf("payload = %#v", payload)
	}
	if !loop.WorkStopRan || !loop.WorkStopIdle {
		t.Fatalf("loop flags ran:%v idle:%v", loop.WorkStopRan, loop.WorkStopIdle)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
		t.Fatal("tool left the task running")
	}
}

func TestStopAllWorkToolPauseAsksAndWorkersCannotCancel(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	loop := NewLoopContext("chat-pause", desktopChatForegroundIterationBudget, nil)
	publishDesktopChatLoop(h, loop, "hold on")
	paused, ok := h.executeChatTaskToolIfAny(desktopUserID, "stop_all_work", map[string]interface{}{"intent": "pause"})
	if !ok || paused.Outcome != toolOutcomeSucceeded {
		t.Fatalf("pause tool = ok:%v %+v", ok, paused)
	}
	body := strings.SplitN(paused.Text, "\n", 2)[0]
	if !strings.Contains(body, desktopChatPauseAskText(false)) || strings.Contains(body, "Stopped.") {
		t.Fatalf("pause payload = %s", paused.Text)
	}

	worker := NewLoopContext("worker", 4, nil)
	worker.ChatTaskID = "ct-worker"
	publishDesktopChatLoop(h, worker, "算了")
	rejected, ok := h.executeChatTaskToolIfAny(desktopUserID, "stop_all_work", nil)
	if !ok || rejected.Outcome == toolOutcomeSucceeded {
		t.Fatalf("background worker was allowed to cancel: %+v", rejected)
	}
}

func TestStopClaimWithoutCancelRunsTheCancel(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.WorkLiveAtStart = true
	publishDesktopChatLoop(h, loop, "算了别弄了")
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "算了别弄了", Lang: "zh-Hans"}
	out := h.correctUnbackedStopClaim(msg, loop, &IMAgentResponse{Text: "好的，已停下，不会继续了。"})
	if out == nil || out.Text != "已停下。" || !loop.WorkStopRan {
		t.Fatalf("claim guard = %+v ran:%v", out, loop.WorkStopRan)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
		t.Fatal("unbacked stop claim left the task running")
	}
}

func TestStopClaimIgnoresAContentEdit(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	loop := NewLoopContext("chat-bold", desktopChatForegroundIterationBudget, nil)
	loop.WorkLiveAtStart = true
	publishDesktopChatLoop(h, loop, "stop using bold text")
	h.workStopClaimClassifier = func(string) (bool, error) { return false, nil }
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "stop using bold text", Lang: "en"}
	bold := h.correctUnbackedStopClaim(msg, loop, &IMAgentResponse{Text: "I'll stop using bold text."})
	if bold == nil || bold.Text != "I'll stop using bold text." || loop.WorkStopRan {
		t.Fatalf("content edit was cancelled: %+v ran:%v", bold, loop.WorkStopRan)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 1 {
		t.Fatal("content edit cancelled the task")
	}
}

func TestStopClaimInAnotherLanguageRunsTheCancel(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	release := startHeldChatTask(t, h)
	defer release()
	loop := NewLoopContext("chat-ja", desktopChatForegroundIterationBudget, nil)
	loop.WorkLiveAtStart = true
	publishDesktopChatLoop(h, loop, "やめて")
	h.workStopClaimClassifier = func(text string) (bool, error) {
		return strings.Contains(text, "やめました"), nil
	}
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "やめて", Lang: "ja"}
	ja := h.correctUnbackedStopClaim(msg, loop, &IMAgentResponse{Text: "やめました。"})
	if ja == nil || ja.Text != "Stopped." || !loop.WorkStopRan {
		t.Fatalf("japanese claim = %+v ran:%v", ja, loop.WorkStopRan)
	}
	if h.ensureChatTasks().activeCount(desktopUserID) != 0 {
		t.Fatal("japanese claim left the task running")
	}
}

func TestStopClaimWhileStillStoppingDoesNotSayStopped(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	loop := NewLoopContext("chat-late", desktopChatForegroundIterationBudget, nil)
	loop.WorkStopRan = true
	loop.WorkStopIdle = false
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "停下", Lang: "zh-Hans"}
	late := h.correctUnbackedStopClaim(msg, loop, &IMAgentResponse{Text: "已停下。"})
	if late == nil || late.Text != desktopChatStillStoppingText(true) {
		t.Fatalf("unverified claim = %+v", late)
	}
}

func TestParseWorkStopIntent(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"intent":"stop"}`, workStopIntentStop},
		{"```json\n{\"intent\":\"pause\"}\n```", workStopIntentPause},
		{`{"intent":"cancel"}`, workStopIntentStop},
		{`{"intent":"continue_other"}`, workStopIntentContinue},
		{`{"intent":"none"}`, workStopIntentContinue},
		{`{"intent":"ambiguous"}`, workStopIntentPause},
	}
	for _, tc := range cases {
		got, err := parseWorkStopIntent(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("%s -> %q %v", tc.raw, got, err)
		}
	}
	if _, err := parseWorkStopIntent("hello"); err == nil {
		t.Fatal("prose was accepted as an intent")
	}
	req := workStopClassifyRequest("yamete")
	if req.Role != maclawLLMRoleForeground || req.TimeoutSec > 3 || req.TimeoutSec <= 0 || !req.NoThinking || req.PreferLightweight {
		t.Fatalf("classify request = %+v", req)
	}
	if workStopClassifyTimeout > 3*time.Second {
		t.Fatalf("timeout = %s", workStopClassifyTimeout)
	}
}

func TestDesktopBotStopPaceBecomesTheCancelToken(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, desktopBotUnderstandRequest) (string, error) {
		return `{"told":"stopping","instruction":"keep going","pace":"stop","persona":""}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("算了别弄了", "plan", "zh-Hans", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "reply" || got.Told != "STOP_WORK" || got.Instruction != "" {
		t.Fatalf("stop pace = %#v", got)
	}
}
