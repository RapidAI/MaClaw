package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// In-memory mock stores for runtime integration testing.
// Prefixed with "rt" to avoid conflicts with other test files in this package.
// =============================================================================

// rtInstanceStore is an in-memory InstanceStore for runtime integration tests.
type rtInstanceStore struct {
	mu        sync.Mutex
	instances map[string]*WorkflowInstance
	nodeExecs []*NodeExecution
}

func newRTInstanceStore() *rtInstanceStore {
	return &rtInstanceStore{instances: make(map[string]*WorkflowInstance)}
}

func (s *rtInstanceStore) Create(_ context.Context, inst *WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.ID] = inst
	return nil
}

func (s *rtInstanceStore) Get(_ context.Context, id string) (*WorkflowInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instances[id], nil
}

func (s *rtInstanceStore) UpdateStatus(_ context.Context, id string, status InstanceStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		inst.Status = status
	}
	return nil
}

func (s *rtInstanceStore) UpdateCurrentNode(_ context.Context, id, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		inst.CurrentNodeID = nodeID
	}
	return nil
}

func (s *rtInstanceStore) UpdateInstanceData(_ context.Context, id string, data map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		inst.InstanceData = data
	}
	return nil
}

func (s *rtInstanceStore) CreateNodeExecution(_ context.Context, exec *NodeExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeExecs = append(s.nodeExecs, exec)
	return nil
}

func (s *rtInstanceStore) UpdateNodeExecution(_ context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, exec := range s.nodeExecs {
		if exec.ID == id {
			exec.Status = status
			exec.Result = result
			exec.FailReason = failReason
			if status == NodeCompleted || status == NodeFailed || status == NodeSkipped {
				now := time.Now().UTC()
				exec.CompletedAt = &now
			}
		}
	}
	return nil
}

func (s *rtInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return nil, nil
}

func (s *rtInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *rtInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *rtInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *rtInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// rtAuditStore is an in-memory AuditStore for runtime integration tests.
type rtAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

func newRTAuditStore() *rtAuditStore { return &rtAuditStore{} }

func (s *rtAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC().Truncate(time.Millisecond)
	}
	s.entries = append(s.entries, entry)
	return nil
}

