package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopStepTimeoutUnsticksHungTab(t *testing.T) {
	t.Setenv(desktopStepTimeoutEnv, "0.05")
	oldOp, oldUn := operateDesktopFunc, desktopUnstickFunc
	defer func() { operateDesktopFunc, desktopUnstickFunc = oldOp, oldUn }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		time.Sleep(time.Second)
		return "", nil
	}
	var asked string
	desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
		asked = url
		return map[string]any{"unstick": map[string]any{"done": true, "closed": url, "reopened": url}}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u1", "bot_625eda1bbe86a157")}
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/write"})
	if err == nil || !strings.Contains(err.Error(), "closed and opened again") || asked != "https://zhuanlan.zhihu.com/write" {
		t.Fatalf("err=%v asked=%q", err, asked)
	}
	asked = "none"
	_, err = operateDesktopWithTimeout(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u1"}, map[string]any{"action": "screenshot"})
	if asked != "none" || !strings.Contains(err.Error(), "take a screenshot") {
		t.Fatalf("shared desktop must not unstick: err=%v asked=%q", err, asked)
	}
}

func TestDesktopStepTimeoutDropsStuckBinding(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u2", "bot_625eda1bbe86a157")}
	key, err := desktopUserKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	b := desktopBindingFor(key)
	b.mu.Lock()
	desktopDropStuckBinding(scope)
	if desktopBindingFor(key) == b {
		t.Fatal("stuck binding kept")
	}
	b.mu.Unlock()
	free := desktopBindingFor(key)
	desktopDropStuckBinding(scope)
	if desktopBindingFor(key) != free {
		t.Fatal("free binding dropped")
	}
}
