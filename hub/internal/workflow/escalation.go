package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Escalation retry constants.
const (
	// EscalationRetryInterval is the time between retry attempts.
	EscalationRetryInterval = 60 * time.Second

	// EscalationMaxRetries is the maximum number of escalation attempts.
	EscalationMaxRetries = 5
)

// EscalationStatus represents the current state of an escalation request.
type EscalationStatus string

const (
	EscalationPending EscalationStatus = "pending_escalation"
	EscalationFailed  EscalationStatus = "escalation_failed"
)

// EscalationRequest represents a request that is pending escalation to a human approver.
type EscalationRequest struct {
	ID             string           `json:"id"`
	ApprovalReq    *ApprovalRequest `json:"approval_request"`
	HumanApprover  string           `json:"human_approver"`
	InstanceID     string           `json:"instance_id"`
	NodeID         string           `json:"node_id"`
	Status         EscalationStatus `json:"status"`
	Attempts       int              `json:"attempts"`
	LastAttemptAt  time.Time        `json:"last_attempt_at"`
	CreatedAt      time.Time        `json:"created_at"`
	FailedAt       *time.Time       `json:"failed_at,omitempty"`
}

// HumanApproverChecker checks whether a human approver is available to receive escalations.
type HumanApproverChecker interface {
	IsAvailable(ctx context.Context, approverID string) bool
}

// EscalationFailedHook is invoked after an escalation exhausts max retries
// (audit + optional initiator notify already recorded). Used by WorkflowExecutor
// to mark the approval node blocked so directory/reconcile stay consistent.
type EscalationFailedHook func(ctx context.Context, esc *EscalationRequest)

// EscalationDeliveredHook is invoked after a queued escalation is successfully
// dispatched so the executor can clear per-approver pending markers.
type EscalationDeliveredHook func(ctx context.Context, esc *EscalationRequest)

// EscalationManager manages the pending-escalation queue and retry logic.
type EscalationManager struct {
	mu             sync.Mutex
	queue          map[string]*EscalationRequest // keyed by escalation request ID
	dispatcher     ApprovalDispatcher
	auditStore     AuditStore
	checker        HumanApproverChecker
	notifier       WorkflowNotifier // optional: push ve:workflow_status when escalation fails
	failedHook     EscalationFailedHook
	deliveredHook  EscalationDeliveredHook
	retryInterval  time.Duration
	maxRetries     int
	stopCh         chan struct{}
	stopped        bool
}

// NewEscalationManager creates a new EscalationManager with the given dependencies.
func NewEscalationManager(dispatcher ApprovalDispatcher, auditStore AuditStore, checker HumanApproverChecker) *EscalationManager {
	return &EscalationManager{
		queue:         make(map[string]*EscalationRequest),
		dispatcher:    dispatcher,
		auditStore:    auditStore,
		checker:       checker,
		retryInterval: EscalationRetryInterval,
		maxRetries:    EscalationMaxRetries,
		stopCh:        make(chan struct{}),
	}
}

// SetNotifier wires an optional WorkflowNotifier so exhausted escalations can
// push blocked/escalation status to the initiator's machines (desktop attention).
// Returns the manager for fluent wiring in the Hub router.
func (m *EscalationManager) SetNotifier(n WorkflowNotifier) *EscalationManager {
	if m == nil {
		return nil
	}
	m.notifier = n
	return m
}

// SetFailedHook registers a callback after max-retries escalation failure.
func (m *EscalationManager) SetFailedHook(h EscalationFailedHook) *EscalationManager {
	if m == nil {
		return nil
	}
	m.failedHook = h
	return m
}

// SetDeliveredHook registers a callback after a queued escalation is delivered.
func (m *EscalationManager) SetDeliveredHook(h EscalationDeliveredHook) *EscalationManager {
	if m == nil {
		return nil
	}
	m.deliveredHook = h
	return m
}

// HasPendingForInstance reports whether any pending escalation targets the
// given instance (and optional node). Used by timeout handling to avoid
// short-circuiting EscalationManager retries with an early markNodeBlocked.
func (m *EscalationManager) HasPendingForInstance(instanceID, nodeID string) bool {
	if m == nil {
		return false
	}
	instanceID = strings.TrimSpace(instanceID)
	nodeID = strings.TrimSpace(nodeID)
	if instanceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.queue {
		if req == nil || req.Status != EscalationPending {
			continue
		}
		if strings.TrimSpace(req.InstanceID) != instanceID {
			continue
		}
		if nodeID != "" && strings.TrimSpace(req.NodeID) != nodeID {
			continue
		}
		return true
	}
	return false
}

// Escalate attempts to escalate a request to the configured human approver.
// If the human approver is unavailable, the request is retained in the
// pending-escalation queue and retried asynchronously.
func (m *EscalationManager) Escalate(ctx context.Context, req *ApprovalRequest, humanApprover string) error {
	// First attempt: try to dispatch immediately.
	if m.checker.IsAvailable(ctx, humanApprover) {
		if err := m.dispatcher.Dispatch(ctx, req, humanApprover); err == nil {
			return nil
		}
	}

	// Human approver is unavailable — record in audit trail and queue for retry.
	_ = m.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: req.InstanceID,
		NodeID:     req.NodeID,
		EventType:  "escalation_unavailable",
		ActorID:    humanApprover,
		Details:    `{"reason":"human approver unavailable","attempt":1}`,
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	now := time.Now().UTC()
	escReq := &EscalationRequest{
		ID:            generateID("esc"),
		ApprovalReq:   req,
		HumanApprover: humanApprover,
		InstanceID:    req.InstanceID,
		NodeID:        req.NodeID,
		Status:        EscalationPending,
		Attempts:      1,
		LastAttemptAt: now,
		CreatedAt:     now,
	}

	m.mu.Lock()
	m.queue[escReq.ID] = escReq
	m.mu.Unlock()

	return nil
}

