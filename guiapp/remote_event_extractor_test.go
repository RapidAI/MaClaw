package guiapp

import "testing"

func TestClaudeEventExtractorDetectsStructuredEvents(t *testing.T) {
	extractor := NewClaudeEventExtractor()
	session := &RemoteSession{ID: "sess-1", Tool: "claude"}

	events := extractor.Consume(session, []string{
		"Reading src/main.go",
		"Modified src/main.go",
		"Running go test ./...",
		"Need your input to continue",
		"Error: build failed",
	})

	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if events[0].Type != "file.read" {
		t.Fatalf("event[0].Type = %q, want %q", events[0].Type, "file.read")
	}
	if events[1].Type != "file.change" {
		t.Fatalf("event[1].Type = %q, want %q", events[1].Type, "file.change")
	}
	if events[2].Type != "command.started" {
		t.Fatalf("event[2].Type = %q, want %q", events[2].Type, "command.started")
	}
	if events[2].Command != "go test ./..." {
		t.Fatalf("event[2].Command = %q, want %q", events[2].Command, "go test ./...")
	}
	if events[3].Type != "input.required" {
		t.Fatalf("event[3].Type = %q, want %q", events[3].Type, "input.required")
	}
	if events[4].Type != "session.error" {
		t.Fatalf("event[4].Type = %q, want %q", events[4].Type, "session.error")
	}
}

func TestClaudeEventExtractorIgnoresBenignErrorPhrases(t *testing.T) {
	extractor := NewClaudeEventExtractor()
	session := &RemoteSession{ID: "sess-1", Tool: "claude"}

	events := extractor.Consume(session, []string{
		"Build completed with 0 errors",
		"Finished successfully without errors",
	})

	if len(events) != 0 {
		t.Fatalf("event count = %d, want 0", len(events))
	}
}

func TestClaudeEventExtractorDetectsPromptStyleCommand(t *testing.T) {
	extractor := NewClaudeEventExtractor()
	session := &RemoteSession{ID: "sess-1", Tool: "claude"}

	events := extractor.Consume(session, []string{
		"$ pytest -q",
	})

	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Type != "command.started" {
		t.Fatalf("event.Type = %q, want %q", events[0].Type, "command.started")
	}
	if events[0].Command != "pytest -q" {
		t.Fatalf("event.Command = %q, want %q", events[0].Command, "pytest -q")
	}
}
