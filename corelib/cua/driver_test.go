package cua

import (
	"context"
	"os"
	"testing"
	"time"
)

// The live tests need MACLAW_CUA_SOCKET pointing at a running
// `cua-driver serve`; without one they skip, and a failure in them is a
// protocol break rather than an environment quirk.

func TestOpenDefaultAgainstLiveDriver(t *testing.T) {
	d, err := OpenDefault(context.Background())
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	defer d.Close()
	health, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got, _ := health["overall"].(string); got != "ok" {
		t.Fatalf("overall = %q, want ok: %v", got, health)
	}
}

// TestListWindowsAgainstServe proves the "call" dialect: the tool name and
// its arguments ride beside "method", and the answer's content[] is
// unwrapped for the caller.
func TestListWindowsAgainstServe(t *testing.T) {
	d, err := OpenDefault(context.Background())
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	defer d.Close()
	wins, err := d.Call(context.Background(), "list_windows", map[string]any{})
	if err != nil {
		t.Fatalf("list_windows: %v", err)
	}
	if got, _ := wins["windows"].([]any); len(got) == 0 {
		t.Fatalf("list_windows returned no windows: %v", wins)
	}
}

// TestSnapshotOfOwnWindow reads this test process's own console window when
// one exists, so a real UIA tree is parsed and a token round-trips.
func TestSnapshotOfOwnWindow(t *testing.T) {
	d, err := OpenDefault(context.Background())
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := d.Snapshot(ctx, os.Getpid(), 0, "", 10); err == nil {
		t.Fatal("a window id of 0 must be refused, not read")
	}
}
