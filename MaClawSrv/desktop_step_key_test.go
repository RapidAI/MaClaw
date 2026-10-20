package main

import "testing"

func TestDesktopStepKeyIsRunScoped(t *testing.T) {
	a := desktopStepKey("run-a", "call_0")
	b := desktopStepKey("run-b", "call_0")
	if a == "" || a == b {
		t.Fatalf("same tool_call_id in two runs must not share a journal key: %q %q", a, b)
	}
	if desktopStepKey("run-a", "call_1") == a {
		t.Fatal("two calls in one run share a key")
	}
	if desktopStepKey("run-a", "") != "" {
		t.Fatal("no call id must not journal")
	}
	if desktopStepKey("", "call_9") != "call_9" {
		t.Fatal("no run id keeps the call id")
	}
}
