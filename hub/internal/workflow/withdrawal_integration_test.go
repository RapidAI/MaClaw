package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Mock stores for withdrawal integration tests
// =============================================================================

// integMockInstanceStore tracks all calls for verification.
type integMockInstanceStore struct {
	mu           sync.Mutex
	instances    map[string]*WorkflowInstance
	pendingExecs []NodeExecution
	updatedNodes []integNodeUpdate
	statusCalls  []integStatusUpdate
}

type integNodeUpdate struct {
	ID         string
	Status     NodeStatus
	FailReason string
}

type integStatusUpdate struct {
	ID     string
	Status InstanceStatus
}

func newIntegMockInstanceStore() *integMockInstanceStore {
	return &integMockInstanceStore{
		instances: make(map[string]*WorkflowInstance),
	}
}

func (s *integMockInstanceStore) Create(_ context.Context, inst *WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.ID] = inst
	return nil
}

func (s *integMockInstanceStore) Get(_ context.Context, id string) (*WorkflowInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return nil, nil
	}
	return inst, nil
}

func (s *integMockInstanceStore) UpdateStatus(_ context.Context, id string, status InstanceStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCalls = append(s.statusCalls, integStatusUpdate{ID: id, Status: status})
	if inst, ok := s.instances[id]; ok {
		inst.Status = status
	}
	return nil
}

func (s *integMockInstanceStore) UpdateCurrentNode(_ context.Context, _, _ string) error { return nil }
func (s *integMockInstanceStore) UpdateInstanceData(_ context.Context, _ string, _ map[string]interface{}) error {
	return nil
}
func (s *integMockInstanceStore) CreateNodeExecution(_ context.Context, _ *NodeExecution) error {
	return nil
}

func (s *integMockInstanceStore) UpdateNodeExecution(_ context.Context, id string, status NodeStatus, _ json.RawMessage, failReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updatedNodes = append(s.updatedNodes, integNodeUpdate{ID: id, Status: status, FailReason: failReason})
	return nil
}

func (s *integMockInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingExecs, nil
}

