package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCleanLastUpdate removes the timestamp file before/after a test.
func testCleanLastUpdate(t *testing.T) string {
	t.Helper()
	p := clawnetLastUpdatePath()
	if p == "" {
		t.Skip("cannot determine home dir")
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	os.Remove(p)
	t.Cleanup(func() { os.Remove(p) })
	return p
}

func TestNeedsUpdate_NoFile(t *testing.T) {
	testCleanLastUpdate(t)
	if !needsUpdate() {
		t.Error("expected needsUpdate=true when no timestamp file exists")
	}
}

func TestWriteAndReadLastUpdateTime(t *testing.T) {
	testCleanLastUpdate(t)

	writeLastUpdateTime()
	last := readLastUpdateTime()
	if last.IsZero() {
		t.Fatal("readLastUpdateTime returned zero after write")
	}
	if time.Since(last) > 5*time.Second {
		t.Errorf("timestamp too old: %v", last)
	}
}

func TestNeedsUpdate_RecentWrite(t *testing.T) {
	testCleanLastUpdate(t)

	writeLastUpdateTime()
	if needsUpdate() {
		t.Error("expected needsUpdate=false right after writing timestamp")
	}
}

func TestNeedsUpdate_StaleTimestamp(t *testing.T) {
	p := testCleanLastUpdate(t)

	stale := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	_ = os.WriteFile(p, []byte(stale), 0644)

	if !needsUpdate() {
		t.Error("expected needsUpdate=true for 25h-old timestamp")
	}
}

func TestStartAutoUpdate_Idempotent(t *testing.T) {
	c := NewClawNetClient()
	defer c.StopAutoUpdate()

	c.StartAutoUpdate(func(string) {})
	c.StartAutoUpdate(func(string) {})
	c.StartAutoUpdate(func(string) {})
	// No panic = pass.
}

func TestStopAutoUpdate_BeforeStart(t *testing.T) {
	c := NewClawNetClient()
	// StopAutoUpdate before StartAutoUpdate should not panic.
	c.StopAutoUpdate()
	c.StopAutoUpdate()
}

func TestAutoUpdate_RestartAfterStop(t *testing.T) {
	c := NewClawNetClient()

	c.StartAutoUpdate(func(string) {})
	c.StopAutoUpdate()

	// After stop, StartAutoUpdate should be able to launch again.
	c.StartAutoUpdate(func(string) {})
	c.StopAutoUpdate()
}
