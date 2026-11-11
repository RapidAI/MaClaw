package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestD101LiveRunProgressIsSaid(t *testing.T) {
	base := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	now := base
	desktopLongJobNow = func() time.Time { return now }
	said := make(chan map[string]any, 4)
	prev := desktopLongJobSayFunc
	desktopLongJobSayFunc = func(_ context.Context, b map[string]any) error { said <- b; return nil }
	t.Cleanup(func() { desktopLongJobNow = time.Now; desktopLongJobSayFunc = prev })
	scope := agentruntime.Scope{TenantID: "tenant_default", UserID: "u_1::desk::bot_1", InstanceID: "inst_1", RunID: "run_progress_1"}
	desktopRunProgressSay(scope, "bash", "15:00 epoch 1 step 100/803 loss=0.31", nil)
	now = base.Add(11 * time.Minute)
	desktopRunProgressSay(scope, "screenshot", "epoch 1 step 400/803 loss=0.25", nil)
	desktopRunProgressSay(scope, "bash", "published: failed to load picture", nil)
	desktopRunProgressSay(scope, "bash", "Warning: added host\n15:11 epoch 1 step 450/803 loss=0.24", nil)
	select {
	case b := <-said:
		text, _ := b["text"].(string)
		if b["user_id"] != "u_1" || !strings.Contains(text, "step 450/803") || strings.Contains(text, "Warning") || !strings.Contains(text, "11 分钟") {
			t.Fatalf("said %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("progress not said")
	}
	now = base.Add(15 * time.Minute)
	desktopRunProgressSay(scope, "bash", "epoch 1 step 600/803 loss=0.2", nil)
	select {
	case b := <-said:
		t.Fatalf("said again within the interval: %+v", b)
	case <-time.After(100 * time.Millisecond):
	}
}
