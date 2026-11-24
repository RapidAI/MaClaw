package cloudagent

import (
	"testing"
	"time"
)

func TestHistoryReportsErrorFlagWithoutChangingLines(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	requestID, created, err := b.Begin("c1", "task_1", "实现登录", now)
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin %v %v %s", err, created, requestID)
	}
	wrote, err := b.Finish(requestID, "云端官方模型未接通", true, now.Add(time.Second))
	if err != nil || !wrote {
		t.Fatalf("finish %v %v", err, wrote)
	}
	lines, err := b.History("task_1")
	if err != nil || len(lines) != 2 || lines[0] != "user\n实现登录" || lines[1] != "assistant\n云端官方模型未接通" {
		t.Fatalf("lines %#v %v", lines, err)
	}
	_, flags, err := b.history("task_1")
	if err != nil || len(flags) != 2 || flags[0] || !flags[1] {
		t.Fatalf("flags %v %v", flags, err)
	}
}

func TestCancelLeavesUserLineAndTurnStatus(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, _, err := b.Begin("c1", "task_1", "实现登录", now); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CancelRunning("task_1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	lines, err := b.History("task_1")
	if err != nil || len(lines) != 1 || lines[0] != "user\n实现登录" {
		t.Fatalf("lines %#v %v", lines, err)
	}
	status, err := b.latestTurn("task_1")
	if err != nil || status != "cancelled" {
		t.Fatalf("turn %q %v", status, err)
	}
}
