package workflow

import (
	"context"
	"fmt"
	"sort"
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
	// delivering tracks in-flight tryDeliverPending keys (instance|node|approver)
	// so concurrent opportunistic + ticker paths do not double-Dispatch.
	delivering     map[string]struct{}
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
		delivering:    make(map[string]struct{}),
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
	return m.hasPending(instanceID, nodeID, "")
}

// HasPendingApprover reports whether a specific human approver is still queued
// for the instance/node. Used after Escalate so immediate redelivery is not
// mistaken for a queue entry when another approver is already pending.
func (m *EscalationManager) HasPendingApprover(instanceID, nodeID, approverID string) bool {
	return m.hasPending(instanceID, nodeID, approverID)
}

func (m *EscalationManager) hasPending(instanceID, nodeID, approverID string) bool {
	if m == nil {
		return false
	}
	instanceID = strings.TrimSpace(instanceID)
	nodeID = strings.TrimSpace(nodeID)
	approverID = strings.TrimSpace(approverID)
	if instanceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.queue {
		if !pendingQueueMatch(req, instanceID, nodeID, approverID) {
			continue
		}
		return true
	}
	return false
}

// PendingApprovers returns sorted human-approver ids still queued for the
// instance/node. Used to keep instance_data.escalation_approvers in sync with
// the live queue after deliver/fail hooks.
func (m *EscalationManager) PendingApprovers(instanceID, nodeID string) []string {
	if m == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	nodeID = strings.TrimSpace(nodeID)
	if instanceID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, req := range m.queue {
		if !pendingQueueMatch(req, instanceID, nodeID, "") {
			continue
		}
		a := strings.TrimSpace(req.HumanApprover)
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func pendingQueueMatch(req *EscalationRequest, instanceID, nodeID, approverID string) bool {
	if req == nil || req.Status != EscalationPending {
		return false
	}
	if strings.TrimSpace(req.InstanceID) != instanceID {
		return false
	}
	if nodeID != "" && strings.TrimSpace(req.NodeID) != nodeID {
		return false
	}
	if approverID != "" && strings.TrimSpace(req.HumanApprover) != approverID {
		return false
	}
	return true
}

// Escalate attempts to escalate a request to the configured human approver.
// If the human approver is unavailable, the request is retained in the
// pending-escalation queue and retried asynchronously.
// Duplicate queue entries for the same instance+node+approver are coalesced
// (no extra audit noise). If the peer is already queued but now available,
// Escalate tries one opportunistic redelivery instead of waiting for the ticker.
func (m *EscalationManager) Escalate(ctx context.Context, req *ApprovalRequest, humanApprover string) error {
	humanApprover = strings.TrimSpace(humanApprover)
	if req == nil || humanApprover == "" {
		return fmt.Errorf("escalation request and human approver are required")
	}
	instanceID := strings.TrimSpace(req.InstanceID)
	nodeID := strings.TrimSpace(req.NodeID)

	// Already queued for this peer → opportunistic redelivery if online now;
	// otherwise leave the existing retry entry (do not reset Attempts / re-audit).
	if existing := m.findPending(instanceID, nodeID, humanApprover); existing != nil {
		// Prefer the latest approval payload when re-dispatching (under lock).
		m.mu.Lock()
		if cur, ok := m.queue[existing.ID]; ok && cur != nil && cur.Status == EscalationPending && req != nil {
			cur.ApprovalReq = req
		}
		m.mu.Unlock()
		_ = m.tryDeliverPending(ctx, existing, "opportunistic")
		return nil
	}

	// First attempt: try to dispatch immediately.
	if m.tryDispatch(ctx, req, humanApprover) {
		return nil
	}

	// Human approver is unavailable — record in audit trail and queue for retry.
	m.appendAudit(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
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
		InstanceID:    instanceID,
		NodeID:        nodeID,
		Status:        EscalationPending,
		Attempts:      1,
		LastAttemptAt: now,
		CreatedAt:     now,
	}

	m.mu.Lock()
	// Re-check under lock: concurrent Escalate for the same peer.
	for _, existing := range m.queue {
		if pendingQueueMatch(existing, instanceID, nodeID, humanApprover) {
			m.mu.Unlock()
			return nil
		}
	}
	m.queue[escReq.ID] = escReq
	m.mu.Unlock()

	return nil
}

// findPending returns the live queue entry for instance+node+approver, or nil.
func (m *EscalationManager) findPending(instanceID, nodeID, approverID string) *EscalationRequest {
	if m == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	nodeID = strings.TrimSpace(nodeID)
	approverID = strings.TrimSpace(approverID)
	if instanceID == "" || approverID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.queue {
		if pendingQueueMatch(req, instanceID, nodeID, approverID) {
			return req
		}
	}
	return nil
}

// tryDispatch performs a one-shot availability check + Dispatch.
func (m *EscalationManager) tryDispatch(ctx context.Context, req *ApprovalRequest, humanApprover string) bool {
	if m == nil || req == nil || strings.TrimSpace(humanApprover) == "" {
		return false
	}
	if m.checker == nil || !m.checker.IsAvailable(ctx, humanApprover) {
		return false
	}
	if m.dispatcher == nil {
		return false
	}
	return m.dispatcher.Dispatch(ctx, req, humanApprover) == nil
}

// tryDeliverPending attempts one immediate redelivery for a queued escalation.
// On success it removes the entry, audits delivery, and fires deliveredHook.
// path is recorded in the audit details (e.g. "opportunistic", "retry").
func (m *EscalationManager) tryDeliverPending(ctx context.Context, escReq *EscalationRequest, path string) bool {
	if m == nil || escReq == nil {
		return false
	}
	if path == "" {
		path = "deliver"
	}
	// Still must be pending in the map (another path may have failed/delivered).
	m.mu.Lock()
	current, ok := m.queue[escReq.ID]
	if !ok || current == nil || current.Status != EscalationPending {
		m.mu.Unlock()
		return false
	}
	// Snapshot for dispatch outside the lock.
	req := current.ApprovalReq
	approver := current.HumanApprover
	attempt := current.Attempts
	instanceID := current.InstanceID
	nodeID := current.NodeID
	dkey := deliverKey(instanceID, nodeID, approver)
	if m.delivering == nil {
		m.delivering = make(map[string]struct{})
	}
	if _, busy := m.delivering[dkey]; busy {
		m.mu.Unlock()
		return false // another path is already Dispatching this peer
	}
	m.delivering[dkey] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.delivering, dkey)
		m.mu.Unlock()
	}()

	if !m.tryDispatch(ctx, req, approver) {
		return false
	}

	m.mu.Lock()
	// Re-validate after I/O: do not resurrect a failed/removed entry.
	if cur, still := m.queue[escReq.ID]; !still || cur == nil || cur.Status != EscalationPending {
		m.mu.Unlock()
		return true // already cleaned up; treat as delivered for callers
	}
	delete(m.queue, escReq.ID)
	m.mu.Unlock()

	m.appendAudit(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: escReq.InstanceID,
		NodeID:     escReq.NodeID,
		EventType:  "escalation_delivered",
		ActorID:    escReq.HumanApprover,
		Details:    fmt.Sprintf(`{"attempt":%d,"path":"%s"}`, attempt, path),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})
	if m.deliveredHook != nil {
		m.deliveredHook(ctx, escReq)
	}
	return true
}