func (s *integMockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *integMockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *integMockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *integMockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// integMockAuditStore records all audit entries for verification.
type integMockAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

func (s *integMockAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}
func (s *integMockAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *integMockAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *integMockAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *integMockAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

// integMockHubNotifier tracks in-app notifications sent.
type integMockHubNotifier struct {
	mu       sync.Mutex
	sent     []*InAppNotification
	sentTo   []string
	failFor  map[string]error
	failAll  bool
}

func newIntegMockHubNotifier() *integMockHubNotifier {
	return &integMockHubNotifier{failFor: make(map[string]error)}
}

func (m *integMockHubNotifier) Send(_ context.Context, recipientID string, notif *InAppNotification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll {
		return errors.New("hub notifier unavailable")
	}
	if err, ok := m.failFor[recipientID]; ok {
		return err
	}
	m.sent = append(m.sent, notif)
	m.sentTo = append(m.sentTo, recipientID)
	return nil
}

// integMockIMPusher tracks IM push notifications sent.
type integMockIMPusher struct {
	mu        sync.Mutex
	pushed    []*IMPushMessage
	pushedTo  []string
	connected map[string]bool
	failFor   map[string]error
	failAll   bool
}

func newIntegMockIMPusher() *integMockIMPusher {
	return &integMockIMPusher{connected: make(map[string]bool), failFor: make(map[string]error)}
}

func (m *integMockIMPusher) Push(_ context.Context, recipientID string, msg *IMPushMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll {
		return errors.New("im pusher unavailable")
	}
	if err, ok := m.failFor[recipientID]; ok {
		return err
	}
	m.pushed = append(m.pushed, msg)
	m.pushedTo = append(m.pushedTo, recipientID)
	return nil
}

func (m *integMockIMPusher) IsConnected(_ context.Context, recipientID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected[recipientID]
}

// integMockNotifStore tracks notification records.
type integMockNotifStore struct {
	mu      sync.Mutex
	records []*Notification
}

func (m *integMockNotifStore) Create(_ context.Context, notif *Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, notif)
	return nil
}
func (m *integMockNotifStore) Get(_ context.Context, _ string) (*Notification, error) {
	return nil, nil
}
func (m *integMockNotifStore) ListByInstance(_ context.Context, _ string) ([]Notification, error) {
	return nil, nil
}
func (m *integMockNotifStore) ListByRecipient(_ context.Context, _ string) ([]Notification, error) {
	return nil, nil
}
func (m *integMockNotifStore) MarkDelivered(_ context.Context, _ string) error { return nil }
func (m *integMockNotifStore) MarkFailed(_ context.Context, _ string, _ string) error {
	return nil
}

// =============================================================================
// Integration Test: Full Withdrawal Flow (Happy Path)
// =============================================================================

func TestIntegration_Withdrawal_FullFlow(t *testing.T) {
	// Setup: running instance with pending approval nodes and participants.
	instStore := newIntegMockInstanceStore()
	instStore.instances["inst-100"] = &WorkflowInstance{
		ID:     "inst-100",
		Status: InstanceRunning,
		InstanceData: map[string]interface{}{
			"initiator_id":        "user-initiator",
			"initiator_name":      "张三",
			"workflow_name":       "请假审批",
			"pending_participants": []interface{}{"user-approver1", "user-approver2"},
			"approver_ids":        []interface{}{"user-approver1", "user-approver2", "user-approver3"},
		},
	}
	instStore.pendingExecs = []NodeExecution{
		{ID: "exec-a", InstanceID: "inst-100", NodeID: "node-approve-1", Status: NodePending},
		{ID: "exec-b", InstanceID: "inst-100", NodeID: "node-approve-2", Status: NodePending},
		{ID: "exec-c", InstanceID: "inst-100", NodeID: "node-approve-3", Status: NodeCompleted}, // already completed
		{ID: "exec-d", InstanceID: "other-inst", NodeID: "node-other", Status: NodePending},     // different instance
	}

	auditStore := &integMockAuditStore{}

	hubNotifier := newIntegMockHubNotifier()
	imPusher := newIntegMockIMPusher()
	imPusher.connected["user-approver1"] = true
	imPusher.connected["user-approver2"] = true
	imPusher.connected["user-approver3"] = true
	notifStore := &integMockNotifStore{}

	dispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)
	handler := NewWithdrawalHandler(instStore, auditStore, dispatcher, nil)

	// Act: initiator withdraws the instance.
	err := handler.Withdraw(context.Background(), "inst-100", "user-initiator")
	if err != nil {
		t.Fatalf("Withdraw() unexpected error: %v", err)
	}

	// Verify 1: Instance status = "withdrawn".
	inst := instStore.instances["inst-100"]
	if inst.Status != InstanceWithdrawn {
		t.Errorf("instance status = %q, want %q", inst.Status, InstanceWithdrawn)
	}

	// Verify 2: All pending nodes for this instance set to "skipped".
	if len(instStore.updatedNodes) != 2 {
		t.Fatalf("expected 2 node updates, got %d", len(instStore.updatedNodes))
	}
	for _, update := range instStore.updatedNodes {
		if update.Status != NodeSkipped {
			t.Errorf("node %s status = %q, want %q", update.ID, update.Status, NodeSkipped)
		}
	}

	// Verify 3: Withdrawal audit event recorded.
	var withdrawalEventFound bool
	for _, entry := range auditStore.entries {
		if entry.EventType == "withdrawal" && entry.InstanceID == "inst-100" && entry.ActorID == "user-initiator" {
			withdrawalEventFound = true
			break
		}
	}
	if !withdrawalEventFound {
		t.Error("expected 'withdrawal' audit entry not found")
	}

	// Verify 4: All participants with pending actions notified.
	// The instance has pending_participants: [user-approver1, user-approver2]
	// and approver_ids: [user-approver1, user-approver2, user-approver3]
	// Combined unique set: user-approver1, user-approver2, user-approver3
	hubNotifier.mu.Lock()
	hubSentCount := len(hubNotifier.sentTo)
	hubNotifier.mu.Unlock()
	if hubSentCount < 3 {
		t.Errorf("expected at least 3 hub notifications sent, got %d", hubSentCount)
	}

	// Verify 5: Notification content includes workflow_name, initiator_name, "无需进一步操作".
	hubNotifier.mu.Lock()
	for i, notif := range hubNotifier.sent {
		if !strings.Contains(notif.Title, "请假审批") {
			t.Errorf("notification[%d] title missing workflow_name, got %q", i, notif.Title)
		}
		if !strings.Contains(notif.Body, "无需进一步操作") {
			t.Errorf("notification[%d] body missing '无需进一步操作', got %q", i, notif.Body)
		}
	}
	hubNotifier.mu.Unlock()
}

// =============================================================================
// Integration Test: Withdrawal Rejections
// =============================================================================

func TestIntegration_Withdrawal_Rejections(t *testing.T) {
	t.Run("NonInitiator_ErrNotInitiator", func(t *testing.T) {
		instStore := newIntegMockInstanceStore()
		instStore.instances["inst-200"] = &WorkflowInstance{
			ID:     "inst-200",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id":   "user-alice",
				"workflow_name":  "报销审批",
				"initiator_name": "Alice",
			},
		}

		handler := NewWithdrawalHandler(instStore, nil, nil, nil)
		err := handler.Withdraw(context.Background(), "inst-200", "user-bob")
		if !errors.Is(err, ErrNotInitiator) {
			t.Errorf("Withdraw() error = %v, want ErrNotInitiator", err)
		}

		// Verify instance status unchanged.
		if instStore.instances["inst-200"].Status != InstanceRunning {
			t.Error("instance status should remain 'running' after rejected withdrawal")
		}
	})

	t.Run("AlreadyCompleted_ErrAlreadyCompleted", func(t *testing.T) {
		instStore := newIntegMockInstanceStore()
		instStore.instances["inst-201"] = &WorkflowInstance{
			ID:     "inst-201",
			Status: InstanceCompleted,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		}

		handler := NewWithdrawalHandler(instStore, nil, nil, nil)
		err := handler.Withdraw(context.Background(), "inst-201", "user-alice")
		if !errors.Is(err, ErrAlreadyCompleted) {
			t.Errorf("Withdraw() error = %v, want ErrAlreadyCompleted", err)
		}
	})

	t.Run("AlreadyWithdrawn_ErrAlreadyWithdrawn", func(t *testing.T) {
		instStore := newIntegMockInstanceStore()
		instStore.instances["inst-202"] = &WorkflowInstance{
			ID:     "inst-202",
			Status: InstanceWithdrawn,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		}

		handler := NewWithdrawalHandler(instStore, nil, nil, nil)
		err := handler.Withdraw(context.Background(), "inst-202", "user-alice")
		if !errors.Is(err, ErrAlreadyWithdrawn) {
			t.Errorf("Withdraw() error = %v, want ErrAlreadyWithdrawn", err)
		}
	})
}

