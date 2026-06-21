package main

import (
	"strings"
	"testing"
)

func TestBuildRepeatedToolFailureStopMessage(t *testing.T) {
	msg := buildRepeatedToolFailureStopMessage("bash")
	if !strings.Contains(msg, "bash") ||
		!strings.Contains(msg, "stopped to avoid a loop") ||
		!strings.Contains(msg, "recent tool output/logs") {
		t.Fatalf("message should name the tool and include recovery guidance, got %q", msg)
	}

	msg = buildRepeatedToolFailureStopMessage(" ")
	if strings.Contains(msg, "called  ") || !strings.Contains(msg, "called a tool") {
		t.Fatalf("empty tool name should use fallback wording, got %q", msg)
	}
}
