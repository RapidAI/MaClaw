package main

import "testing"

func TestResolveCreateSessionContextKeepsExplicitValues(t *testing.T) {
	handler := &IMMessageHandler{}

	got := handler.resolveCreateSessionContext("claude", "/repo")

	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
	if got.Tool != "claude" {
		t.Fatalf("expected explicit tool, got %q", got.Tool)
	}
	if got.ProjectPath != "/repo" {
		t.Fatalf("expected explicit project path, got %q", got.ProjectPath)
	}
	if len(got.Hints) != 0 {
		t.Fatalf("expected no hints without auto resolution, got %#v", got.Hints)
	}
}

func TestResolveCreateSessionContextRequiresToolWhenNoResolver(t *testing.T) {
	handler := &IMMessageHandler{}

	got := handler.resolveCreateSessionContext("", "/repo")

	if got.Error == "" {
		t.Fatal("expected missing tool error")
	}
}