func (s *rtAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *rtAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *rtAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *rtAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

// rtConfirmationStore is an in-memory ConfirmationStore for runtime integration tests.
type rtConfirmationStore struct {
	mu      sync.Mutex
	records []*Confirmation
}

func newRTConfirmationStore() *rtConfirmationStore { return &rtConfirmationStore{} }

func (s *rtConfirmationStore) Create(_ context.Context, conf *Confirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conf.ID == "" {
		conf.ID = generateConfirmationID()
	}
	if conf.CreatedAt.IsZero() {
		conf.CreatedAt = time.Now().UTC()
	}
	s.records = append(s.records, conf)
	return nil
}

func (s *rtConfirmationStore) Get(_ context.Context, id string) (*Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.records {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

func (s *rtConfirmationStore) UpdateStatus(_ context.Context, id string, status ConfirmationStatus, notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.records {
		if c.ID == id {
			c.Status = status
			c.Notes = notes
			return nil
		}
	}
	return nil
}

func (s *rtConfirmationStore) IncrementReminders(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.records {
		if c.ID == id {
			c.RemindersSent++
			now := time.Now().UTC()
			c.LastReminderAt = &now
			return nil
		}
	}
	return nil
}

func (s *rtConfirmationStore) ListPending(_ context.Context, recipientID string) ([]Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Confirmation
	for _, c := range s.records {
		if c.RecipientID == recipientID && c.Status == ConfirmPending {
			result = append(result, *c)
		}
	}
	return result, nil
}

func (s *rtConfirmationStore) ListByInstance(_ context.Context, instanceID string) ([]Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Confirmation
	for _, c := range s.records {
		if c.InstanceID == instanceID {
			result = append(result, *c)
		}
	}
	return result, nil
}

func (s *rtConfirmationStore) FindOverdue(_ context.Context) ([]Confirmation, error) {
	return nil, nil
}

// rtNotificationStore is an in-memory NotificationStore for runtime integration tests.
type rtNotificationStore struct {
	mu      sync.Mutex
	records []*Notification
}

func newRTNotificationStore() *rtNotificationStore { return &rtNotificationStore{} }

func (s *rtNotificationStore) Create(_ context.Context, notif *Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if notif.ID == "" {
		notif.ID = GenerateNotificationID()
	}
	if notif.CreatedAt.IsZero() {
		notif.CreatedAt = time.Now().UTC()
	}
	s.records = append(s.records, notif)
	return nil
}

func (s *rtNotificationStore) Get(_ context.Context, id string) (*Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.records {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, nil
}

func (s *rtNotificationStore) ListByInstance(_ context.Context, instanceID string) ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Notification
	for _, n := range s.records {
		if n.InstanceID == instanceID {
			result = append(result, *n)
		}
	}
	return result, nil
}

func (s *rtNotificationStore) ListByRecipient(_ context.Context, recipientID string) ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Notification
	for _, n := range s.records {
		if n.RecipientID == recipientID {
			result = append(result, *n)
		}
	}
	return result, nil
}

func (s *rtNotificationStore) MarkDelivered(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.records {
		if n.ID == id {
			n.Delivered = true
			now := time.Now().UTC()
			n.DeliveredAt = &now
			return nil
		}
	}
	return nil
}

func (s *rtNotificationStore) MarkFailed(_ context.Context, id string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.records {
		if n.ID == id {
			n.FailureReason = reason
			return nil
		}
	}
	return nil
}

// rtHubNotifier is a mock HubInAppNotifier that records sent notifications.
type rtHubNotifier struct {
	mu   sync.Mutex
	sent []rtNotifRecord
}

type rtNotifRecord struct {
	RecipientID string
	Notif       *InAppNotification
}

func (m *rtHubNotifier) Send(_ context.Context, recipientID string, notif *InAppNotification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, rtNotifRecord{RecipientID: recipientID, Notif: notif})
	return nil
}

// rtIMPusher is a mock IMPushNotifier that records push messages.
type rtIMPusher struct {
	mu        sync.Mutex
	pushed    []rtPushRecord
	connected map[string]bool
}

type rtPushRecord struct {
	RecipientID string
	Msg         *IMPushMessage
}

func newRTIMPusher(connectedUsers ...string) *rtIMPusher {
	m := &rtIMPusher{connected: make(map[string]bool)}
	for _, u := range connectedUsers {
		m.connected[u] = true
	}
	return m
}

func (m *rtIMPusher) Push(_ context.Context, recipientID string, msg *IMPushMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushed = append(m.pushed, rtPushRecord{RecipientID: recipientID, Msg: msg})
	return nil
}

func (m *rtIMPusher) IsConnected(_ context.Context, recipientID string) bool {
	return m.connected[recipientID]
}

// rtWorkflowStore implements WorkflowStore, returning the version by ID.
type rtWorkflowStore struct {
	publishedVersion *WorkflowVersion
}

func (m *rtWorkflowStore) CreateWorkflow(_ context.Context, _ *WorkflowDefinition) error { return nil }
func (m *rtWorkflowStore) GetWorkflow(_ context.Context, _ string) (*WorkflowDefinition, error) {
	return nil, nil
}
func (m *rtWorkflowStore) ListWorkflows(_ context.Context, _ string) ([]WorkflowDefinition, error) {
	return nil, nil
}
func (m *rtWorkflowStore) CreateVersion(_ context.Context, _ *WorkflowVersion) error { return nil }
func (m *rtWorkflowStore) GetVersion(_ context.Context, id string) (*WorkflowVersion, error) {
	if m.publishedVersion != nil && m.publishedVersion.ID == id {
		return m.publishedVersion, nil
	}
	return nil, nil
}
func (m *rtWorkflowStore) GetPublishedVersion(_ context.Context, _ string) (*WorkflowVersion, error) {
	return m.publishedVersion, nil
}
func (m *rtWorkflowStore) UpdateVersionStatus(_ context.Context, _ string, _ VersionStatus, _ string) error {
	return nil
}
func (m *rtWorkflowStore) ListVersions(_ context.Context, _ string) ([]WorkflowVersion, error) {
	return nil, nil
}
func (m *rtWorkflowStore) ListPendingReviews(_ context.Context, _, _ int) ([]WorkflowVersion, int, error) {
	return nil, 0, nil
}

// rtApprovalDispatcher is a no-op dispatcher for runtime integration tests.
type rtApprovalDispatcher struct {
	mu       sync.Mutex
	requests []*ApprovalRequest
}

func (m *rtApprovalDispatcher) Dispatch(_ context.Context, req *ApprovalRequest, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return nil
}

func (m *rtApprovalDispatcher) DispatchFallback(_ context.Context, req *ApprovalRequest, _ string, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return nil
}

// =============================================================================
// Integration Test: Hub Page Initiation Full Flow
// =============================================================================

// TestIntegration_HubPageInitiation_FullFlow exercises the complete happy-path flow:
// Form submission → Validation → Instance creation → Node execution →
// Terminal node → Notification dispatch → Confirmation tracking.
//
// Validates: Requirements 1.1, 1.2, 1.4, 1.5, 1.6, 4.1, 4.2, 5.1
func TestIntegration_HubPageInitiation_FullFlow(t *testing.T) {
	ctx := context.Background()

	// --- Setup mock stores ---
	instanceStore := newRTInstanceStore()
	auditStore := newRTAuditStore()
	confirmStore := newRTConfirmationStore()
	notifStore := newRTNotificationStore()
	hubNotifier := &rtHubNotifier{}
	imPusher := newRTIMPusher("executor-user-1", "notifier-user-1")
	dispatcher := &rtApprovalDispatcher{}

	// --- Build notification dispatcher ---
	notifDispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)

	// --- Build confirmation tracker ---
	confirmTracker := NewConfirmationTracker(confirmStore, instanceStore, notifDispatcher, auditStore)

	// --- Build workflow graph: Trigger → Form → Approval → Terminal ---
	formConfig, _ := json.Marshal(FormNodeConfig{
		Fields: []FormFieldSchema{
			{Name: "leave_type", Label: "请假类型", Type: FieldSelect, Required: true, Options: []string{"annual", "sick", "personal"}},
			{Name: "start_date", Label: "开始日期", Type: FieldDate, Required: true},
			{Name: "reason", Label: "事由", Type: FieldText, Required: false, MaxLength: 500},
		},
	})

	approvalConfig, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"approver-ve-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	terminalConfig, _ := json.Marshal(TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "executor-user-1", TimeoutHours: 48, MaxReminders: 3, ReminderInterval: 24},
		},
		Notifiers: []NotifierConfig{
			{UserID: "notifier-user-1", TimeoutHours: 72, MaxReminders: 2, ReminderInterval: 24},
		},
	})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "form-1", Type: NodeForm, Label: "Leave Form", Config: formConfig},
			{ID: "approval-1", Type: NodeApproval, Label: "Manager Approval", Config: approvalConfig},
			{ID: "terminal-1", Type: NodeTypeTerminal, Label: "End", Config: terminalConfig},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "form-1"},
			{ID: "e2", SourceID: "form-1", TargetID: "approval-1"},
			{ID: "e3", SourceID: "approval-1", TargetID: "terminal-1"},
		},
	}

	// --- Setup workflow store with published version ---
	wfStore := &rtWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:            "ver-integration-001",
			WorkflowID:    "wf-leave-001",
			VersionNumber: "1.0.0",
			Status:        VersionPublished,
			Graph:         graph,
		},
	}

	// --- Create executor with all dependencies ---
	executor := NewWorkflowExecutor(
		wfStore, instanceStore, auditStore, dispatcher,
		WithNotificationDispatcher(notifDispatcher),
		WithConfirmationTracker(confirmTracker),
	)

	// --- Step 1: Validate form data ---
	formData := map[string]interface{}{
		"leave_type": "annual",
		"start_date": "2025-05-01",
		"reason":     "家庭事务",
	}

	validator := &FormValidator{}
	schema, err := ExtractFormSchema(&graph)
	if err != nil {
		t.Fatalf("ExtractFormSchema failed: %v", err)
	}

	t.Run("FormValidation", func(t *testing.T) {
		validationErrors := validator.Validate(formData, schema)
		if len(validationErrors) > 0 {
			t.Fatalf("form validation should pass, got errors: %v", validationErrors)
		}
	})

	// --- Step 2: Start workflow instance (simulates handleInitiateWorkflow) ---
	triggerData, _ := json.Marshal(map[string]interface{}{
		"form_data":    formData,
		"initiator_id": "user-zhang-san",
		"channel":      "hub_page",
		"version_id":   "ver-integration-001",
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
	})

	inst, err := executor.StartInstance(ctx, "wf-leave-001", string(triggerData))
	if err != nil {
		t.Fatalf("StartInstance failed: %v", err)
	}

	t.Run("InstanceCreation", func(t *testing.T) {
		if inst == nil {
			t.Fatal("instance should not be nil")
		}
		if inst.Status != InstanceRunning {
			t.Errorf("expected status 'running', got %q", inst.Status)
		}
		if inst.WorkflowID != "wf-leave-001" {
			t.Errorf("expected workflow_id 'wf-leave-001', got %q", inst.WorkflowID)
		}
		if inst.VersionID != "ver-integration-001" {
			t.Errorf("expected version_id 'ver-integration-001', got %q", inst.VersionID)
		}
	})

	t.Run("FormDataPersistence", func(t *testing.T) {
		var persisted map[string]interface{}
		if err := json.Unmarshal([]byte(inst.TriggerData), &persisted); err != nil {
			t.Fatalf("failed to unmarshal trigger data: %v", err)
		}
		if id, _ := persisted["initiator_id"].(string); id != "user-zhang-san" {
			t.Errorf("expected initiator_id 'user-zhang-san', got %v", persisted["initiator_id"])
		}
		if ch, _ := persisted["channel"].(string); ch != "hub_page" {
			t.Errorf("expected channel 'hub_page', got %v", persisted["channel"])
		}
		if vid, _ := persisted["version_id"].(string); vid != "ver-integration-001" {
			t.Errorf("expected version_id 'ver-integration-001', got %v", persisted["version_id"])
		}
		if _, ok := persisted["timestamp"].(string); !ok {
			t.Error("expected timestamp to be present in persisted data")
		}
		fd, ok := persisted["form_data"].(map[string]interface{})
		if !ok {
			t.Fatal("expected form_data to be a map")
		}
		if fd["leave_type"] != "annual" {
			t.Errorf("expected leave_type 'annual', got %v", fd["leave_type"])
		}
	})

	t.Run("NodeExecutionRecords_BeforeApproval", func(t *testing.T) {
		instanceStore.mu.Lock()
		execCount := len(instanceStore.nodeExecs)
		instanceStore.mu.Unlock()

		// After StartInstance, executor traverses: trigger → form → approval (blocks).
		// Expect node executions for trigger and form at minimum.
		if execCount < 2 {
			t.Errorf("expected at least 2 node executions (trigger + form), got %d", execCount)
		}

		// Verify trigger node execution exists
		found := false
		instanceStore.mu.Lock()
		for _, exec := range instanceStore.nodeExecs {
			if exec.NodeID == "trigger-1" && exec.NodeType == NodeTrigger {
				found = true
				break
			}
		}
		instanceStore.mu.Unlock()
		if !found {
			t.Error("expected trigger node execution record")
		}
	})

	// --- Step 3: Simulate approval (ResumeInstance with approved response) ---
	// Set instance data with form_data and initiator info for terminal node notifications.
	instanceStore.mu.Lock()
	if inst.InstanceData == nil {
		inst.InstanceData = make(map[string]interface{})
	}
	inst.InstanceData["form_data"] = formData
	inst.InstanceData["initiator_id"] = "user-zhang-san"
	inst.InstanceData["initiator_name"] = "张三"
	inst.InstanceData["workflow_name"] = "请假审批"
	inst.InstanceData["result"] = "approved"
	instanceStore.mu.Unlock()

	err = executor.ResumeInstance(ctx, inst.ID, "approval-1", ApprovalResponse{
		Decision:   "approve",
		Rationale:  "符合请假规定",
		ApproverID: "approver-ve-1",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance failed: %v", err)
	}

	t.Run("TerminalNodeReached_InstanceCompleted", func(t *testing.T) {
		instanceStore.mu.Lock()
		finalInst := instanceStore.instances[inst.ID]
		instanceStore.mu.Unlock()

		if finalInst.Status != InstanceCompleted {
			t.Errorf("expected instance status 'completed', got %q", finalInst.Status)
		}
	})

	t.Run("NodeExecutionRecords_AfterApproval", func(t *testing.T) {
		instanceStore.mu.Lock()
		execCount := len(instanceStore.nodeExecs)
		var nodeIDs []string
		for _, exec := range instanceStore.nodeExecs {
			nodeIDs = append(nodeIDs, exec.NodeID)
		}
		instanceStore.mu.Unlock()

		// After approval, executor traverses: approval (completed) → terminal.
		// Total node executions: trigger + form + approval + terminal = 4.
		if execCount < 4 {
			t.Errorf("expected at least 4 node executions, got %d (nodes: %v)", execCount, nodeIDs)
		}

		// Verify terminal node execution exists
		found := false
		instanceStore.mu.Lock()
		for _, exec := range instanceStore.nodeExecs {
			if exec.NodeID == "terminal-1" && exec.NodeType == NodeTypeTerminal {
				found = true
				break
			}
		}
		instanceStore.mu.Unlock()
		if !found {
			t.Error("expected terminal node execution record")
		}
	})

	t.Run("NotificationsSent", func(t *testing.T) {
		// Check Hub in-app notifications were sent
		hubNotifier.mu.Lock()
		hubSentCount := len(hubNotifier.sent)
		hubNotifier.mu.Unlock()
		if hubSentCount < 2 {
			t.Errorf("expected at least 2 Hub in-app notifications (executor + notifier), got %d", hubSentCount)
		}

		// Check IM push notifications were sent (both users are connected)
		imPusher.mu.Lock()
		imPushCount := len(imPusher.pushed)
		imPusher.mu.Unlock()
		if imPushCount < 2 {
			t.Errorf("expected at least 2 IM push notifications (executor + notifier), got %d", imPushCount)
		}

		// Check notification store records
		notifStore.mu.Lock()
		notifRecordCount := len(notifStore.records)
		notifStore.mu.Unlock()
		// Each recipient gets Hub in-app + IM push = 2 records per recipient, 2 recipients = 4
		if notifRecordCount < 4 {
			t.Errorf("expected at least 4 notification records, got %d", notifRecordCount)
		}
	})

	t.Run("ConfirmationTracking", func(t *testing.T) {
		confirmStore.mu.Lock()
		confirmCount := len(confirmStore.records)
		confirmStore.mu.Unlock()

		// 1 executor + 1 notifier = 2 confirmation records
		if confirmCount != 2 {
			t.Errorf("expected 2 confirmation records, got %d", confirmCount)
		}

		confirmStore.mu.Lock()
		var executorConf, notifierConf *Confirmation
		for _, c := range confirmStore.records {
			switch c.Type {
			case ConfirmTypeExecutor:
				executorConf = c
			case ConfirmTypeNotifier:
				notifierConf = c
			}
		}
		confirmStore.mu.Unlock()

		if executorConf == nil {
			t.Fatal("expected executor confirmation record")
		}
		if executorConf.RecipientID != "executor-user-1" {
			t.Errorf("expected executor recipient 'executor-user-1', got %q", executorConf.RecipientID)
		}
		if executorConf.Status != ConfirmPending {
			t.Errorf("expected executor status 'pending', got %q", executorConf.Status)
		}
		if executorConf.TimeoutHours != 48 {
			t.Errorf("expected executor timeout 48h, got %d", executorConf.TimeoutHours)
		}
		if executorConf.MaxReminders != 3 {
			t.Errorf("expected executor max_reminders 3, got %d", executorConf.MaxReminders)
		}

		if notifierConf == nil {
			t.Fatal("expected notifier confirmation record")
		}
		if notifierConf.RecipientID != "notifier-user-1" {
			t.Errorf("expected notifier recipient 'notifier-user-1', got %q", notifierConf.RecipientID)
		}
		if notifierConf.Status != ConfirmPending {
			t.Errorf("expected notifier status 'pending', got %q", notifierConf.Status)
		}
		if notifierConf.TimeoutHours != 72 {
			t.Errorf("expected notifier timeout 72h, got %d", notifierConf.TimeoutHours)
		}
		if notifierConf.MaxReminders != 2 {
			t.Errorf("expected notifier max_reminders 2, got %d", notifierConf.MaxReminders)
		}
	})

	t.Run("AuditTrail", func(t *testing.T) {
		auditStore.mu.Lock()
		entries := make([]*AuditEntry, len(auditStore.entries))
		copy(entries, auditStore.entries)
		auditStore.mu.Unlock()

		if len(entries) == 0 {
			t.Fatal("expected audit trail entries")
		}

		hasCreated := false
		hasCompleted := false
		for _, e := range entries {
			switch e.EventType {
			case "instance_created":
				hasCreated = true
			case "instance_completed":
				hasCompleted = true
			}
		}
		if !hasCreated {
			t.Error("expected 'instance_created' audit event")
		}
		if !hasCompleted {
			t.Error("expected 'instance_completed' audit event")
		}
	})
}
