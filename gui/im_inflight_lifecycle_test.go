package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestInFlightLifecycleUsesProjectTabPathFromUserID(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)

	projectPath := `D:\tasks\recent-task-instance`
	userID := desktopUserID + ":" + projectPath
	h := &IMMessageHandler{memory: memory}

	lifecycle := h.newInFlightLifecycle(userID, "成都天气")
	lifecycle.SetOnce()

	task, gotProjectPath := memory.ConsumeInFlightTask(userID)
	if task != "成都天气" {
		t.Fatalf("task = %q, want 成都天气", task)
	}
	if gotProjectPath != projectPath {
		t.Fatalf("project path = %q, want %q", gotProjectPath, projectPath)
	}
}

func TestInFlightLifecycleDoesNotInventProjectPathForLocalUser(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)

	h := &IMMessageHandler{memory: memory}
	lifecycle := h.newInFlightLifecycle(desktopUserID, "本地任务")
	lifecycle.SetOnce()

	_, gotProjectPath := memory.ConsumeInFlightTask(desktopUserID)
	if gotProjectPath != "" {
		t.Fatalf("local project path = %q, want empty", gotProjectPath)
	}
}
