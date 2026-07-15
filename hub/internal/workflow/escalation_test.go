package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Test doubles ---

type mockHumanChecker struct {
	mu        sync.Mutex
	available bool
}

func (c *mockHumanChecker) IsAvailable(ctx context.Context, approverID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available
}

func (c *mockHumanChecker) setAvailable(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.available = v
}

type mockDispatcherForEsc struct {
	mu        sync.Mutex
	calls     []string // approver IDs dispatched to
	failNext  bool
}

func (d *mockDispatcherForEsc) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failNext {
		return context.DeadlineExceeded
	}
	d.calls = append(d.calls, approverID)
	return nil
}

func (d *mockDispatcherForEsc) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, "fallback:"+fallbackID)
	return nil
}

func (d *mockDispatcherForEsc) dispatchCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

type mockAuditStoreForEsc struct {
	mu      sync.Mutex
	entries []AuditEntry
}

func (s *mockAuditStoreForEsc) Append(ctx context.Context, entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, *entry)
	return nil
}

func (s *mockAuditStoreForEsc) QueryByInstance(ctx context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *mockAuditStoreForEsc) QueryByApprover(ctx context.Context, approverID string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *mockAuditStoreForEsc) QueryByTimeRange(ctx context.Context, start, end time.Time, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *mockAuditStoreForEsc) QueryByDecision(ctx context.Context, decision string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *mockAuditStoreForEsc) getEntries() []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]AuditEntry, len(s.entries))
	copy(cp, s.entries)
	return cp
}

func (s *mockAuditStoreForEsc) countByEventType(eventType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, e := range s.entries {
		if e.EventType == eventType {
			count++
		}
	}
	return count
}

// --- Tests ---

func TestEscalate_ImmediateSuccess(t *testing.T) {
	checker := &mockHumanChecker{available: true}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)

	req := &ApprovalRequest{
		ID:         "req_1",
		InstanceID: "inst_1",
		NodeID:     "node_1",
	}

	err := mgr.Escalate(context.Background(), req, "human_approver_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have dispatched immediately without queuing.
	if dispatcher.dispatchCount() != 1 {
		t.Errorf("expected 1 dispatch, got %d", dispatcher.dispatchCount())
	}
	if mgr.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", mgr.PendingCount())
	}
	// No audit entries for unavailability.
	if audit.countByEventType("escalation_unavailable") != 0 {
		t.Errorf("expected 0 unavailability audit entries, got %d", audit.countByEventType("escalation_unavailable"))
	}
}

func TestEscalate_UnavailableQueuesRequest(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)

	req := &ApprovalRequest{
		ID:         "req_2",
		InstanceID: "inst_2",
		NodeID:     "node_2",
	}

	err := mgr.Escalate(context.Background(), req, "human_approver_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not have dispatched.
	if dispatcher.dispatchCount() != 0 {
		t.Errorf("expected 0 dispatches, got %d", dispatcher.dispatchCount())
	}
	// Should be in pending queue.
	if mgr.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", mgr.PendingCount())
	}
	// Should have recorded unavailability in audit trail.
	if audit.countByEventType("escalation_unavailable") != 1 {
		t.Errorf("expected 1 unavailability audit entry, got %d", audit.countByEventType("escalation_unavailable"))
	}
}

