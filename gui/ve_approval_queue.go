package main

import (
	"sync"
	"time"
)

type ApprovalQueue struct {
	mu               sync.Mutex
	maxSize          int
	pending          []string
	dailyQuota       int
	dailyCount       int
	dailyResetDate   string
	fallbackApprover string
}

type QueueSubmitResult struct {
	Accepted         bool
	RejectionReason  string
	FallbackApprover string
}

func NewApprovalQueue(maxSize, dailyQuota int) *ApprovalQueue {
	return &ApprovalQueue{
		maxSize:        maxSize,
		pending:        make([]string, 0),
		dailyQuota:     dailyQuota,
		dailyResetDate: todayDateString(),
	}
}

func NewApprovalQueueFromConfig(cfg *VEApprovalConfig) *ApprovalQueue {
	if cfg == nil {
		return NewApprovalQueue(50, 100)
	}
	q := NewApprovalQueue(cfg.MaxQueueSize, cfg.DailyQuota)
	q.fallbackApprover = cfg.FallbackApprover
	return q
}

func (q *ApprovalQueue) Submit(requestID string) QueueSubmitResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maybeResetDailyCount()
	if q.dailyCount >= q.dailyQuota {
		return q.buildRejection("daily quota exceeded")
	}
	if len(q.pending) >= q.maxSize {
		return q.buildRejection("queue full")
	}
	q.pending = append(q.pending, requestID)
	q.dailyCount++
	return QueueSubmitResult{Accepted: true}
}

func (q *ApprovalQueue) Enqueue(requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, requestID)
}

func (q *ApprovalQueue) Dequeue(requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, id := range q.pending {
		if id == requestID {
			q.pending = append(q.pending[:i], q.pending[i+1:]...)
			return
		}
	}
}

func (q *ApprovalQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *ApprovalQueue) IsFull() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending) >= q.maxSize
}

func (q *ApprovalQueue) MaxSize() int { return q.maxSize }

func (q *ApprovalQueue) DailyQuotaLimit() int { return q.dailyQuota }

func (q *ApprovalQueue) DailyCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maybeResetDailyCount()
	return q.dailyCount
}

func (q *ApprovalQueue) DailyQuotaExceeded() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maybeResetDailyCount()
	return q.dailyCount >= q.dailyQuota
}

func (q *ApprovalQueue) IncrementDailyCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maybeResetDailyCount()
	q.dailyCount++
	return q.dailyCount
}

func (q *ApprovalQueue) SetFallbackApprover(approverID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fallbackApprover = approverID
}

func (q *ApprovalQueue) FallbackApproverID() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.fallbackApprover
}

func (q *ApprovalQueue) buildRejection(reason string) QueueSubmitResult {
	result := QueueSubmitResult{Accepted: false, RejectionReason: reason}
	if q.fallbackApprover != "" {
		result.FallbackApprover = q.fallbackApprover
	}
	return result
}

func (q *ApprovalQueue) maybeResetDailyCount() {
	today := todayDateString()
	if today != q.dailyResetDate {
		q.dailyCount = 0
		q.dailyResetDate = today
	}
}

func todayDateString() string {
	return time.Now().Format("2006-01-02")
}
