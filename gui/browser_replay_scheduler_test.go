package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestBrowserReplaySchedulerRejectsLegacyReplay(t *testing.T) {
	bridge := &browserReplaySchedulerBridge{}
	task := &scheduler.ScheduledTask{
		Action: `{"type":"browser_replay","flow_name":"old-flow"}`,
	}

	result, err, handled := bridge.handleScheduledReplay(task)
	if !handled {
		t.Fatalf("expected browser_replay task to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
}

func TestBrowserReplaySchedulerIgnoresNonReplayAction(t *testing.T) {
	bridge := &browserReplaySchedulerBridge{}
	task := &scheduler.ScheduledTask{Action: `{"type":"reminder","text":"ping"}`}

	result, err, handled := bridge.handleScheduledReplay(task)
	if handled || err != nil || result != "" {
		t.Fatalf("expected non replay action to pass through, got handled=%v result=%q err=%v", handled, result, err)
	}
}

func TestWrapExecutorWithReplayAllowsNilBridge(t *testing.T) {
	executor := wrapExecutorWithReplay(func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		return "default", nil
	}, nil)

	result, err := executor(context.Background(), &scheduler.ScheduledTask{Action: `{"type":"browser_replay","flow_name":"old-flow"}`})
	if err != nil || result != "default" {
		t.Fatalf("expected nil bridge to fall back to default executor, got result=%q err=%v", result, err)
	}
}
