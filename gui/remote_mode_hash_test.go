package main

import (
	"testing"
)

func TestLaunchFingerprintDeterministic(t *testing.T) {
	spec := LaunchSpec{
		Tool:     "claude",
		ModelID:  "claude-sonnet-4-20250514",
		YoloMode: true,
	}
	fp1 := LaunchFingerprint(spec)
	fp2 := LaunchFingerprint(spec)
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %s != %s", fp1, fp2)
	}
	if len(fp1) != 16 {
		t.Fatalf("expected 16-char fingerprint, got %d: %s", len(fp1), fp1)
	}
}

func TestLaunchFingerprintChangesOnModelSwitch(t *testing.T) {
	base := LaunchSpec{Tool: "claude", ModelID: "claude-sonnet-4-20250514"}
	changed := LaunchSpec{Tool: "claude", ModelID: "claude-opus-4-20250514"}
	if LaunchFingerprint(base) == LaunchFingerprint(changed) {
		t.Fatal("fingerprint should differ when model changes")
	}
}

func TestLaunchFingerprintChangesOnYoloToggle(t *testing.T) {
	base := LaunchSpec{Tool: "claude", ModelID: "m1", YoloMode: false}
	changed := LaunchSpec{Tool: "claude", ModelID: "m1", YoloMode: true}
	if LaunchFingerprint(base) == LaunchFingerprint(changed) {
		t.Fatal("fingerprint should differ when yolo mode changes")
	}
}

func TestModeChangedHelper(t *testing.T) {
	a := LaunchSpec{Tool: "claude", ModelID: "m1"}
	b := LaunchSpec{Tool: "claude", ModelID: "m1"}
	if ModeChanged(a, b) {
		t.Fatal("identical specs should not be flagged as changed")
	}
	b.AdminMode = true
	if !ModeChanged(a, b) {
		t.Fatal("different admin mode should be flagged as changed")
	}
}

func TestLaunchFingerprintIgnoresNonBehavioralFields(t *testing.T) {
	a := LaunchSpec{Tool: "claude", ModelID: "m1", Title: "Session A", SessionID: "s1"}
	b := LaunchSpec{Tool: "claude", ModelID: "m1", Title: "Session B", SessionID: "s2"}
	if LaunchFingerprint(a) != LaunchFingerprint(b) {
		t.Fatal("fingerprint should ignore title and session_id")
	}
}
