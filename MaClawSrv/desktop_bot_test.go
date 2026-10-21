package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotUserFollowsHubMetadata(t *testing.T) {
	if got := desktopBotUser("alice", map[string]string{"hub_desktop_bot": "bot_1"}); got != desktop.BotDesktopUser("alice", "bot_1") {
		t.Fatalf("isolated user %q", got)
	}
	if got := desktopBotUser("alice", map[string]string{}); got != "alice" {
		t.Fatalf("shared user %q", got)
	}
	if got := desktopBotUser("alice", map[string]string{"hub_desktop_bot": "bad id"}); got != "alice" {
		t.Fatalf("invalid bot %q", got)
	}
}

func TestRequestTakeoverMarksTheBotDisplayAndYields(t *testing.T) {
	var ops []string
	takeover := false
	old := desktopRemoteCtl
	desktopRemoteCtl = func(_ context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
		if userID != desktop.BotDesktopUser("alice", "bot_1") || display != "" {
			t.Errorf("ctl went to %q %q", userID, display)
		}
		op, _ := request["op"].(string)
		ops = append(ops, op)
		switch op {
		case desktop.CtlTakeoverBegin:
			takeover = true
		case desktop.CtlBotState:
			return map[string]any{"yield": takeover, "takeover": takeover, "reason": "login"}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	defer func() { desktopRemoteCtl = old }()
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1"), InstanceID: "inst-takeover-test"}
	if err := desktopBotYield(context.Background(), scope); err != nil {
		t.Fatalf("yield before takeover: %v", err)
	}
	text, err := requestDesktopTakeover(context.Background(), scope, "请登录")
	if err != nil || !strings.Contains(text, "End your turn") {
		t.Fatalf("takeover %v %q", err, text)
	}
	handoff, attention := takeDesktopTurn(scope.TenantID, scope.UserID, scope.InstanceID)
	if !handoff || attention != "请登录" {
		t.Fatalf("handoff %v attention %q", handoff, attention)
	}
	if err := desktopBotYield(context.Background(), scope); err == nil {
		t.Fatal("bot kept acting during takeover")
	}
	if _, err := requestDesktopTakeover(context.Background(), scope, " "); err == nil {
		t.Fatal("empty reason accepted")
	}
	// A shared bot neither calls bot ctl ops nor yields on them.
	ops = nil
	shared := agentruntime.Scope{TenantID: "t", UserID: "alice"}
	if err := desktopBotYield(context.Background(), shared); err != nil || len(ops) != 0 {
		t.Fatalf("shared %v %v", err, ops)
	}
}

// The supervisor's input lease alone makes the bot yield: a person holding
// the H.264 viewer's lease stops the bot even if yield is not set.
func TestDesktopBotYieldFollowsInputLease(t *testing.T) {
	lease := "bot"
	old := desktopRemoteCtl
	desktopRemoteCtl = func(_ context.Context, _, _, _ string, request map[string]any) (map[string]any, error) {
		return map[string]any{"yield": false, "lease": lease}, nil
	}
	defer func() { desktopRemoteCtl = old }()
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1")}
	if err := desktopBotYield(context.Background(), scope); err != nil {
		t.Fatalf("bot lease: %v", err)
	}
	lease = "human"
	if err := desktopBotYield(context.Background(), scope); err == nil {
		t.Fatal("bot kept acting while the person held the lease")
	}
}
