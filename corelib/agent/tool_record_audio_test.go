package agent

import (
	"strings"
	"testing"
)

func TestToolRecordAudioMarkerRoundTrip(t *testing.T) {
	raw := ToolRecordAudio(map[string]interface{}{
		"title":   "团队周会",
		"purpose": "记录讨论",
		"hint":    "结束后可整理纪要",
	})
	if !IsRecordAudioResult(raw) {
		t.Fatalf("expected record audio marker, got %q", raw)
	}
	req, ok := ParseRecordAudioResult(raw)
	if !ok {
		t.Fatal("ParseRecordAudioResult failed")
	}
	if req.Title != "团队周会" {
		t.Fatalf("title = %q", req.Title)
	}
	if req.Purpose != "记录讨论" {
		t.Fatalf("purpose = %q", req.Purpose)
	}
	display := FormatRecordAudioForDisplay(req)
	if !strings.Contains(display, "团队周会") {
		t.Fatalf("display missing title: %q", display)
	}
}

func TestToolRecordAudioDefaultTitle(t *testing.T) {
	raw := ToolRecordAudio(map[string]interface{}{})
	req, ok := ParseRecordAudioResult(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if req.Title != "录音" {
		t.Fatalf("default title = %q", req.Title)
	}
}
