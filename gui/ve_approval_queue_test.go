package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewApprovalQueue_Basic(t *testing.T) {
	q := NewApprovalQueue(50, 100)
	if q.MaxSize() != 50 {
		t.Errorf("MaxSize() = %d, want 50", q.MaxSize())
	}
	if q.DailyQuotaLimit() != 100 {
		t.Errorf("DailyQuotaLimit() = %d, want 100", q.DailyQuotaLimit())
	}
	if q.PendingCount() != 0 {
		t.Errorf("PendingCount() = %d, want 0", q.PendingCount())
	}
}

func TestNewApprovalQueueFromConfig(t *testing.T) {
	cfg := &VEApprovalConfig{
		MaxQueueSize:     75,
		DailyQuota:       200,
		FallbackApprover: "fallback-ve-1",
	}
	q := NewApprovalQueueFromConfig(cfg)
	if q.MaxSize() != 75 {
		t.Errorf("MaxSize() = %d, want 75", q.MaxSize())
	}
	if q.DailyQuotaLimit() != 200 {
		t.Errorf("DailyQuotaLimit() = %d, want 200", q.DailyQuotaLimit())
	}
	if q.FallbackApproverID() != "fallback-ve-1" {
		t.Errorf("FallbackApproverID() = %q, want %q", q.FallbackApproverID(), "fallback-ve-1")
	}
}

func TestNewApprovalQueueFromConfig_Nil(t *testing.T) {
	q := NewApprovalQueueFromConfig(nil)
	if q.MaxSize() != 50 {
		t.Errorf("MaxSize() = %d, want 50 (default)", q.MaxSize())
	}
	if q.DailyQuotaLimit() != 100 {
		t.Errorf("DailyQuotaLimit() = %d, want 100 (default)", q.DailyQuotaLimit())
	}
}

func TestApprovalQueue_Submit_AcceptsWithinCapacity(t *testing.T) {
	q := NewApprovalQueue(3, 100)
	for i := 0; i < 3; i++ {
		result := q.Submit(fmt.Sprintf("req-%d", i))
		if !result.Accepted {
			t.Fatalf("request %d should be accepted", i)
		}
	}
	if q.PendingCount() != 3 {
		t.Errorf("PendingCount() = %d, want 3", q.PendingCount())
	}
}

func TestApprovalQueue_Submit_RejectsWhenFull_NoFallback(t *testing.T) {
	q := NewApprovalQueue(2, 100)
	q.Submit("req-1")
	q.Submit("req-2")

	result := q.Submit("req-3")
	if result.Accepted {
		t.Fatal("request should be rejected when queue is full")
	}
	if result.RejectionReason != "queue full" {
		t.Errorf("RejectionReason = %q, want %q", result.RejectionReason, "queue full")
	}
	if result.FallbackApprover != "" {
		t.Errorf("FallbackApprover should be empty, got %q", result.FallbackApprover)
	}
}

func TestApprovalQueue_Submit_RejectsWhenFull_WithFallback(t *testing.T) {
	q := NewApprovalQueue(2, 100)
	q.SetFallbackApprover("fallback-ve")
	q.Submit("req-1")
	q.Submit("req-2")

	result := q.Submit("req-3")
	if result.Accepted {
		t.Fatal("request should be rejected when queue is full")
	}
	if result.RejectionReason != "queue full" {
		t.Errorf("RejectionReason = %q, want %q", result.RejectionReason, "queue full")
	}
	if result.FallbackApprover != "fallback-ve" {
		t.Errorf("FallbackApprover = %q, want %q", result.FallbackApprover, "fallback-ve")
	}
}

func TestApprovalQueue_Submit_RejectsWhenDailyQuotaExceeded_NoFallback(t *testing.T) {
	q := NewApprovalQueue(100, 3)
	for i := 0; i < 3; i++ {
		result := q.Submit(fmt.Sprintf("req-%d", i))
		if !result.Accepted {
			t.Fatalf("request %d should be accepted within quota", i)
		}
		q.Dequeue(fmt.Sprintf("req-%d", i))
	}

	result := q.Submit("req-4")
	if result.Accepted {
		t.Fatal("request should be rejected when daily quota exceeded")
	}
	if result.RejectionReason != "daily quota exceeded" {
		t.Errorf("RejectionReason = %q, want %q", result.RejectionReason, "daily quota exceeded")
	}
}

