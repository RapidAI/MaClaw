package main

import (
	"strings"
	"testing"
	"time"
)

func TestBrowserAgentManagerListIncludesCurrentSnapshotFields(t *testing.T) {
	app := &App{}
	app.ensureAITrace()
	mgr := NewBrowserAgentManager(app)
	mgr.mapViews["browser-x"] = &BrowserSessionView{
		ID:             "browser-x",
		Tool:           "browser",
		Title:          "Browser Session",
		Status:         SessionRunning,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		RunID:          "run-browser-x",
		CurrentURL:     "https://example.org/page",
		CurrentTitle:   "Example Page",
		ReadyState:     "complete",
		LastSnapshotID: "snap-browser-x",
		Summary: SessionSummary{
			SessionID:       "browser-x",
			Tool:            "browser",
			Status:          string(SessionRunning),
			ProgressSummary: "Browser session active",
		},
		Preview: SessionPreview{SessionID: "browser-x", PreviewLines: []string{"Example Page", "https://example.org/page"}},
	}

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	got := list[0]
	if got.CurrentURL != "https://example.org/page" {
		t.Fatalf("CurrentURL = %q", got.CurrentURL)
	}
	if got.CurrentTitle != "Example Page" {
		t.Fatalf("CurrentTitle = %q", got.CurrentTitle)
	}
	if got.LastSnapshotID != "snap-browser-x" {
		t.Fatalf("LastSnapshotID = %q", got.LastSnapshotID)
	}
}

func TestBrowserTraceStoredTextDoesNotExposeBrowserContent(t *testing.T) {
	got := browserTraceStoredText("browser.navigate", "Browser: SECRET_BROWSER_TEXT https://example.com/path?token=SECRET")
	for _, leaked := range []string{"SECRET_BROWSER_TEXT", "SECRET", "Browser:", "https://example.com"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("browser trace stored text leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "browser.navigate text_len=") {
		t.Fatalf("browser trace stored text missing metadata: %s", got)
	}
}
