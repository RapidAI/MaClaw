package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsNonWAVBeforeModelLookup(t *testing.T) {
	t.Setenv("MACLAW_MEETING_ASR_MODEL", "")
	var out bytes.Buffer
	err := run(strings.NewReader(`{"audio_path":"recording.m4a","content_type":"audio/mp4"}`), &out)
	if err == nil || !strings.Contains(err.Error(), "unsupported content_type") {
		t.Fatalf("run error = %v, want unsupported content type", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must remain empty on failure: %q", out.String())
	}
}

func TestRunRequiresConfiguredModel(t *testing.T) {
	t.Setenv("MACLAW_MEETING_ASR_MODEL", "")
	var out bytes.Buffer
	err := run(strings.NewReader(`{"audio_path":"recording.wav","content_type":"audio/wav"}`), &out)
	if err == nil || !strings.Contains(err.Error(), "MACLAW_MEETING_ASR_MODEL") {
		t.Fatalf("run error = %v, want missing model error", err)
	}
}