func TestApprovalQueue_Submit_RejectsWhenDailyQuotaExceeded_WithFallback(t *testing.T) {
	q := NewApprovalQueue(100, 2)
	q.SetFallbackApprover("fallback-human")
	q.Submit("req-1")
	q.Dequeue("req-1")
	q.Submit("req-2")
	q.Dequeue("req-2")

	result := q.Submit("req-3")
	if result.Accepted {
		t.Fatal("request should be rejected when daily quota exceeded")
	}
	if result.RejectionReason != "daily quota exceeded" {
		t.Errorf("RejectionReason = %q, want %q", result.RejectionReason, "daily quota exceeded")
	}
	if result.FallbackApprover != "fallback-human" {
		t.Errorf("FallbackApprover = %q, want %q", result.FallbackApprover, "fallback-human")
	}
}

func TestApprovalQueue_Dequeue(t *testing.T) {
	q := NewApprovalQueue(10, 100)
	q.Submit("req-1")
	q.Submit("req-2")
	q.Submit("req-3")

	q.Dequeue("req-2")
	if q.PendingCount() != 2 {
		t.Errorf("PendingCount() = %d, want 2 after dequeue", q.PendingCount())
	}
}

func TestApprovalQueue_IsFull(t *testing.T) {
	q := NewApprovalQueue(2, 100)
	if q.IsFull() {
		t.Fatal("queue should not be full when empty")
	}
	q.Submit("req-1")
	q.Submit("req-2")
	if !q.IsFull() {
		t.Fatal("queue should be full with 2/2 items")
	}
	q.Dequeue("req-1")
	if q.IsFull() {
		t.Fatal("queue should not be full after dequeue")
	}
}

func TestApprovalQueue_DailyQuotaNotAffectedByDequeue(t *testing.T) {
	q := NewApprovalQueue(100, 3)
	q.Submit("req-1")
	q.Submit("req-2")
	q.Submit("req-3")
	q.Dequeue("req-1")
	q.Dequeue("req-2")
	q.Dequeue("req-3")

	result := q.Submit("req-4")
	if result.Accepted {
		t.Fatal("request should be rejected: daily quota exhausted regardless of dequeues")
	}
}

func TestApprovalQueue_AcceptsAfterDequeueFreesCapacity(t *testing.T) {
	q := NewApprovalQueue(2, 100)
	q.Submit("req-1")
	q.Submit("req-2")

	result := q.Submit("req-3")
	if result.Accepted {
		t.Fatal("should be rejected when full")
	}

	q.Dequeue("req-1")
	result = q.Submit("req-3")
	if !result.Accepted {
		t.Fatalf("should accept after dequeue freed capacity, got: %s", result.RejectionReason)
	}
}

func TestApprovalQueue_SetFallbackApprover(t *testing.T) {
	q := NewApprovalQueue(1, 100)
	q.Submit("req-1")

	result := q.Submit("req-2")
	if result.FallbackApprover != "" {
		t.Errorf("should have no fallback initially, got %q", result.FallbackApprover)
	}

	q.SetFallbackApprover("new-fallback")
	result = q.Submit("req-3")
	if result.FallbackApprover != "new-fallback" {
		t.Errorf("FallbackApprover = %q, want %q", result.FallbackApprover, "new-fallback")
	}
}

func TestApprovalQueue_ConcurrentSubmit(t *testing.T) {
	q := NewApprovalQueue(50, 1000)
	var wg sync.WaitGroup
	accepted := make(chan string, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result := q.Submit(fmt.Sprintf("req-%d", idx))
			if result.Accepted {
				accepted <- fmt.Sprintf("req-%d", idx)
			}
		}(i)
	}
	wg.Wait()
	close(accepted)

	count := 0
	for range accepted {
		count++
	}
	if count != 50 {
		t.Errorf("expected exactly 50 accepted requests, got %d", count)
	}
}

func TestApprovalQueue_QuotaCheckedBeforeCapacity(t *testing.T) {
	q := NewApprovalQueue(2, 2)
	q.SetFallbackApprover("fb")
	q.Submit("req-1")
	q.Submit("req-2")

	result := q.Submit("req-3")
	if result.Accepted {
		t.Fatal("should be rejected")
	}
	if result.RejectionReason != "daily quota exceeded" {
		t.Errorf("RejectionReason = %q, want %q (quota checked before capacity)",
			result.RejectionReason, "daily quota exceeded")
	}
}

func TestApprovalQueue_DailyQuotaTracking(t *testing.T) {
	q := NewApprovalQueue(100, 5)
	if q.DailyCount() != 0 {
		t.Errorf("DailyCount() = %d, want 0", q.DailyCount())
	}
	q.Submit("req-1")
	q.Submit("req-2")
	if q.DailyCount() != 2 {
		t.Errorf("DailyCount() = %d, want 2", q.DailyCount())
	}
}
