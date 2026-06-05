package main

import "testing"

func TestAppToolLockLazyInitializesMap(t *testing.T) {
	app := &App{}
	if app.isToolLocked("codex") {
		t.Fatal("zero-value App should not report tool locked")
	}
	if !app.tryLockTool("codex") {
		t.Fatal("zero-value App should acquire first tool lock")
	}
	if !app.isToolLocked("codex") {
		t.Fatal("tool should be locked after acquisition")
	}
	if app.tryLockTool("codex") {
		t.Fatal("second acquisition should fail while locked")
	}
	app.unlockTool("codex")
	if app.isToolLocked("codex") {
		t.Fatal("tool should be unlocked after release")
	}
}
