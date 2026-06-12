package main

import (
	"strings"
	"testing"

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
