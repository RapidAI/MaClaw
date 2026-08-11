package main

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestInitCodingRuntimeStoreTUIUsesDurableCorelibLedger(t *testing.T) {
	app := &TUIApp{}
	app.initCodingRuntimeStoreTUI(t.TempDir())
	if app.codingRuntimeStore == nil {
		t.Fatal("TUI did not initialize the shared coding runtime ledger")
	}
	defer app.codingRuntimeStore.Close()
	if _, err := app.codingRuntimeStore.CreateTask(codingruntime.Task{ProjectRef: "repo"}); err != nil {
		t.Fatal(err)
	}
}

func TestTUICancelPropagatesToBoundRuntimeTask(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "tui-parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "tui:test", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	cb := newTuiCallbacks(nil, nil)
	cb.bindRuntimeTask(store, *attempt)
	cb.Cancel()
	current, err := store.GetTask(task.TaskID)
	if err != nil || current.Status != codingruntime.TaskCancelled {
		t.Fatalf("task=%+v err=%v", current, err)
	}
	currentAttempt, err := store.GetAttempt(attempt.AttemptID)
	if err != nil || currentAttempt.Status != codingruntime.TaskCancelled || currentAttempt.SideEffectState != codingruntime.SideEffectUncertain {
		t.Fatalf("attempt=%+v err=%v", currentAttempt, err)
	}
}

func TestTUICancelAlsoInterruptsLiveDetachedChildContext(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "tui-parent-child", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "tui:test", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	children := &codingruntime.ChildExecutionRegistry{}
	childCtx, release := children.Begin(task.TaskID, "child")
	defer release()
	cb := newTuiCallbacks(nil, nil)
	cb.bindRuntimeTask(store, *attempt)
	cb.runtimeMu.Lock()
	cb.childExecutions = children
	cb.runtimeMu.Unlock()
	cb.Cancel()
	if err := childCtx.Err(); err != context.Canceled {
		t.Fatalf("detached child context = %v, want context.Canceled", err)
	}
}
