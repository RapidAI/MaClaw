package main

import (
	"context"
	"testing"
	"time"
)

func TestScreenLeaseWaitsWhileMemoryIsHigh(t *testing.T) {
	prev := desktopMemoryPressured
	prevWait := desktopMemoryRecheck
	t.Cleanup(func() {
		desktopMemoryPressured = prev
		desktopMemoryRecheck = prevWait
	})
	desktopMemoryRecheck = time.Millisecond
	n := 0
	desktopMemoryPressured = func(context.Context, string, string) (bool, string) {
		n++
		if n < 3 {
			return true, desktopMemoryWaitLine
		}
		return false, ""
	}
	start := time.Now()
	if err := waitDesktopMemory(context.Background(), "tenant-a", "alice", "run-mem"); err != nil {
		t.Fatal(err)
	}
	if n < 3 || time.Since(start) > time.Second {
		t.Fatalf("reads=%d after %s", n, time.Since(start))
	}
	if got := loadDesktopActivity("run-mem"); got != "正在等待内存" {
		t.Fatalf("activity %q", got)
	}
}

func TestMemoryGaugeReadsTheContainer(t *testing.T) {
	prev := desktopRemoteMemory
	t.Cleanup(func() { desktopRemoteMemory = prev })
	desktopRemoteMemory = func(context.Context, string, string) (int64, int64, error) {
		return 90, 100, nil
	}
	pressed, line := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	if !pressed || line != desktopMemoryWaitLine {
		t.Fatalf("pressed=%v line=%q", pressed, line)
	}
	desktopRemoteMemory = func(context.Context, string, string) (int64, int64, error) {
		return 10, 100, nil
	}
	pressed, line = readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	if pressed || line != "" {
		t.Fatalf("recovered pressed=%v line=%q", pressed, line)
	}
}