// Start begins the background retry loop. It periodically checks pending
// escalation requests and retries dispatching them to the human approver.
func (m *EscalationManager) Start() {
	go m.retryLoop()
}

// Stop terminates the background retry loop.
func (m *EscalationManager) Stop() {
	m.mu.Lock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
	m.mu.Unlock()
}

// PendingCount returns the number of requests currently in the pending-escalation queue.
func (m *EscalationManager) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, req := range m.queue {
		if req.Status == EscalationPending {
			count++
		}
	}
	return count
}

// GetRequest returns an escalation request by ID, or nil if not found.
func (m *EscalationManager) GetRequest(id string) *EscalationRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queue[id]
}

// retryLoop runs in a background goroutine, periodically retrying pending escalations.
func (m *EscalationManager) retryLoop() {
	ticker := time.NewTicker(m.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.processPendingEscalations()
		}
	}
}

// processPendingEscalations iterates over all pending escalation requests
// and attempts to retry dispatching them.
func (m *EscalationManager) processPendingEscalations() {
	m.mu.Lock()
	// Collect pending requests that are due for retry.
	var pending []*EscalationRequest
	now := time.Now().UTC()
	for _, req := range m.queue {
		if req.Status != EscalationPending {
			continue
		}
		if now.Sub(req.LastAttemptAt) >= m.retryInterval {
			pending = append(pending, req)
		}
	}
	m.mu.Unlock()

	ctx := context.Background()
	for _, escReq := range pending {
		m.retryEscalation(ctx, escReq)
	}
}

// retryEscalation attempts a single retry for an escalation request.
func (m *EscalationManager) retryEscalation(ctx context.Context, escReq *EscalationRequest) {
	m.mu.Lock()
	escReq.Attempts++
	escReq.LastAttemptAt = time.Now().UTC()
	attempt := escReq.Attempts
	m.mu.Unlock()

	// Check if human approver is now available and try to dispatch.
	success := false
	if m.checker.IsAvailable(ctx, escReq.HumanApprover) {
		if err := m.dispatcher.Dispatch(ctx, escReq.ApprovalReq, escReq.HumanApprover); err == nil {
			success = true
		}
	}

	if success {
		// Escalation succeeded — remove from queue.
		_ = m.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: escReq.InstanceID,
			NodeID:     escReq.NodeID,
			EventType:  "escalation_delivered",
			ActorID:    escReq.HumanApprover,
			Details:    fmt.Sprintf(`{"attempt":%d}`, attempt),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		})

		m.mu.Lock()
		delete(m.queue, escReq.ID)
		m.mu.Unlock()
		if m.deliveredHook != nil {
			m.deliveredHook(ctx, escReq)
		}
		return
	}

	// Still unavailable — record retry attempt in audit trail.
	_ = m.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: escReq.InstanceID,
		NodeID:     escReq.NodeID,
		EventType:  "escalation_unavailable",
		ActorID:    escReq.HumanApprover,
		Details:    fmt.Sprintf(`{"reason":"human approver unavailable","attempt":%d}`, attempt),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Check if max retries exhausted.
	if attempt >= m.maxRetries {
		m.markEscalationFailed(ctx, escReq)
	}
}

// markEscalationFailed marks an escalation request as failed after all retries are exhausted
// and removes it from the queue to prevent memory leak.
func (m *EscalationManager) markEscalationFailed(ctx context.Context, escReq *EscalationRequest) {
	now := time.Now().UTC()

	m.mu.Lock()
	escReq.Status = EscalationFailed
	escReq.FailedAt = &now
	// Remove from queue to prevent memory leak.
	delete(m.queue, escReq.ID)
	m.mu.Unlock()

	_ = m.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: escReq.InstanceID,
		NodeID:     escReq.NodeID,
		EventType:  "escalation_failed",
		ActorID:    escReq.HumanApprover,
		Details:    fmt.Sprintf(`{"reason":"max retries exhausted","total_attempts":%d}`, escReq.Attempts),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Push to initiator machines so MaClaw can project attention without waiting
	// for the next directory reconcile. Reason includes "escalation" so Hub
	// participant notifier classifies event=escalation / urgency=overdue.
	if m.notifier != nil && strings.TrimSpace(escReq.InstanceID) != "" {
		reason := fmt.Sprintf(
			"escalation failed: human approver %s unavailable after %d attempts",
			escReq.HumanApprover, escReq.Attempts,
		)
		details := fmt.Sprintf(
			"node=%s approver=%s reason=max_retries_exhausted",
			escReq.NodeID, escReq.HumanApprover,
		)
		_ = m.notifier.NotifyInitiator(ctx, escReq.InstanceID, reason, details)
	}

	// Executor marks the node blocked so Hub directory / desktop reconcile align.
	if m.failedHook != nil {
		m.failedHook(ctx, escReq)
	}
}
