package main

import "testing"

func TestCurrentRuntimeTaskTextDoesNotUseLegacyLastUser(t *testing.T) {
	h := &IMMessageHandler{lastUserID: desktopUserID, lastUserText: "desktop secret task"}

	text, ownerID := h.currentRuntimeTaskTextOrLegacy()
	if text != "" || ownerID != "" {
		t.Fatalf("currentRuntimeTaskTextOrLegacy() = (%q, %q), want empty without explicit runtime", text, ownerID)
	}
}

func TestCurrentRuntimeTaskTextUsesExplicitOwner(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	ctx.Runtime = RuntimeContext{RequestID: "req-owner", PolicyOwnerID: "owner-1"}
	h.currentLoopCtx = ctx
	h.setSessionLoopCtx("owner-1", ctx)
	state := h.getSessionLoop("owner-1")
	state.stateMu.Lock()
	state.userText = "owner task"
	state.stateMu.Unlock()

	text, ownerID := h.currentRuntimeTaskTextOrLegacy()
	if text != "owner task" || ownerID != "owner-1" {
		t.Fatalf("currentRuntimeTaskTextOrLegacy() = (%q, %q), want explicit owner task", text, ownerID)
	}
}

func TestOlderLoopCleanupDoesNotClearReplacementLoopState(t *testing.T) {
	const userID = "im:replacement"
	h := &IMMessageHandler{}
	oldCtx := NewLoopContext("old", 1, nil)
	oldCleanup := h.beginAgentLoopRuntime(oldCtx, userID, "old task", "weixin")

	newCtx := NewLoopContext("new", 1, nil)
	newCleanup := h.beginAgentLoopRuntime(newCtx, userID, "new task", "weixin")
	h.accumulateInjection(userID, "[用户补充] only for replacement")

	oldCleanup()
	if got := h.getSessionLoopCtx(userID); got != newCtx {
		t.Fatalf("active loop after old cleanup = %p, want replacement %p", got, newCtx)
	}
	if got := h.sessionLoopTaskText(userID); got != "new task" {
		t.Fatalf("replacement task text = %q, want new task", got)
	}
	if raw, ok := h.pendingInjection.Load(userID); !ok || raw == "" {
		t.Fatal("old cleanup discarded replacement steering injection")
	}

	newCleanup()
	if got := h.getSessionLoopCtx(userID); got != nil {
		t.Fatalf("active loop after replacement cleanup = %p, want nil", got)
	}
}