// =============================================================================
// Integration Test: Withdrawal Edge Cases
// =============================================================================

func TestIntegration_Withdrawal_EdgeCases(t *testing.T) {
	t.Run("NoPendingParticipants_SucceedsNoNotifications", func(t *testing.T) {
		// Instance with no pending_participants and no approver_ids.
		instStore := newIntegMockInstanceStore()
		instStore.instances["inst-300"] = &WorkflowInstance{
			ID:     "inst-300",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id":   "user-alice",
				"workflow_name":  "采购审批",
				"initiator_name": "Alice",
				// No pending_participants or approver_ids.
			},
		}
		instStore.pendingExecs = []NodeExecution{
			{ID: "exec-x", InstanceID: "inst-300", NodeID: "node-1", Status: NodePending},
		}

		auditStore := &integMockAuditStore{}
		hubNotifier := newIntegMockHubNotifier()
		imPusher := newIntegMockIMPusher()
		notifStore := &integMockNotifStore{}
		dispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)
		handler := NewWithdrawalHandler(instStore, auditStore, dispatcher, nil)

		err := handler.Withdraw(context.Background(), "inst-300", "user-alice")
		if err != nil {
			t.Fatalf("Withdraw() unexpected error: %v", err)
		}

		// Verify withdrawal succeeded.
		if instStore.instances["inst-300"].Status != InstanceWithdrawn {
			t.Error("instance status should be 'withdrawn'")
		}

		// Verify pending nodes were skipped.
		if len(instStore.updatedNodes) != 1 {
			t.Errorf("expected 1 node update, got %d", len(instStore.updatedNodes))
		}

		// Verify no notifications were sent (no participants).
		hubNotifier.mu.Lock()
		hubCount := len(hubNotifier.sentTo)
		hubNotifier.mu.Unlock()
		if hubCount != 0 {
			t.Errorf("expected 0 hub notifications, got %d", hubCount)
		}

		// Verify audit trail still has withdrawal event.
		var found bool
		for _, entry := range auditStore.entries {
			if entry.EventType == "withdrawal" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'withdrawal' audit entry even with no participants")
		}
	})

	t.Run("NotificationDispatchFailure_WithdrawalStillSucceeds", func(t *testing.T) {
		// Notifications are non-fatal: even if dispatch fails, withdrawal succeeds.
		instStore := newIntegMockInstanceStore()
		instStore.instances["inst-301"] = &WorkflowInstance{
			ID:     "inst-301",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id":        "user-alice",
				"workflow_name":       "出差审批",
				"initiator_name":      "Alice",
				"pending_participants": []interface{}{"user-bob"},
			},
		}
		instStore.pendingExecs = []NodeExecution{
			{ID: "exec-y", InstanceID: "inst-301", NodeID: "node-2", Status: NodePending},
		}

		auditStore := &integMockAuditStore{}

		// Both notification channels fail.
		hubNotifier := newIntegMockHubNotifier()
		hubNotifier.failAll = true
		imPusher := newIntegMockIMPusher()
		imPusher.failAll = true
		imPusher.connected["user-bob"] = true
		notifStore := &integMockNotifStore{}

		dispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)
		handler := NewWithdrawalHandler(instStore, auditStore, dispatcher, nil)

		err := handler.Withdraw(context.Background(), "inst-301", "user-alice")
		// Withdrawal should still succeed even though notifications failed.
		// The Withdraw method ignores DispatchBatch errors.
		if err != nil {
			t.Fatalf("Withdraw() unexpected error: %v", err)
		}

		// Verify instance was withdrawn.
		if instStore.instances["inst-301"].Status != InstanceWithdrawn {
			t.Error("instance status should be 'withdrawn' despite notification failure")
		}

		// Verify pending nodes were still skipped.
		if len(instStore.updatedNodes) != 1 {
			t.Errorf("expected 1 node update, got %d", len(instStore.updatedNodes))
		}
		if instStore.updatedNodes[0].Status != NodeSkipped {
			t.Errorf("node status = %q, want %q", instStore.updatedNodes[0].Status, NodeSkipped)
		}

		// Verify audit trail has withdrawal event (audit is independent of notifications).
		var found bool
		for _, entry := range auditStore.entries {
			if entry.EventType == "withdrawal" && entry.InstanceID == "inst-301" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'withdrawal' audit entry despite notification failure")
		}
	})
}
