package main

import (
	"errors"
	"testing"
)

func TestCreateSessionParentRunID(t *testing.T) {
	handler := &IMMessageHandler{}
	if got := handler.createSessionParentRunID(); got != "" {
		t.Fatalf("expected empty parent run id without loop context, got %q", got)
	}

	handler.currentLoopCtx = &LoopContext{RunID: "run-parent"}
	if got := handler.createSessionParentRunID(); got != "run-parent" {
		t.Fatalf("expected run-parent, got %q", got)
	}
}

func TestBuildCreateSessionStartRequest(t *testing.T) {
	handler := &IMMessageHandler{currentLoopCtx: &LoopContext{RunID: "run-parent"}}

	got := handler.buildCreateSessionStartRequest("claude", "proj-1", "/repo", "Original", "resume-1")

	if got.Tool != "claude" {
		t.Fatalf("expected tool claude, got %q", got.Tool)
	}
	if got.ProjectID != "proj-1" {
		t.Fatalf("expected project id proj-1, got %q", got.ProjectID)
	}
	if got.ProjectPath != "/repo" {
		t.Fatalf("expected project path /repo, got %q", got.ProjectPath)
	}
	if got.Provider != "Original" {
		t.Fatalf("expected provider Original, got %q", got.Provider)
	}
	if got.ResumeSessionID != "resume-1" {
		t.Fatalf("expected resume session id resume-1, got %q", got.ResumeSessionID)
	}
	if got.InjectResumePrompt {
		t.Fatal("create_session should not inject resume prompt")
	}
	if got.LaunchSource != RemoteLaunchSourceAI {
		t.Fatalf("expected AI launch source, got %q", got.LaunchSource)
	}
	if got.ParentRunID != "run-parent" {
		t.Fatalf("expected parent run id, got %q", got.ParentRunID)
	}
}

func TestRenderCreateSessionStartError(t *testing.T) {
	got := renderCreateSessionStartError(errors.New("boom"), "claude", "/repo")

	if !contains(got, "boom") {
		t.Fatalf("expected original error, got %q", got)
	}
	if !contains(got, "claude") {
		t.Fatalf("expected tool name, got %q", got)
	}
	if !contains(got, "/repo") {
		t.Fatalf("expected project path, got %q", got)
	}
	if !contains(got, "list_providers") {
		t.Fatalf("expected provider guidance, got %q", got)
	}
}
