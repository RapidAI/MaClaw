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

func TestFormatRecordAudioMarkerRoundTrip(t *testing.T) {
	raw := FormatRecordAudioMarker(&RecordAudioRequest{
		Title:   "  会  ",
		Purpose: "纪要",
		Hint:    "结束后整理",
	})
	req, ok := ParseRecordAudioResult(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if req.Title != "会" {
		t.Fatalf("title = %q", req.Title)
	}
	if req.Purpose != "纪要" || req.Hint != "结束后整理" {
		t.Fatalf("req = %#v", req)
	}
	// Nil request still yields a valid marker with default title.
	if _, ok := ParseRecordAudioResult(FormatRecordAudioMarker(nil)); !ok {
		t.Fatal("nil marker should parse")
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
