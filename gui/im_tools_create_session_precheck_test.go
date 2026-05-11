package main

import "testing"

func TestResolveCreateSessionPrecheckResultAllPassed(t *testing.T) {
	got := resolveCreateSessionPrecheckResult(PrecheckResult{
		ToolReady:    true,
		ProjectReady: true,
		ModelReady:   true,
		AllPassed:    true,
	}, "claude")

	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
	if len(got.Hints) != 1 {
		t.Fatalf("expected one success hint, got %#v", got.Hints)
	}
}

func TestResolveCreateSessionPrecheckResultBlocksMissingTool(t *testing.T) {
	got := resolveCreateSessionPrecheckResult(PrecheckResult{
		ToolReady:    false,
		ProjectReady: true,
		ModelReady:   true,
		ToolHint:     "install claude",
	}, "claude")

	if got.Error == "" {
		t.Fatal("expected missing tool to block session creation")
	}
	if !contains(got.Error, "install claude") {
		t.Fatalf("expected tool hint in error, got %q", got.Error)
	}
	if !contains(got.Error, "claude") {
		t.Fatalf("expected tool name in error, got %q", got.Error)
	}
}

func TestResolveCreateSessionPrecheckResultKeepsProjectAndModelAsHints(t *testing.T) {
	got := resolveCreateSessionPrecheckResult(PrecheckResult{
		ToolReady:    true,
		ProjectReady: false,
		ModelReady:   false,
		ModelHint:    "configure model",
	}, "claude")

	if got.Error != "" {
		t.Fatalf("project/model precheck failures should be hints only, got %q", got.Error)
	}
	if len(got.Hints) != 2 {
		t.Fatalf("expected two warning hints, got %#v", got.Hints)
	}
}