// appendAudit is a nil-safe audit write used by retry/fail paths.
func (m *EscalationManager) appendAudit(ctx context.Context, entry *AuditEntry) {
	if m == nil || m.auditStore == nil || entry == nil {
		return
	}
	_ = m.auditStore.Append(ctx, entry)
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

// CancelForInstance drops pending escalations for the instance (and optional node).
// Empty nodeID cancels all nodes for the instance. Does not fire delivered/failed
// hooks — callers own instance_data cleanup and terminal status transitions.
// Returns how many queue entries were removed.
func (m *EscalationManager) CancelForInstance(instanceID, nodeID string) int {
	if m == nil {
		return 0
	}
	instanceID = strings.TrimSpace(instanceID)
	nodeID = strings.TrimSpace(nodeID)
	if instanceID == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for id, req := range m.queue {
		if req == nil || req.Status != EscalationPending {
			continue
		}
		if strings.TrimSpace(req.InstanceID) != instanceID {
			continue
		}
		if nodeID != "" && strings.TrimSpace(req.NodeID) != nodeID {
			continue
		}
		delete(m.queue, id)
		removed++
	}
	return removed
}

// deliverKey builds the in-flight dispatch key for an escalation peer.
func deliverKey(instanceID, nodeID, approverID string) string {
	return strings.TrimSpace(instanceID) + "|" + strings.TrimSpace(nodeID) + "|" + strings.TrimSpace(approverID)
}

// RestorePending re-enqueues a peer after Hub restart without an immediate
// dispatch attempt and without writing a new "unavailable" audit row.
// LastAttemptAt is set in the past so the next processPending cycle is due.
// Returns true when a new queue entry was created.
func (m *EscalationManager) RestorePending(req *ApprovalRequest, humanApprover, nodeID string) bool {
	if m == nil || req == nil {
		return false
	}
	humanApprover = strings.TrimSpace(humanApprover)
	instanceID := strings.TrimSpace(req.InstanceID)
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		nodeID = strings.TrimSpace(req.NodeID)
	}
	if humanApprover == "" || instanceID == "" || nodeID == "" {
		return false
	}
	if m.hasPending(instanceID, nodeID, humanApprover) {
		return false
	}
	now := time.Now().UTC()
	// Due immediately on the next processPendingEscalations pass.
	lastAttempt := now
	if m.retryInterval > 0 {
		lastAttempt = now.Add(-m.retryInterval)
	} else {
		lastAttempt = now.Add(-time.Second)
	}
	escReq := &EscalationRequest{
		ID:            generateID("esc"),
		ApprovalReq:   req,
		HumanApprover: humanApprover,
		InstanceID:    instanceID,
		NodeID:        nodeID,
		Status:        EscalationPending,
		Attempts:      1,
		LastAttemptAt: lastAttempt,
		CreatedAt:     now,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.queue {
		if pendingQueueMatch(existing, instanceID, nodeID, humanApprover) {
			return false
		}
	}
	m.queue[escReq.ID] = escReq
	return true
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
	if m == nil || escReq == nil {
		return
	}
	m.mu.Lock()
	// Drop if another path already failed/delivered this entry.
	if current, ok := m.queue[escReq.ID]; !ok || current == nil || current.Status != EscalationPending {
		m.mu.Unlock()
		return
	}
	escReq.Attempts++
	escReq.LastAttemptAt = time.Now().UTC()
	attempt := escReq.Attempts
	m.mu.Unlock()

	// Shared deliver path (also used by opportunistic Escalate redelivery).
	if m.tryDeliverPending(ctx, escReq, "retry") {
		return
	}

	// Still unavailable — record retry attempt in audit trail.
	m.appendAudit(ctx, &AuditEntry{
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
	if m == nil || escReq == nil {
		return
	}
	now := time.Now().UTC()

	m.mu.Lock()
	escReq.Status = EscalationFailed
	escReq.FailedAt = &now
	// Remove from queue to prevent memory leak.
	delete(m.queue, escReq.ID)
	m.mu.Unlock()

	m.appendAudit(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: escReq.InstanceID,
		NodeID:     escReq.NodeID,
		EventType:  "escalation_failed",
		ActorID:    escReq.HumanApprover,
		Details:    fmt.Sprintf(`{"reason":"max retries exhausted","total_attempts":%d}`, escReq.Attempts),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Prefer failedHook (WorkflowExecutor.markNodeBlocked) for a single
	// NotifyInitiator path — avoids double ve:workflow_status when both are wired.
	if m.failedHook != nil {
		m.failedHook(ctx, escReq)
		return
	}
	// Standalone manager (no executor hook): still push so desktops can attention.
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
}
