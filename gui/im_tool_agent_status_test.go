package main

import (
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCollectRuntimeStatusForOwnerDoesNotExposeOtherCurrentLoop(t *testing.T) {
	h := &IMMessageHandler{}
	desktopCtx := NewLoopContext("desktop", 1, nil)
	desktopCtx.StartedAt = desktopCtx.StartedAt.Add(-10)
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.userText = "desktop secret task"
	state.stateMu.Unlock()
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserText = "desktop secret task"
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	got := h.collectRuntimeStatusForOwner("remote:mobile")
	if got.MainAgentRunning || got.MainAgentTask != "" {
		t.Fatalf("remote owner saw desktop main-agent state: %+v", got)
	}

	got = h.collectRuntimeStatusForOwner(desktopUserID)
	if !got.MainAgentRunning || got.MainAgentTask != "desktop secret task" {
		t.Fatalf("desktop owner did not see its own main-agent state: %+v", got)
	}
}

func TestCollectRuntimeStatusWithoutOwnerDoesNotExposeLegacyCurrentLoop(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx(desktopUserID, ctx)
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.userText = "desktop secret task"
	state.stateMu.Unlock()
	h.globalLoopMu.Lock()
	h.currentLoopCtx = ctx
	h.lastUserText = "desktop secret task"
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	got := h.collectRuntimeStatus()
	if got.MainAgentRunning || got.MainAgentTask != "" {
		t.Fatalf("ownerless status inherited legacy loop: %+v", got)
	}
}

func TestToolAgentStatusUsesHiddenRuntimeOwner(t *testing.T) {
	h := &IMMessageHandler{}
	desktopCtx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.userText = "desktop secret task"
	state.stateMu.Unlock()
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserText = "desktop secret task"
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	out := h.toolAgentStatus(map[string]interface{}{
		"category":                       "main_agent",
		registeredToolPolicyOwnerIDField: "remote:mobile",
	})
	if strings.Contains(out, "desktop secret task") {
		t.Fatalf("agent_status leaked desktop task to remote owner: %s", out)
	}
}

func TestToolAgentStatusEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	h := &IMMessageHandler{}
	desktopCtx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.userText = "desktop secret task"
	state.stateMu.Unlock()
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserText = "desktop secret task"
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	out := h.toolAgentStatus(map[string]interface{}{
		"category":                       "main_agent",
		registeredToolPolicyOwnerIDField: "",
	})
	if !strings.Contains(out, "runtime owner is missing") {
		t.Fatalf("agent_status with empty runtime owner should fail closed, got: %s", out)
	}
	if strings.Contains(out, "desktop secret task") {
		t.Fatalf("agent_status leaked desktop task with empty runtime owner: %s", out)
	}
}

func TestToolAgentStatusCurrentRuntimeOwnerMissingFailsClosed(t *testing.T) {
	desktopCtx := NewLoopContext("desktop", 1, nil)
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-owner"}}}
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	state := h.getSessionLoop(desktopUserID)
	state.stateMu.Lock()
	state.userText = "desktop secret task"
	state.stateMu.Unlock()
	h.globalLoopMu.Lock()
	h.lastUserText = "desktop secret task"
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	out := h.toolAgentStatus(map[string]interface{}{"category": "main_agent"})
	if !strings.Contains(out, "runtime owner is missing") {
		t.Fatalf("agent_status with ownerless current runtime should fail closed, got: %s", out)
	}
	if strings.Contains(out, "desktop secret task") {
		t.Fatalf("agent_status leaked desktop task from ownerless current runtime: %s", out)
	}
}

func TestToolAgentStatusTaskIDUsesHiddenRuntimeOwner(t *testing.T) {
	mgr := coretool.NewLocalBackgroundTaskManager(t.TempDir())
	task, err := mgr.SubmitWithOwner("echo desktop secret", "", "command", "owner-a")
	if err != nil {
		t.Fatalf("SubmitWithOwner: %v", err)
	}
	h := &IMMessageHandler{localBgTaskMgr: mgr}

	out := h.toolAgentStatus(map[string]interface{}{
		"task_id":                        task.TaskID,
		registeredToolPolicyOwnerIDField: "owner-b",
	})
	if strings.Contains(out, "desktop secret") || strings.Contains(out, task.TaskID+" [") {
		t.Fatalf("agent_status task_id leaked cross-owner task: %s", out)
	}

	out = h.toolAgentStatus(map[string]interface{}{
		"task_id":                        task.TaskID,
		registeredToolPolicyOwnerIDField: "owner-a",
	})
	if !strings.Contains(out, task.TaskID) {
		t.Fatalf("same-owner task_id lookup should include task evidence, got: %s", out)
	}
}

func TestBtwAgentStatusUsesSubAgentOwnerForMainAgent(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	ctx.StartedAt = ctx.StartedAt.Add(-10)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "owner-a active task"
	state.stateMu.Unlock()

	callback := &btwCallbacks{subagent: &BtwSubAgent{handler: h, userID: "owner-a"}}
	out := callback.ExecuteTool("agent_status", `{"category":"main_agent"}`)
	if !strings.Contains(out, "**主 Agent**") || strings.Contains(out, "**Main Agent**") || !strings.Contains(out, "owner-a active task") {
		t.Fatalf("/btw agent_status did not return its owner's main-agent status: %s", out)
	}
}

