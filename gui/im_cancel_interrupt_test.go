package main

import "testing"

func TestCancelledLoopDoesNotTryInlineInterrupt(t *testing.T) {
	h := &IMMessageHandler{
		interruptHandler: &imInterruptHandler{},
		currentLoopCtx:   NewLoopContext("chat", 3, nil),
	}
	msg := IMUserMessage{UserID: "desktop-user", Text: "new task after cancel"}

	if !h.shouldTryInlineInterrupt(msg) {
		t.Fatal("running loop should accept inline interrupt routing")
	}

	h.currentLoopCtx.Cancel()

	if h.shouldTryInlineInterrupt(msg) {
		t.Fatal("cancelled loop must not route new messages as current-task supplements")
	}
}
