package main

import (
	"strings"
	"testing"
)

func TestRuntimeLoopContextForOwnerDoesNotUseOtherCurrentLoop(t *testing.T) {
	h := &IMMessageHandler{}
	desktopCtx := NewLoopContext("desktop", 1, nil)
	remoteCtx := NewLoopContext("remote", 1, nil)
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	h.setSessionLoopCtx("remote:mobile", remoteCtx)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	if got := h.runtimeLoopContextForOwner("remote:mobile"); got != remoteCtx {
		t.Fatalf("runtimeLoopContextForOwner(remote) = %p, want remote ctx %p", got, remoteCtx)
	}
	if got := h.runtimeLoopContextForOwner("missing:owner"); got != nil {
		t.Fatalf("runtimeLoopContextForOwner(missing) = %p, want nil", got)
	}
	if got := h.runtimeLoopContextForOwner(""); got != nil {
		t.Fatalf("runtimeLoopContextForOwner(empty) = %p, want nil isolation boundary", got)
	}
}

func TestToolAsyncWaitEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	desktopCtx := NewLoopContext("desktop", 1, nil)
	h := &IMMessageHandler{}
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	got := h.toolAsyncWait(map[string]interface{}{
		"action":                         "wait",
		"task_id":                        "bg_test",
		registeredToolPolicyOwnerIDField: "",
	}, nil)
	if !strings.Contains(got, "runtime owner is missing") {
		t.Fatalf("async_wait with empty runtime owner should fail closed, got %q", got)
	}
}
