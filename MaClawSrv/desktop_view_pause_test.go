package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func withPauseStub(t *testing.T, states []map[string]any) *int {
	t.Helper()
	calls := 0
	old, oldIv, oldMax := desktopRemoteCtl, desktopPauseIntervals, desktopPauseMax
	desktopRemoteCtl = func(_ context.Context, _, _, _ string, request map[string]any) (map[string]any, error) {
		if op, _ := request["op"].(string); op != desktop.CtlBotState {
			return map[string]any{"ok": true}, nil
		}
		i := calls
		calls++
		if i >= len(states) {
			i = len(states) - 1
		}
		return states[i], nil
	}
	desktopPauseIntervals = []time.Duration{time.Millisecond}
	t.Cleanup(func() { desktopRemoteCtl, desktopPauseIntervals, desktopPauseMax = old, oldIv, oldMax })
	return &calls
}

func TestDesktopWaitForPersonPausesAndResumes(t *testing.T) {
	calls := withPauseStub(t, []map[string]any{{"viewer": true}, {"lease": "human"}, {"lease": "bot"}})
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1")}
	note, err := desktopWaitForPerson(context.Background(), scope)
	if err != nil || note != desktopResumeNote || *calls != 3 {
		t.Fatalf("note=%q err=%v calls=%d", note, err, *calls)
	}
	if !strings.Contains(note, "先截图") {
		t.Fatal("resume must ask for a fresh screenshot")
	}
}

func TestDesktopWaitForPersonRunsWhenNobodyIsThere(t *testing.T) {
	withPauseStub(t, []map[string]any{{"lease": "bot"}})
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1")}
	if note, err := desktopWaitForPerson(context.Background(), scope); err != nil || note != "" {
		t.Fatalf("note=%q err=%v", note, err)
	}
	// shared bots are not paused
	if note, err := desktopWaitForPerson(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "alice"}); err != nil || note != "" {
		t.Fatalf("shared note=%q err=%v", note, err)
	}
}

func TestDesktopWaitForPersonCapKeepsTheRunGoing(t *testing.T) {
	withPauseStub(t, []map[string]any{{"takeover": true, "yield": true}})
	desktopPauseMax = 5 * time.Millisecond
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1")}
	note, err := desktopWaitForPerson(context.Background(), scope)
	if err != nil || !strings.Contains(note, "不要结束任务") {
		t.Fatalf("note=%q err=%v", note, err)
	}
}

func TestDesktopWaitForPersonStopsOnCancel(t *testing.T) {
	withPauseStub(t, []map[string]any{{"viewer": true}})
	desktopPauseIntervals = []time.Duration{time.Hour}
	desktopPauseMax = 10 * time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1")}
	if _, err := desktopWaitForPerson(ctx, scope); err == nil {
		t.Fatal("canceled run kept waiting")
	}
}
