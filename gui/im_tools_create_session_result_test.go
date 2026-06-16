package main

import "testing"

func TestRenderCreateSessionLaunchBanner(t *testing.T) {
	got := renderCreateSessionLaunchBanner("claude", "Original", "/repo")

	if !containsText(got, "claude") {
		t.Fatalf("expected tool in banner, got %q", got)
	}
	if !containsText(got, "Original") {
		t.Fatalf("expected provider in banner, got %q", got)
	}
	if !containsText(got, "/repo") {
		t.Fatalf("expected project path in banner, got %q", got)
	}
}

func TestRenderCreateSessionStartedMessageIncludesHintsAndSessionID(t *testing.T) {
	got := renderCreateSessionStartedMessage([]string{"hint one", "hint two"}, "sess_123")

	if !containsText(got, "hint one") || !containsText(got, "hint two") {
		t.Fatalf("expected hints in message, got %q", got)
	}
	if !containsText(got, "sess_123") {
		t.Fatalf("expected session id in message, got %q", got)
	}
	if !containsText(got, "get_session_output") {
		t.Fatalf("expected output verification guidance, got %q", got)
	}
	if !containsText(got, "CodingSubAgent") || containsText(got, "send_and_observe") {
		t.Fatalf("expected CodingSubAgent guidance without send guidance, got %q", got)
	}
}

func TestRenderCreateSessionStartedMessageWorksWithoutHints(t *testing.T) {
	got := renderCreateSessionStartedMessage(nil, "sess_empty")

	if !containsText(got, "sess_empty") {
		t.Fatalf("expected session id in message, got %q", got)
	}
}