func TestEscalation_RetrySucceeds(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)
	// Use short interval for testing.
	mgr.retryInterval = 10 * time.Millisecond

	req := &ApprovalRequest{
		ID:         "req_3",
		InstanceID: "inst_3",
		NodeID:     "node_3",
	}

	_ = mgr.Escalate(context.Background(), req, "human_approver_3")

	// Verify queued.
	if mgr.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", mgr.PendingCount())
	}

	// Make approver available before retry.
	checker.setAvailable(true)

	// Manually set LastAttemptAt to past to trigger retry immediately.
	mgr.mu.Lock()
	for _, escReq := range mgr.queue {
		escReq.LastAttemptAt = time.Now().Add(-time.Minute)
	}
	mgr.mu.Unlock()

	// Process pending escalations manually.
	mgr.processPendingEscalations()

	// Should have dispatched successfully.
	if dispatcher.dispatchCount() != 1 {
		t.Errorf("expected 1 dispatch after retry, got %d", dispatcher.dispatchCount())
	}
	// Should be removed from queue.
	if mgr.PendingCount() != 0 {
		t.Errorf("expected 0 pending after successful retry, got %d", mgr.PendingCount())
	}
	// Should have recorded delivery in audit trail.
	if audit.countByEventType("escalation_delivered") != 1 {
		t.Errorf("expected 1 delivery audit entry, got %d", audit.countByEventType("escalation_delivered"))
	}
}

type captureEscNotifier struct {
	mu    sync.Mutex
	calls []struct{ instanceID, reason, details string }
}

func (n *captureEscNotifier) NotifyInitiator(ctx context.Context, instanceID string, reason string, details string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, struct{ instanceID, reason, details string }{instanceID, reason, details})
	return nil
}

func (n *captureEscNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

func TestEscalation_MaxRetriesExhausted(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}
	notifier := &captureEscNotifier{}

	mgr := NewEscalationManager(dispatcher, audit, checker).SetNotifier(notifier)
	mgr.retryInterval = 10 * time.Millisecond
	mgr.maxRetries = 5

	req := &ApprovalRequest{
		ID:         "req_4",
		InstanceID: "inst_4",
		NodeID:     "node_4",
	}

	_ = mgr.Escalate(context.Background(), req, "human_approver_4")

	// Simulate 4 more retry attempts (initial attempt was 1, need 4 more to reach 5).
	for i := 0; i < 4; i++ {
		mgr.mu.Lock()
		for _, escReq := range mgr.queue {
			escReq.LastAttemptAt = time.Now().Add(-time.Minute)
		}
		mgr.mu.Unlock()
		mgr.processPendingEscalations()
	}

	// Should have recorded escalation_failed in audit trail.
	if audit.countByEventType("escalation_failed") != 1 {
		t.Errorf("expected 1 escalation_failed audit entry, got %d", audit.countByEventType("escalation_failed"))
	}

	// Verify the request has been removed from the queue to prevent memory leak.
	mgr.mu.Lock()
	queueLen := len(mgr.queue)
	mgr.mu.Unlock()

	if queueLen != 0 {
		t.Fatalf("expected escalation request to be removed from queue after failure, got %d items", queueLen)
	}

	// Verify PendingCount is 0.
	if mgr.PendingCount() != 0 {
		t.Errorf("expected PendingCount=0, got %d", mgr.PendingCount())
	}

	// Total unavailability entries: 1 (initial) + 4 (retries) = 5.
	if audit.countByEventType("escalation_unavailable") != 5 {
		t.Errorf("expected 5 unavailability audit entries, got %d", audit.countByEventType("escalation_unavailable"))
	}

	// No successful dispatches.
	if dispatcher.dispatchCount() != 0 {
		t.Errorf("expected 0 dispatches, got %d", dispatcher.dispatchCount())
	}

	// Standalone manager (no failedHook): initiator notified once.
	if notifier.callCount() != 1 {
		t.Fatalf("expected 1 NotifyInitiator call on escalation failure, got %d", notifier.callCount())
	}
	notifier.mu.Lock()
	call := notifier.calls[0]
	notifier.mu.Unlock()
	if call.instanceID != "inst_4" {
		t.Fatalf("instanceID=%q", call.instanceID)
	}
	if !strings.Contains(strings.ToLower(call.reason), "escalation") {
		t.Fatalf("reason should mention escalation: %q", call.reason)
	}
}

