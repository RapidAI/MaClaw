package main

import (
	"strings"
	"testing"
)

func TestMergeTaskPromptWithSendObserveText(t *testing.T) {
	if got := mergeTaskPromptWithSendObserveText("task prompt", ""); got != "task prompt" {
		t.Fatalf("empty supplemental text merge = %q", got)
	}
	got := mergeTaskPromptWithSendObserveText("task prompt", "extra")
	if !strings.Contains(got, "task prompt") || !strings.Contains(got, "补充说明") || !strings.Contains(got, "extra") {
		t.Fatalf("merged task prompt = %q", got)
	}
}

func TestEnrichSendAndObserveTextForTaskNoActiveOrchestrator(t *testing.T) {
	h := &IMMessageHandler{}
	if got := h.enrichSendAndObserveTextForTask("s1", "original"); got != "original" {
		t.Fatalf("text = %q, want original", got)
	}
}

func TestEnrichSendAndObserveTextForTaskPendingTask(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.Activate([]*TaskItem{{
		Index:       0,
		Title:       "Wire renderer",
		Description: "Move send observe prompt construction",
		Status:      TaskExecPending,
	}}, "requirements", "design", "/proj", "codex")
	h := &IMMessageHandler{
		lastUserID:               "u1",
		taskOrchestratorRegistry: registry,
	}

	got := h.enrichSendAndObserveTextForTask("s1", "extra context")
	if !strings.Contains(got, "Wire renderer") || !strings.Contains(got, "extra context") {
		t.Fatalf("enriched text missing task prompt or supplemental text:\n%s", got)
	}

	task := orch.CurrentTask()
	if task == nil {
		t.Fatal("current task is nil")
	}
	if task.SessionID != "s1" {
		t.Fatalf("SessionID = %q, want s1", task.SessionID)
	}
	if task.Status != TaskExecInProgress {
		t.Fatalf("Status = %q, want %q", task.Status, TaskExecInProgress)
	}
}

func TestEnrichSendAndObserveTextForTaskNonPendingTaskOnlyBindsSession(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task", Status: TaskExecPending}}, "", "", "/proj", "codex")
	orch.MarkCurrentStatus(TaskExecInProgress, "")
	h := &IMMessageHandler{
		lastUserID:               "u1",
		taskOrchestratorRegistry: registry,
	}

	got := h.enrichSendAndObserveTextForTask("s2", "original")
	if got != "original" {
		t.Fatalf("text = %q, want original", got)
	}
	task := orch.CurrentTask()
	if task == nil || task.SessionID != "s2" {
		t.Fatalf("task session = %#v, want s2", task)
	}
	if task.Status != TaskExecInProgress {
		t.Fatalf("Status = %q, want still in progress", task.Status)
	}
}
