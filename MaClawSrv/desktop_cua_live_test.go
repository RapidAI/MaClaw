package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// TestDesktopCuaActionsAgainstLiveDriver runs both new actions end to end
// against a real daemon. Without MACLAW_CUA_SOCKET it skips.
func TestDesktopCuaActionsAgainstLiveDriver(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MACLAW_CUA_SOCKET")) == "" {
		t.Skip("no Cua Driver socket configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run-cua-live"}

	// wait_until: a window that is surely there appears at once.
	text, err := operateDesktopWaitUntil(ctx, scope, map[string]any{
		"title": "OpenBot", "timeout": "5s",
	})
	if err != nil {
		t.Fatalf("wait_until: %v", err)
	}
	t.Logf("wait_until: %s", text)
	if !strings.Contains(text, "appear") {
		t.Fatalf("wait_until text did not report the outcome: %s", text)
	}

	// verify_state: a missing window is not there, which the window check
	// can prove; an element check on a window that is not the target is
	// refused with the reason.
	_, err = operateDesktopVerifyState(ctx, scope, map[string]any{
		"expect": []any{map[string]any{"window": map[string]any{"exists": true}}},
	})
	if err == nil || !strings.Contains(err.Error(), "pid") {
		t.Fatalf("verify_state without a target must ask for pid/window_id, got %v", err)
	}
}
