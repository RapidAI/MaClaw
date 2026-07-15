package remote

import (
	"testing"
	"time"
)

func TestSSHHostConfig_Defaults(t *testing.T) {
	cfg := SSHHostConfig{Host: "10.0.0.1", User: "root"}
	cfg.Defaults()

	if cfg.Port != 22 {
		t.Errorf("expected port 22, got %d", cfg.Port)
	}
	if cfg.ConnectTimeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", cfg.ConnectTimeout)
	}
	if cfg.KeepaliveInterval != 15*time.Second {
		t.Errorf("expected 15s keepalive, got %v", cfg.KeepaliveInterval)
	}
	if cfg.AuthMethod != "key" {
		t.Errorf("expected auth_method=key, got %s", cfg.AuthMethod)
	}
}

func TestSSHHostConfig_SSHHostID(t *testing.T) {
	tests := []struct {
		cfg    SSHHostConfig
		expect string
	}{
		{SSHHostConfig{Host: "10.0.0.1", User: "root", Port: 22}, "root@10.0.0.1:22"},
		{SSHHostConfig{Host: "web.example.com", User: "deploy", Port: 2222}, "deploy@web.example.com:2222"},
		{SSHHostConfig{Host: "10.0.0.1", User: "root"}, "root@10.0.0.1:22"}, // Port=0 → default 22
	}
	for _, tt := range tests {
		got := tt.cfg.SSHHostID()
		if got != tt.expect {
			t.Errorf("SSHHostID() = %q, want %q", got, tt.expect)
		}
	}
}

func TestSSHPool_NewAndStats(t *testing.T) {
	pool := NewSSHPool()
	stats := pool.Stats()
	if len(stats) != 0 {
		t.Errorf("new pool should have 0 connections, got %d", len(stats))
	}
}

func TestSSHSessionManager_GetNotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent session")
	}
}

func TestSSHSessionManager_GetSessionStatus_NotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	_, ok := mgr.GetSessionStatus("nonexistent")
	if ok {
		t.Error("expected GetSessionStatus to return false for nonexistent session")
	}
}

func TestSSHSessionManager_ListEmpty(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	list := mgr.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestSSHSessionManager_WriteInput_NotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	err := mgr.WriteInput("nonexistent", "ls")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestNormalizeSSHPTYSize(t *testing.T) {
	cols, rows := normalizeSSHPTYSize(0, 0)
	if cols != 120 || rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", cols, rows)
	}
	cols, rows = normalizeSSHPTYSize(80, 24)
	if cols != 80 || rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", cols, rows)
	}
}

func TestSplitSSHOutputLines(t *testing.T) {
	lines := splitSSHOutputLines([]byte("hello\nworld\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("unexpected lines: %v", lines)
	}

	lines = splitSSHOutputLines([]byte("no newline"))
	if len(lines) != 1 || lines[0] != "no newline" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestSplitCompleteSSHLines(t *testing.T) {
	complete, rem := splitCompleteSSHLines("hello\nworld\nEXIT: 0")
	if len(complete) != 2 || complete[0] != "hello" || complete[1] != "world" {
		t.Fatalf("complete=%v", complete)
	}
	if rem != "EXIT: 0" {
		t.Fatalf("remainder=%q", rem)
	}

	// CRLF
	complete, rem = splitCompleteSSHLines("a\r\nb\r\n")
	if len(complete) != 2 || complete[0] != "a" || complete[1] != "b" || rem != "" {
		t.Fatalf("crlf complete=%v rem=%q", complete, rem)
	}

	// half-line only
	complete, rem = splitCompleteSSHLines("partial")
	if len(complete) != 0 || rem != "partial" {
		t.Fatalf("half complete=%v rem=%q", complete, rem)
	}
}

func TestHasCommandExitMarker(t *testing.T) {
	if !hasCommandExitMarker([]string{"output", "EXIT: 0"}) {
		t.Fatal("expected EXIT: 0 detected")
	}
	if !hasCommandExitMarker([]string{"EXIT:127"}) {
		t.Fatal("expected EXIT:127 detected")
	}
	if hasCommandExitMarker([]string{"no exit here", "price EXIT: never"}) {
		t.Fatal("should not match non-numeric EXIT")
	}
	if hasCommandExitMarker([]string{"just output"}) {
		t.Fatal("should not match without EXIT")
	}
	if hasCommandExitMarker([]string{"EXIT: 0xdead"}) {
		t.Fatal("should reject non-decimal EXIT codes")
	}
	// only inspect tail
	old := make([]string, 20)
	for i := range old {
		old[i] = "noise"
	}
	old = append(old, "EXIT: 0")
	if !hasCommandExitMarker(old) {
		t.Fatal("expected tail EXIT detected")
	}
}
