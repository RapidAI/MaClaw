package main

import "testing"

func TestRenderCreateSessionLaunchBanner(t *testing.T) {
	got := renderCreateSessionLaunchBanner("claude", "Original", "/repo")

	if !contains(got, "claude") {
		t.Fatalf("expected tool in banner, got %q", got)
	}
	if !contains(got, "Original") {
		t.Fatalf("expected provider in banner, got %q", got)
	}
	if !contains(got, "/repo") {
		t.Fatalf("expected project path in banner, got %q", got)
	}
}

func TestRenderCreateSessionStartedMessageIncludesHintsAndSessionID(t *testing.T) {
	got := renderCreateSessionStartedMessage([]string{"hint one", "hint two"}, "sess_123")

	if !contains(got, "hint one") || !contains(got, "hint two") {
		t.Fatalf("expected hints in message, got %q", got)
	}
	if !contains(got, "sess_123") {
		t.Fatalf("expected session id in message, got %q", got)
	}
	if !contains(got, "get_session_output") {
		t.Fatalf("expected output verification guidance, got %q", got)
	}
	if !contains(got, "send_and_observe") {
		t.Fatalf("expected send guidance, got %q", got)
	}
}

func TestRenderCreateSessionStartedMessageWorksWithoutHints(t *testing.T) {
	got := renderCreateSessionStartedMessage(nil, "sess_empty")

	if !contains(got, "sess_empty") {
		t.Fatalf("expected session id in message, got %q", got)
	}
}
