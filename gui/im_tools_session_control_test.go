package main

import (
	"strings"
	"testing"
	"time"
)

func TestRunSessionControlActionValidatesInputs(t *testing.T) {
	if got := runSessionControlAction(nil, "", sessionControlActionInterrupt); !strings.Contains(got, "session_id") {
		t.Fatalf("missing session id result = %q", got)
	}
	if got := runSessionControlAction(nil, "s1", sessionControlActionInterrupt); !strings.Contains(got, "未初始化") {
		t.Fatalf("nil manager result = %q", got)
	}
	manager := &RemoteSessionManager{sessions: map[string]*RemoteSession{}}
	if got := runSessionControlAction(manager, "s1", sessionControlActionUnknown); !strings.Contains(got, "action") {
		t.Fatalf("unknown action result = %q", got)
	}
}

func TestRunSessionControlActionInterruptAndKill(t *testing.T) {
	exec := newFakeExecutionHandle(1)
	manager := &RemoteSessionManager{sessions: map[string]*RemoteSession{
		"s1": {
			ID:        "s1",
			Status:    SessionBusy,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Exec:      exec,
		},
	}}

	if got := runSessionControlAction(manager, "s1", sessionControlActionInterrupt); !strings.Contains(got, "中断") {
		t.Fatalf("interrupt result = %q", got)
	}
	if exec.interruptCalls != 1 {
		t.Fatalf("interruptCalls = %d, want 1", exec.interruptCalls)
	}
	if got := runSessionControlAction(manager, "s1", sessionControlActionKill); !strings.Contains(got, "终止") {
		t.Fatalf("kill result = %q", got)
	}
	if exec.killCalls != 1 {
		t.Fatalf("killCalls = %d, want 1", exec.killCalls)
	}
}

func TestToolControlSessionUsesNormalizedAction(t *testing.T) {
	exec := newFakeExecutionHandle(1)
	h := newTestIMHandler(map[string]*RemoteSession{
		"s1": {
			ID:        "s1",
			Status:    SessionBusy,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Exec:      exec,
		},
	})

	got := h.toolControlSession(map[string]interface{}{
		"session_id": "s1",
		"action":     " INTERRUPT ",
	})
	if !strings.Contains(got, "control_session is disabled") || exec.interruptCalls != 0 {
		t.Fatalf("control_session should be disabled, result = %q interruptCalls=%d", got, exec.interruptCalls)
	}
}
