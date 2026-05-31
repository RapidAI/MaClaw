package main

import "testing"

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
	if got := h.runtimeLoopContextForOwner(""); got != desktopCtx {
		t.Fatalf("runtimeLoopContextForOwner(legacy) = %p, want current ctx %p", got, desktopCtx)
	}
}
