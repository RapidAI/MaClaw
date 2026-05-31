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
		currentLoopCtx:           &LoopContext{Runtime: RuntimeContext{PolicyOwnerID: "u1"}},
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

func TestEnrichSendAndObserveTextForTaskUsesNextReadyTask(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Blocked task", Description: "wait", DependsOn: []int{1}},
		{Index: 1, Title: "Ready task", Description: "run first"},
	}, "requirements", "design", "/proj", "codex")
	h := &IMMessageHandler{
		currentLoopCtx:           &LoopContext{Runtime: RuntimeContext{PolicyOwnerID: "u1"}},
		taskOrchestratorRegistry: registry,
	}

	got := h.enrichSendAndObserveTextForTask("s-ready", "extra context")
	if strings.Contains(got, "Blocked task") || !strings.Contains(got, "Ready task") {
		t.Fatalf("enriched text should target ready task only:\n%s", got)
	}
	if orch.Tasks[0].SessionID != "" || orch.Tasks[0].Status != TaskExecPending {
		t.Fatalf("blocked task should remain untouched: %#v", orch.Tasks[0])
	}
	if orch.Tasks[1].SessionID != "s-ready" || orch.Tasks[1].Status != TaskExecInProgress {
		t.Fatalf("ready task should be bound and in progress: %#v", orch.Tasks[1])
	}
}

func TestEnrichSendAndObserveTextForTaskNonPendingTaskOnlyBindsSession(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task", Status: TaskExecPending}}, "", "", "/proj", "codex")
	orch.MarkCurrentStatus(TaskExecInProgress, "")
	h := &IMMessageHandler{
		currentLoopCtx:           &LoopContext{Runtime: RuntimeContext{PolicyOwnerID: "u1"}},
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

func TestEnrichSendAndObserveTextForTaskWithoutRuntimeOwnerDoesNotInheritLastUserID(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Leaked task", Status: TaskExecPending}}, "", "", "/proj", "codex")
	h := &IMMessageHandler{
		lastUserID:               "u1",
		taskOrchestratorRegistry: registry,
	}

	got := h.enrichSendAndObserveTextForTask("s-leak", "original")
	if got != "original" {
		t.Fatalf("send_and_observe without runtime owner must not inherit lastUserID task orchestration, got:\n%s", got)
	}
	if orch.CurrentTask().SessionID != "" {
		t.Fatalf("orchestrator should not be touched without runtime owner: %#v", orch.CurrentTask())
	}
}

func TestEnrichSendAndObserveTextForTaskUsesRuntimeOwner(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	desktopOrch := registry.GetOrCreate(desktopUserID)
	desktopOrch.Activate([]*TaskItem{{Index: 0, Title: "Desktop task", Status: TaskExecPending}}, "", "", "/proj", "codex")
	remoteOrch := registry.GetOrCreate("remote:mobile")
	remoteOrch.Activate([]*TaskItem{{Index: 0, Title: "Mobile task", Status: TaskExecPending}}, "", "", "/proj", "codex")
	h := &IMMessageHandler{
		lastUserID:               desktopUserID,
		currentLoopCtx:           &LoopContext{Runtime: RuntimeContext{RequestID: "req-mobile", PolicyOwnerID: "remote:mobile"}},
		taskOrchestratorRegistry: registry,
	}

	got := h.enrichSendAndObserveTextForTask("s-mobile", "extra context")
	if !strings.Contains(got, "Mobile task") || strings.Contains(got, "Desktop task") {
		t.Fatalf("enriched text should use runtime owner task only:\n%s", got)
	}
	if desktopOrch.CurrentTask().SessionID != "" {
		t.Fatalf("desktop orchestrator should not be touched: %#v", desktopOrch.CurrentTask())
	}
	if remoteOrch.CurrentTask().SessionID != "s-mobile" {
		t.Fatalf("remote orchestrator session = %#v", remoteOrch.CurrentTask())
	}
}