func TestToolAgentStatusDefaultsMainAgentOutputToSimplifiedChinese(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "默认语言任务"
	state.stateMu.Unlock()

	out := h.toolAgentStatus(map[string]interface{}{
		"category":                       "main_agent",
		registeredToolPolicyOwnerIDField: "owner-a",
	})
	if !strings.Contains(out, "**主 Agent**") || strings.Contains(out, "**Main Agent**") {
		t.Fatalf("default agent_status language = %q; want Simplified Chinese", out)
	}
}

func TestBtwStatusCommandReturnsMainAgentStatusWithoutLLM(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "running without an LLM"
	state.stateMu.Unlock()

	response, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: "owner-a", Text: "/btw status"},
		"/btw status", nil, nil,
	)
	if !handled || response == nil || !strings.Contains(response.Text, "running without an LLM") {
		t.Fatalf("/btw status = %#v, handled=%v; want direct main-agent status", response, handled)
	}
}

func TestBtwStatusCommandSupportsLocalizedWhitespaceAndLanguage(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "繁體主 Agent 工作中"
	state.stateMu.Unlock()

	response, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: "owner-a", Lang: "zh-Hant", Text: "/btw　查看主 Agent 狀態"},
		"/btw　查看主 Agent 狀態", nil, nil,
	)
	if !handled || response == nil || !strings.Contains(response.Text, "繁體主 Agent 工作中") || !strings.Contains(response.Text, "/btw 查詢結果") {
		t.Fatalf("localized /btw status = %#v, handled=%v; want localized direct status", response, handled)
	}
}

func TestBtwAgentStatusCannotOverrideSubAgentOwner(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "owner-a private task"
	state.stateMu.Unlock()

	callback := &btwCallbacks{subagent: &BtwSubAgent{handler: h, userID: "owner-a"}}
	out := callback.ExecuteTool("agent_status", `{"category":"main_agent","_runtime_policy_owner_id":"owner-b"}`)
	if !strings.Contains(out, "owner-a private task") || strings.Contains(out, "owner-b") {
		t.Fatalf("/btw agent_status did not enforce the subagent owner: %s", out)
	}
}

func TestBtwStatusCommandLocalizesMainAgentStatus(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "review the current change"
	state.stateMu.Unlock()

	response, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: "owner-a", Lang: "en", Text: "/btw status"},
		"/btw status", nil, nil,
	)
	if !handled || response == nil || !strings.Contains(response.Text, "**Main Agent**: Running") || !strings.Contains(response.Text, "Task: review the current change") {
		t.Fatalf("English /btw status = %#v, handled=%v; want localized main-agent status", response, handled)
	}
}

func TestCollectMainAgentRuntimeStatusHandlesUnsetStartTime(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := &LoopContext{}
	h.setSessionLoopCtx("owner-a", ctx)
	state := h.getSessionLoop("owner-a")
	state.stateMu.Lock()
	state.userText = "task with unset start time"
	state.stateMu.Unlock()

	status := h.collectRuntimeStatusForOwner("owner-a")
	if !status.MainAgentRunning || status.MainAgentElapsed != 0 {
		t.Fatalf("main-agent status with unset StartedAt = %+v; want running with zero elapsed", status)
	}
}

func TestBtwMainAgentStatusQueryAcceptsUnicodeWhitespace(t *testing.T) {
	if !isBtwMainAgentStatusQuery("\u2003查看\u00a0主 Agent\u2009状态\n") {
		t.Fatal("Unicode whitespace should not prevent /btw main-agent status detection")
	}
}

func TestCollectMainAgentRuntimeStatusClampsFutureStartTime(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("future-start", 1, nil)
	ctx.StartedAt = time.Now().Add(time.Minute)
	h.setSessionLoopCtx("owner-a", ctx)

	status := h.collectRuntimeStatusForOwner("owner-a")
	if !status.MainAgentRunning || status.MainAgentElapsed != 0 {
		t.Fatalf("main-agent status with future StartedAt = %+v; want running with zero elapsed", status)
	}
}

func TestFormatMainAgentStatusUsesInitializingTaskFallback(t *testing.T) {
	status := formatMainAgentStatus(RuntimeStatus{MainAgentRunning: true}, "en")
	if !strings.Contains(status, "Task details are initializing.") {
		t.Fatalf("empty active task should use a clear fallback, got %q", status)
	}
}

func TestBtwMainAgentStatusResponseFailsClosedWithoutOwner(t *testing.T) {
	response := (&IMMessageHandler{}).btwMainAgentStatusResponse("", "en")
	if response == nil || response.Error == "" || response.Text != "" {
		t.Fatalf("ownerless /btw status = %#v; want a fail-closed error", response)
	}
}
