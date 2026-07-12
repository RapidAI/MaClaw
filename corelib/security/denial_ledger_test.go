package security

import (
	"strings"
	"testing"
)

func TestDenialLedger_PausesAfterThreshold(t *testing.T) {
	t.Setenv(DenialPauseEnvKey, "on")
	t.Setenv(DenialPauseThresholdEnvKey, "3")
	ResetProcessDenialLedgerForTest()
	l := ProcessDenialLedger()

	if l.RecordDeny("bash", "policy") {
		t.Fatal("should not pause on first")
	}
	if l.RecordDeny("bash", "policy") {
		t.Fatal("should not pause on second")
	}
	if !l.RecordDeny("write_file", "policy") {
		t.Fatal("should pause on third")
	}
	if !l.IsPaused() {
		t.Fatal("expected paused")
	}
	msg := l.PauseBlockMessage()
	if !strings.Contains(msg, "paused") || !strings.Contains(msg, "write_file") {
		t.Fatalf("msg=%q", msg)
	}
	l.ClearPause()
	if l.IsPaused() {
		t.Fatal("cleared")
	}
}

func TestDenialLedger_AllowResetsStreak(t *testing.T) {
	t.Setenv(DenialPauseThresholdEnvKey, "3")
	t.Setenv(DenialPauseEnvKey, "")
	ResetProcessDenialLedgerForTest()
	l := ProcessDenialLedger()
	l.RecordDeny("bash", "x")
	l.RecordDeny("bash", "x")
	l.RecordAllow()
	if l.Snapshot().ConsecutiveDenies != 0 {
		t.Fatal("streak should reset")
	}
	l.RecordDeny("bash", "x")
	if l.IsPaused() {
		t.Fatal("should not pause after reset")
	}
}

func TestDenialLedger_Disabled(t *testing.T) {
	t.Setenv(DenialPauseEnvKey, "off")
	t.Setenv(DenialPauseThresholdEnvKey, "1")
	ResetProcessDenialLedgerForTest()
	l := ProcessDenialLedger()
	for i := 0; i < 5; i++ {
		if l.RecordDeny("bash", "x") {
			t.Fatal("disabled must not pause")
		}
	}
	if l.IsPaused() {
		t.Fatal("paused")
	}
}