func TestEscalation_DedupSameApprover(t *testing.T) {
	audit := &mockAuditStoreForEsc{}
	mgr := NewEscalationManager(&mockDispatcherForEsc{}, audit, &mockHumanChecker{available: false})
	req := &ApprovalRequest{ID: "r1", InstanceID: "inst-d", NodeID: "n1"}
	_ = mgr.Escalate(context.Background(), req, "human-a")
	_ = mgr.Escalate(context.Background(), req, "human-a")
	if mgr.PendingCount() != 1 {
		t.Fatalf("PendingCount=%d want 1 (deduped)", mgr.PendingCount())
	}
	if got := mgr.PendingApprovers("inst-d", "n1"); len(got) != 1 || got[0] != "human-a" {
		t.Fatalf("PendingApprovers=%v", got)
	}
	// Coalesce must not spam a second escalation_unavailable audit row.
	if n := audit.countByEventType("escalation_unavailable"); n != 1 {
		t.Fatalf("unavailable audits=%d want 1 (no noise on dedupe)", n)
	}
}

func TestEscalation_OpportunisticRedeliverWhenPeerComesOnline(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}
	hookCalls := 0
	mgr := NewEscalationManager(dispatcher, audit, checker)
	mgr.SetDeliveredHook(func(ctx context.Context, esc *EscalationRequest) {
		hookCalls++
	})
	req := &ApprovalRequest{ID: "r1", InstanceID: "inst-op", NodeID: "n1"}
	_ = mgr.Escalate(context.Background(), req, "human-a")
	if mgr.PendingCount() != 1 {
		t.Fatalf("PendingCount=%d want 1 after offline escalate", mgr.PendingCount())
	}
	// Peer comes online; re-Escalate should deliver without waiting for ticker.
	checker.setAvailable(true)
	_ = mgr.Escalate(context.Background(), req, "human-a")
	if mgr.PendingCount() != 0 {
		t.Fatalf("PendingCount=%d want 0 after opportunistic deliver", mgr.PendingCount())
	}
	if dispatcher.dispatchCount() != 1 {
		t.Fatalf("dispatchCount=%d want 1", dispatcher.dispatchCount())
	}
	if audit.countByEventType("escalation_delivered") != 1 {
		t.Fatalf("delivered audits=%d want 1", audit.countByEventType("escalation_delivered"))
	}
	if hookCalls != 1 {
		t.Fatalf("deliveredHook calls=%d want 1", hookCalls)
	}
	// Attempts must not have been reset/spammed via a second queue entry.
	if n := audit.countByEventType("escalation_unavailable"); n != 1 {
		t.Fatalf("unavailable audits=%d want 1", n)
	}
}

func TestEscalation_NilDepsDoNotPanic(t *testing.T) {
	// Production always wires deps; unit/fuzz paths may construct a bare manager.
	mgr := NewEscalationManager(nil, nil, nil)
	req := &ApprovalRequest{ID: "r1", InstanceID: "inst-nil", NodeID: "n1"}
	if err := mgr.Escalate(context.Background(), req, "human-a"); err != nil {
		t.Fatalf("Escalate with nil deps: %v", err)
	}
	if mgr.PendingCount() != 1 {
		t.Fatalf("PendingCount=%d want 1", mgr.PendingCount())
	}
	// Drive one retry cycle (due immediately). Nil checker/dispatcher must not panic;
	// leave maxRetries high so the entry stays queued for further retries.
	mgr.retryInterval = 0
	mgr.maxRetries = 10
	mgr.processPendingEscalations()
	if mgr.PendingCount() != 1 {
		t.Fatalf("after retry PendingCount=%d want 1", mgr.PendingCount())
	}
}

