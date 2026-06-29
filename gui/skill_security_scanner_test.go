package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// Most scanner tests are in corelib/skill/security_scanner_test.go.
// This file only tests the GUI-specific adapter.

func TestNewSkillSecurityScanner_NilApp(t *testing.T) {
	scanner := NewSkillSecurityScanner(nil, nil)
	if scanner == nil {
		t.Fatal("scanner should not be nil even with nil app")
	}
}

func TestFormatScanReportForUser_DelegatesToShared(t *testing.T) {
	report := &skill.ScanReport{
		FinalLevel: "low",
		Summary:    "test",
		ScannedBy:  "pattern",
	}
	// Verify the gui wrapper delegates to the shared implementation.
	text := FormatScanReportForUser(report, "test-skill")
	want := skill.FormatScanReportForUser(report, "test-skill")
	if text != want {
		t.Error("should delegate to shared FormatScanReportForUser")
	}
}

// contains is defined in im_message_handler_tools_test.go
