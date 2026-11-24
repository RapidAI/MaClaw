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

func TestQueuedTurnsFIFOAndDedup(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := b.Enqueue("c1", "task_1", "第一条", now); err != nil {
		t.Fatal(err)
	}
	second, err := b.Enqueue("c2", "task_1", "第二条", now.Add(time.Second))
	if err != nil || second == "" {
		t.Fatalf("enqueue second %q %v", second, err)
	}
	dup, err := b.Enqueue("c1", "task_1", "第一条", now.Add(2*time.Second))
	if err != nil || dup == "" {
		t.Fatalf("enqueue dup %q %v", dup, err)
	}
	msg, ok, err := b.PeekOldest()
	if err != nil || !ok || msg.Text != "第一条" {
		t.Fatalf("peek %#v %v %v", msg, ok, err)
	}
	msg, ok, err = b.PeekOldest()
	if err != nil || !ok || msg.Text != "第一条" {
		t.Fatalf("peek twice %#v %v %v", msg, ok, err)
	}
	if err := b.DropQueued(msg.QueueID); err != nil {
		t.Fatal(err)
	}
	msg, ok, err = b.PeekOldest()
	if err != nil || !ok || msg.Text != "第二条" || msg.QueueID != second {
		t.Fatalf("peek second %#v %v %v", msg, ok, err)
	}
	// A begun turn dedups through client_messages, not the queue.
	if _, _, err := b.Begin("c2", "task_1", "第二条", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	back, err := b.Enqueue("c2", "task_1", "第二条", now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if back == "" {
		t.Fatal("enqueue after begin returned empty id")
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
