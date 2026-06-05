package main

import (
	"strings"
	"testing"
)

func TestSelectScreenshotSession(t *testing.T) {
	s1 := &RemoteSession{ID: "s1", Tool: "codex", Status: SessionRunning}
	s2 := &RemoteSession{ID: "s2", Tool: "opencode", Status: SessionBusy}

	tests := []struct {
		name       string
		sessionID  string
		sessions   []*RemoteSession
		wantKind   screenshotSessionSelectionKind
		wantID     string
		wantSubstr string
	}{
		{name: "explicit", sessionID: "given", sessions: []*RemoteSession{s1, s2}, wantKind: screenshotSessionSelectionSelected, wantID: "given"},
		{name: "none", sessions: nil, wantKind: screenshotSessionSelectionNone},
		{name: "single", sessions: []*RemoteSession{s1}, wantKind: screenshotSessionSelectionSelected, wantID: "s1"},
		{name: "multiple", sessions: []*RemoteSession{s1, s2}, wantKind: screenshotSessionSelectionMultiple, wantSubstr: "请指定 session_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectScreenshotSession(tt.sessionID, tt.sessions)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.SessionID != tt.wantID {
				t.Fatalf("SessionID = %q, want %q", got.SessionID, tt.wantID)
			}
			if tt.wantSubstr != "" && !strings.Contains(got.Message, tt.wantSubstr) {
				t.Fatalf("Message = %q, want substring %q", got.Message, tt.wantSubstr)
			}
		})
	}
}

func TestRenderMultipleScreenshotSessions(t *testing.T) {
	got := renderMultipleScreenshotSessions([]*RemoteSession{
		{ID: "s1", Tool: "codex", Status: SessionRunning},
		{ID: "s2", Tool: "opencode", Status: SessionBusy},
	})
	for _, want := range []string{"s1", "codex", "running", "s2", "opencode", "busy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered sessions missing %q:\n%s", want, got)
		}
	}
}
