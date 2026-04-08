package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestBrowserSessionViewSummaryCanReflectLatestRefsCount(t *testing.T) {
	state := browser.BrowserAgentState{
		ID:           "browser-1",
		CurrentURL:   "https://example.com",
		CurrentTitle: "Example",
		ReadyState:   "complete",
		LatestRefs: []browser.BrowserElementRef{
			{Ref: "@e1"},
			{Ref: "@e2"},
			{Ref: "@e3"},
		},
	}
	view := &BrowserSessionView{}
	mgr := &BrowserAgentManager{}
	mgr.applyStateLocked(view, state)
	if view.Summary.ProgressSummary == "" {
		t.Fatal("ProgressSummary should not be empty")
	}
}
