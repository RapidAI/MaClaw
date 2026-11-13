package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
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

func TestMemoryWaitGivesUpInsteadOfWaitingForever(t *testing.T) {
	prev := desktopMemoryPressured
	prevWait := desktopMemoryRecheck
	prevMax := desktopMemoryWaitMax
	t.Cleanup(func() {
		desktopMemoryPressured = prev
		desktopMemoryRecheck = prevWait
		desktopMemoryWaitMax = prevMax
	})
	desktopMemoryRecheck = time.Millisecond
	desktopMemoryWaitMax = 5 * time.Millisecond
	desktopMemoryPressured = func(context.Context, string, string) (bool, string) {
		return true, desktopMemoryWaitLine
	}
	err := waitDesktopMemory(context.Background(), "tenant-a", "alice", "run-mem-giveup")
	var admit *desktop.MemoryAdmission
	if !errors.As(err, &admit) || admit.Kind() != desktop.AdmissionMemoryBusy || admit.Error() != desktop.MemoryBusyOutcome {
		t.Fatalf("err = %v", err)
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
