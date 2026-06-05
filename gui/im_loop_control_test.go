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