func TestEscalation_HasPendingApprover_DistinguishesPeers(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}
	mgr := NewEscalationManager(dispatcher, audit, checker)
	req := &ApprovalRequest{ID: "r1", InstanceID: "inst-x", NodeID: "n1"}
	_ = mgr.Escalate(context.Background(), req, "human-a")
	// human-b is available → immediate dispatch success, not queued.
	checker.setAvailable(true)
	_ = mgr.Escalate(context.Background(), req, "human-b")
	if !mgr.HasPendingApprover("inst-x", "n1", "human-a") {
		t.Fatal("human-a should still be pending")
	}
	if mgr.HasPendingApprover("inst-x", "n1", "human-b") {
		t.Fatal("human-b was delivered immediately; must not report pending")
	}
	if !mgr.HasPendingForInstance("inst-x", "n1") {
		t.Fatal("instance still has pending escalations")
	}
}

func TestEscalation_MaxRetries_FailedHookSkipsDirectNotify(t *testing.T) {
	// When failedHook is wired (production executor path), manager must not also
	// NotifyInitiator — markNodeBlocked owns the single desktop push.
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}
	notifier := &captureEscNotifier{}
	hookCalls := 0
	mgr := NewEscalationManager(dispatcher, audit, checker).
		SetNotifier(notifier).
		SetFailedHook(func(ctx context.Context, esc *EscalationRequest) {
			hookCalls++
			if esc == nil || esc.InstanceID != "inst_hook" {
				t.Fatalf("esc=%#v", esc)
			}
		})
	mgr.retryInterval = time.Millisecond
	mgr.maxRetries = 2
	_ = mgr.Escalate(context.Background(), &ApprovalRequest{
		ID: "req", InstanceID: "inst_hook", NodeID: "n1",
	}, "human-1")
	for i := 0; i < 2; i++ {
		mgr.mu.Lock()
		for _, escReq := range mgr.queue {
			escReq.LastAttemptAt = time.Now().Add(-time.Minute)
		}
		mgr.mu.Unlock()
		mgr.processPendingEscalations()
	}
	if hookCalls != 1 {
		t.Fatalf("hookCalls=%d", hookCalls)
	}
	if notifier.callCount() != 0 {
		t.Fatalf("expected no direct NotifyInitiator when failedHook is set, got %d", notifier.callCount())
	}
}

func TestEscalation_RetryIntervalRespected(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)
	mgr.retryInterval = time.Hour // Very long interval.

	req := &ApprovalRequest{
		ID:         "req_5",
		InstanceID: "inst_5",
		NodeID:     "node_5",
	}

	_ = mgr.Escalate(context.Background(), req, "human_approver_5")

	// Process immediately — should NOT retry because interval hasn't elapsed.
	mgr.processPendingEscalations()

	// Still 1 pending, no additional audit entries beyond the initial one.
	if mgr.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", mgr.PendingCount())
	}
	// Only the initial unavailability entry.
	if audit.countByEventType("escalation_unavailable") != 1 {
		t.Errorf("expected 1 unavailability audit entry, got %d", audit.countByEventType("escalation_unavailable"))
	}
}

func TestEscalation_StartStop(t *testing.T) {
	checker := &mockHumanChecker{available: false}
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)
	mgr.retryInterval = 10 * time.Millisecond

	mgr.Start()
	// Give the goroutine time to start.
	time.Sleep(20 * time.Millisecond)
	mgr.Stop()
	// Should not panic on double stop.
	mgr.Stop()
}

func TestEscalation_DispatchFailureQueues(t *testing.T) {
	// Even if checker says available, if dispatch fails, should queue.
	checker := &mockHumanChecker{available: true}
	dispatcher := &mockDispatcherForEsc{failNext: true}
	audit := &mockAuditStoreForEsc{}

	mgr := NewEscalationManager(dispatcher, audit, checker)

	req := &ApprovalRequest{
		ID:         "req_6",
		InstanceID: "inst_6",
		NodeID:     "node_6",
	}

	err := mgr.Escalate(context.Background(), req, "human_approver_6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dispatch failed, should be queued.
	if mgr.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", mgr.PendingCount())
	}
}
