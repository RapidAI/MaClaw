package security

import (
	"os"
	"testing"
	"time"
)

func TestLoadSessionState_NewSession(t *testing.T) {
	s, err := LoadSessionState("test-new-session-" + time.Now().Format("150405"))
	if err != nil {
		t.Fatal(err)
	}
	if s.SecurityMode != "standard" {
		t.Errorf("SecurityMode = %q, want standard", s.SecurityMode)
	}
	if s.ToolCallCount != 0 {
		t.Errorf("ToolCallCount = %d, want 0", s.ToolCallCount)
	}
	// cleanup
	os.Remove(sessionStatePath(s.SessionID))
}

func TestSessionState_SaveAndLoad(t *testing.T) {
	id := "test-save-load-" + time.Now().Format("150405")
	s, _ := LoadSessionState(id)
	s.IncrementToolCall()
	s.IncrementToolCall()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(sessionStatePath(id))

	loaded, _ := LoadSessionState(id)
	if loaded.ToolCallCount != 2 {
		t.Errorf("ToolCallCount = %d, want 2", loaded.ToolCallCount)
	}
}

func TestSessionState_HighRiskUpgrade(t *testing.T) {
	id := "test-upgrade-" + time.Now().Format("150405")
	s, _ := LoadSessionState(id)
	defer os.Remove(sessionStatePath(id))

	// First two should not trigger upgrade
	if s.IncrementHighRisk() {
		t.Error("1st high risk should not trigger upgrade")
	}
	if s.IncrementHighRisk() {
		t.Error("2nd high risk should not trigger upgrade")
	}
	// Third should trigger
	if !s.IncrementHighRisk() {
		t.Error("3rd high risk should trigger upgrade")
	}
	if s.SecurityMode != "strict" {
		t.Errorf("SecurityMode = %q, want strict", s.SecurityMode)
	}
	if !s.ModeUpgraded {
		t.Error("ModeUpgraded should be true")
	}
}

func TestSessionState_CorruptedFile(t *testing.T) {
	id := "test-corrupt-" + time.Now().Format("150405")
	p := sessionStatePath(id)
	os.WriteFile(p, []byte("{invalid json"), 0o644)
	defer os.Remove(p)

	s, err := LoadSessionState(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.SecurityMode != "standard" {
		t.Errorf("corrupted file should return fresh state, got mode=%q", s.SecurityMode)
	}
}

func TestSanitizeSessionID(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"abc-123", "abc-123"},
		{"../../../etc/passwd", "etcpasswd"},
		{"", "unknown"},
		{"a/b\\c", "abc"},
	} {
		got := sanitizeSessionID(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
