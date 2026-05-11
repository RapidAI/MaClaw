package main

import (
	"strings"
	"testing"
)

func TestSnapshotSessionEventsCopiesEvents(t *testing.T) {
	session := &RemoteSession{
		Events: []ImportantEvent{{Type: "file.change", Title: "Changed", RelatedFile: "a.go"}},
	}

	events := snapshotSessionEvents(session)
	session.Events[0].Title = "mutated"

	if len(events) != 1 {
		t.Fatalf("events length = %d, want 1", len(events))
	}
	if got := events[0].Title; got != "Changed" {
		t.Fatalf("events[0].Title = %q, want copied value", got)
	}
}

func TestRenderSessionEventsEmpty(t *testing.T) {
	got := renderSessionEvents("s1", nil)
	if !strings.Contains(got, "s1") || !strings.Contains(got, "暂无重要事件") {
		t.Fatalf("empty events output = %q", got)
	}
}

func TestRenderSessionEventsIncludesOptionalFields(t *testing.T) {
	got := renderSessionEvents("s1", []ImportantEvent{{
		Severity:    "info",
		Type:        "file.change",
		Title:       "Changed file",
		Summary:     "updated handler",
		RelatedFile: "gui/im_tools_session.go",
	}})

	for _, want := range []string{
		"[info]",
		"file.change",
		"Changed file",
		"updated handler",
		"gui/im_tools_session.go",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered events missing %q:\n%s", want, got)
		}
	}
}
