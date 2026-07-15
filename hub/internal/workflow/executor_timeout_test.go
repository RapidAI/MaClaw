package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- Mock stores for timeout/fallback tests ---

type mockWorkflowStoreWithVersion struct {
	version *WorkflowVersion
	err     error
}

func (m *mockWorkflowStoreWithVersion) CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	return nil
}
func (m *mockWorkflowStoreWithVersion) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return nil, nil
}
func (m *mockWorkflowStoreWithVersion) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	return nil, nil
}
func (m *mockWorkflowStoreWithVersion) CreateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *mockWorkflowStoreWithVersion) UpdateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *mockWorkflowStoreWithVersion) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	return m.version, m.err
}
func (m *mockWorkflowStoreWithVersion) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	return m.version, m.err
}
func (m *mockWorkflowStoreWithVersion) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	return nil
}
func (m *mockWorkflowStoreWithVersion) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	return nil, nil
}
func (m *mockWorkflowStoreWithVersion) ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	return nil, 0, nil
}

type mockInstanceStoreForTimeout struct {
	instance      *WorkflowInstance
	updatedStatus InstanceStatus
	nodeExecs     []NodeExecution
	nodeUpdates   []nodeExecUpdate
}

type nodeExecUpdate struct {
	ID         string
	Status     NodeStatus
	FailReason string
}

func (m *mockInstanceStoreForTimeout) Create(ctx context.Context, inst *WorkflowInstance) error {
	m.instance = inst
	return nil
}
func (m *mockInstanceStoreForTimeout) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	if m.instance != nil && m.instance.ID == id {
		return m.instance, nil
	}
	return nil, nil
}
func (m *mockInstanceStoreForTimeout) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	m.updatedStatus = status
	return nil
}
func (m *mockInstanceStoreForTimeout) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	return nil
}
func (m *mockInstanceStoreForTimeout) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	if m.instance != nil && m.instance.ID == id {
		m.instance.InstanceData = data
	}
	return nil
}
func (m *mockInstanceStoreForTimeout) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	m.nodeExecs = append(m.nodeExecs, *exec)
	return nil
}
func (m *mockInstanceStoreForTimeout) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	m.nodeUpdates = append(m.nodeUpdates, nodeExecUpdate{ID: id, Status: status, FailReason: failReason})
	return nil
}
func (m *mockInstanceStoreForTimeout) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	return m.nodeExecs, nil
}
func (m *mockInstanceStoreForTimeout) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStoreForTimeout) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStoreForTimeout) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStoreForTimeout) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

type mockDispatcherForTimeout struct {
	dispatched         []dispatchRecord
	fallbackDispatched []fallbackRecord
	fallbackErr        error
}

type dispatchRecord struct {
	ApproverID string
	RequestID  string
}

type fallbackRecord struct {
	FallbackID string
	Reason     string
	RequestID  string
}

func (m *mockDispatcherForTimeout) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	m.dispatched = append(m.dispatched, dispatchRecord{ApproverID: approverID, RequestID: req.ID})
	return nil
}
func (m *mockDispatcherForTimeout) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	m.fallbackDispatched = append(m.fallbackDispatched, fallbackRecord{FallbackID: fallbackID, Reason: reason, RequestID: req.ID})
	return m.fallbackErr
}

type mockNotifier struct {
	notifications []notifyRecord
}

type notifyRecord struct {
	InstanceID string
	Reason     string
	Details    string
}

func (m *mockNotifier) NotifyInitiator(ctx context.Context, instanceID string, reason string, details string) error {
	m.notifications = append(m.notifications, notifyRecord{InstanceID: instanceID, Reason: reason, Details: details})
	return nil
}

// --- Helper to build test graph with an approval node ---

func buildTimeoutTestGraph(fallbackApprover string) WorkflowGraph {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:      []string{"ve-primary"},
		Mode:             ModeSingle,
		TimeoutHours:     24,
		FallbackApprover: fallbackApprover,
	})
	return WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Manager Approval", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"},
		},
	}
}

// --- Tests ---

func TestHandleTimeout_WithFallback_DispatchesToFallback(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Purchase Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	notifier := &mockNotifier{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

	err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1")
	if err != nil {
		t.Fatalf("HandleTimeout returned error: %v", err)
	}

	// Verify fallback was dispatched.
	if len(dispatcher.fallbackDispatched) != 1 {
		t.Fatalf("expected 1 fallback dispatch, got %d", len(dispatcher.fallbackDispatched))
	}
	fb := dispatcher.fallbackDispatched[0]
	if fb.FallbackID != "ve-fallback" {
		t.Errorf("expected fallback to ve-fallback, got %s", fb.FallbackID)
	}
	if fb.Reason != "timeout" {
		t.Errorf("expected reason 'timeout', got %s", fb.Reason)
	}

	// Verify audit trail has timeout + fallback_routed events.
	if len(auditStore.entries) < 2 {
		t.Fatalf("expected at least 2 audit entries, got %d", len(auditStore.entries))
	}
	if auditStore.entries[0].EventType != "node_timeout" {
		t.Errorf("expected first audit entry to be node_timeout, got %s", auditStore.entries[0].EventType)
	}
	if auditStore.entries[1].EventType != "fallback_routed" {
		t.Errorf("expected second audit entry to be fallback_routed, got %s", auditStore.entries[1].EventType)
	}

	// Verify no notification was sent (fallback succeeded).
	if len(notifier.notifications) != 0 {
		t.Errorf("expected no notifications, got %d", len(notifier.notifications))
	}
}

func TestHandleTimeout_FallbackIsPersistedAndNotRedispatched(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{ID: "inst-1", VersionID: "ver-1", Status: InstanceRunning, InstanceData: map[string]interface{}{}},
		nodeExecs: []NodeExecution{{
			ID: "exec-1", InstanceID: "inst-1", NodeID: "approval-1", NodeType: NodeApproval,
			Status: NodeRunning, StartedAt: time.Now().UTC().Add(-25 * time.Hour), Result: json.RawMessage(`{"timeout_hours":24}`),
		}},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	if err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1"); err != nil {
		t.Fatalf("first HandleTimeout: %v", err)
	}
	if len(dispatcher.fallbackDispatched) != 1 {
		t.Fatalf("first timeout dispatched %d fallbacks, want 1", len(dispatcher.fallbackDispatched))
	}
	if len(instStore.nodeUpdates) != 1 || instStore.nodeUpdates[0].Status != NodeRunning {
		t.Fatalf("fallback state was not persisted: %+v", instStore.nodeUpdates)
	}
	instStore.nodeExecs[0].Result = json.RawMessage(`{"timeout_hours":24,"fallback_active":true,"fallback_approver":"ve-fallback","approver_ids":["ve-fallback"]}`)

	if err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1"); err != nil {
		t.Fatalf("second HandleTimeout: %v", err)
	}
	if len(dispatcher.fallbackDispatched) != 1 {
		t.Fatalf("fallback was redispatched: %+v", dispatcher.fallbackDispatched)
	}
	if instStore.updatedStatus != InstanceBlocked {
		t.Fatalf("fallback timeout status = %q, want %q", instStore.updatedStatus, InstanceBlocked)
	}
}

func TestHandleTimeout_NoFallback_MarksBlocked(t *testing.T) {
	graph := buildTimeoutTestGraph("") // no fallback
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Purchase Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	notifier := &mockNotifier{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

	err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1")
	if err != nil {
		t.Fatalf("HandleTimeout returned error: %v", err)
	}

	// Verify instance was marked as blocked.
	if instStore.updatedStatus != InstanceBlocked {
		t.Errorf("expected instance status to be blocked, got %s", instStore.updatedStatus)
	}

	// Verify audit trail has timeout + node_blocked events.
	hasTimeout := false
	hasBlocked := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "node_timeout" {
			hasTimeout = true
		}
		if entry.EventType == "node_blocked" {
			hasBlocked = true
		}
	}
	if !hasTimeout {
		t.Error("expected node_timeout audit entry")
	}
	if !hasBlocked {
		t.Error("expected node_blocked audit entry")
	}

	// Verify initiator was notified.
	if len(notifier.notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.notifications))
	}
	if notifier.notifications[0].InstanceID != "inst-1" {
		t.Errorf("expected notification for inst-1, got %s", notifier.notifications[0].InstanceID)
	}
	if !strings.Contains(notifier.notifications[0].Reason, "blocked") {
		t.Errorf("expected notification reason to contain 'blocked', got %s", notifier.notifications[0].Reason)
	}
}

func TestHandleTimeout_CascadingFailure_FallbackAlsoUnavailable(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Purchase Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{
		fallbackErr: errors.New("fallback VE queue full"),
	}
	notifier := &mockNotifier{}

	// No EscalationManager → immediate block (legacy / soft-fail path).
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

	err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1")
	if err != nil {
		t.Fatalf("HandleTimeout returned error: %v", err)
	}

	// Verify instance was marked as blocked (cascading failure).
	if instStore.updatedStatus != InstanceBlocked {
		t.Errorf("expected instance status to be blocked, got %s", instStore.updatedStatus)
	}

	// Verify audit trail has timeout + fallback_failed + node_blocked events.
	hasFallbackFailed := false
	hasBlocked := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "fallback_failed" {
			hasFallbackFailed = true
			if !strings.Contains(entry.Details, "cascading_failure") {
				t.Errorf("expected fallback_failed details to contain 'cascading_failure', got %s", entry.Details)
			}
		}
		if entry.EventType == "node_blocked" {
			hasBlocked = true
		}
	}
	if !hasFallbackFailed {
		t.Error("expected fallback_failed audit entry")
	}
	if !hasBlocked {
		t.Error("expected node_blocked audit entry")
	}

	// Verify initiator was notified about cascading failure.
	if len(notifier.notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.notifications))
	}
	if !strings.Contains(notifier.notifications[0].Reason, "blocked") {
		t.Errorf("expected notification reason to contain 'blocked', got %s", notifier.notifications[0].Reason)
	}
}

func TestHandleTimeout_CascadingFailure_QueuesEscalationWhenManagerWired(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Purchase Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{
		fallbackErr: errors.New("fallback offline"),
	}
	notifier := &mockNotifier{}
	// Checker reports unavailable so Escalate queues rather than immediate re-dispatch success.
	checker := &mockHumanChecker{available: false}
	escMgr := NewEscalationManager(dispatcher, auditStore, checker)
	escMgr.maxRetries = 2
	escMgr.retryInterval = time.Millisecond

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher,
		WithNotifier(notifier),
		WithEscalationManager(escMgr),
	)

	if err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1"); err != nil {
		t.Fatalf("HandleTimeout: %v", err)
	}

	// Must not block immediately — EscalationManager owns retries.
	if instStore.updatedStatus == InstanceBlocked {
		t.Fatalf("expected no immediate block while escalation pending, got %s", instStore.updatedStatus)
	}
	if !escMgr.HasPendingForInstance("inst-1", "approval-1") {
		t.Fatal("expected pending escalation for inst-1/approval-1")
	}
	if pending, _ := instStore.instance.InstanceData["escalation_pending"].(bool); !pending {
		t.Fatalf("expected escalation_pending on instance data, got %#v", instStore.instance.InstanceData)
	}
	if len(notifier.notifications) < 1 || !strings.Contains(notifier.notifications[0].Reason, "escalation pending") {
		t.Fatalf("expected pending escalation notify, got %#v", notifier.notifications)
	}
	// Reason wording is shared for primary and fallback ("approver unavailable").

	// Subsequent timeout while escalation is pending must not short-circuit to block.
	if err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1"); err != nil {
		t.Fatalf("second HandleTimeout: %v", err)
	}
	if instStore.updatedStatus == InstanceBlocked {
		t.Fatal("timeout must not block while escalation is still pending")
	}

	// Exhaust retries → failed hook marks blocked.
	for i := 0; i < 3; i++ {
		escMgr.mu.Lock()
		for _, req := range escMgr.queue {
			req.LastAttemptAt = time.Now().Add(-time.Minute)
		}
		escMgr.mu.Unlock()
		escMgr.processPendingEscalations()
	}
	if instStore.updatedStatus != InstanceBlocked {
		t.Fatalf("after max retries expected blocked, got %s", instStore.updatedStatus)
	}
	if escMgr.HasPendingForInstance("inst-1", "approval-1") {
		t.Fatal("escalation queue should be empty after failure")
	}
}

func TestHandleUnavailable_WithFallback_DispatchesToFallback(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Leave Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	notifier := &mockNotifier{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

	err := executor.HandleUnavailable(context.Background(), "inst-1", "approval-1", "ve-primary")
	if err != nil {
		t.Fatalf("HandleUnavailable returned error: %v", err)
	}

	// Verify fallback was dispatched.
	if len(dispatcher.fallbackDispatched) != 1 {
		t.Fatalf("expected 1 fallback dispatch, got %d", len(dispatcher.fallbackDispatched))
	}
	if dispatcher.fallbackDispatched[0].FallbackID != "ve-fallback" {
		t.Errorf("expected fallback to ve-fallback, got %s", dispatcher.fallbackDispatched[0].FallbackID)
	}

	// Verify audit trail has unavailable + fallback_routed events.
	hasUnavailable := false
	hasFallbackRouted := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "approver_unavailable" {
			hasUnavailable = true
			if entry.ActorID != "ve-primary" {
				t.Errorf("expected actor_id ve-primary, got %s", entry.ActorID)
			}
		}
		if entry.EventType == "fallback_routed" {
			hasFallbackRouted = true
		}
	}
	if !hasUnavailable {
		t.Error("expected approver_unavailable audit entry")
	}
	if !hasFallbackRouted {
		t.Error("expected fallback_routed audit entry")
	}
}

func TestHandleQueueFull_WithFallback_DispatchesToFallback(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Expense Report"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	notifier := &mockNotifier{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

	err := executor.HandleQueueFull(context.Background(), "inst-1", "approval-1", "ve-primary")
	if err != nil {
		t.Fatalf("HandleQueueFull returned error: %v", err)
	}

	// Verify fallback was dispatched.
	if len(dispatcher.fallbackDispatched) != 1 {
		t.Fatalf("expected 1 fallback dispatch, got %d", len(dispatcher.fallbackDispatched))
	}
	if dispatcher.fallbackDispatched[0].Reason != "queue_full" {
		t.Errorf("expected reason 'queue_full', got %s", dispatcher.fallbackDispatched[0].Reason)
	}

	// Verify audit trail has queue_full + fallback_routed events.
	hasQueueFull := false
	hasFallbackRouted := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "approver_queue_full" {
			hasQueueFull = true
		}
		if entry.EventType == "fallback_routed" {
			hasFallbackRouted = true
		}
	}
	if !hasQueueFull {
		t.Error("expected approver_queue_full audit entry")
	}
	if !hasFallbackRouted {
		t.Error("expected fallback_routed audit entry")
	}
}

func TestHandleTimeout_InstanceNotFound(t *testing.T) {
	wfStore := &mockWorkflowStoreWithVersion{}
	instStore := &mockInstanceStoreForTimeout{instance: nil}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	err := executor.HandleTimeout(context.Background(), "nonexistent", "node-1")
	if err == nil {
		t.Fatal("expected error for nonexistent instance")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", err.Error())
	}
}

func TestHandleTimeout_FallbackReceivesSamePayload(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:        "inst-1",
			VersionID: "ver-1",
			Status:    InstanceRunning,
			InstanceData: map[string]interface{}{
				"title":        "Important Purchase",
				"summary":      "Need to buy equipment",
				"requester_id": "user-123",
				"hint_rules":   []interface{}{"Auto-approve if amount < 1000"},
			},
		},
	}
	auditStore := &mockAuditStore{}

	// Custom dispatcher that captures the request payload.
	var capturedReq *ApprovalRequest
	dispatcher := &mockDispatcherForTimeout{}
	dispatcher.fallbackErr = nil

	// Override DispatchFallback to capture the request.
	type capturingDispatcher struct {
		*mockDispatcherForTimeout
		captured *ApprovalRequest
	}
	cd := &capturingDispatcher{mockDispatcherForTimeout: dispatcher}

	// Use a wrapper dispatcher that captures the request.
	wrapDispatcher := &fallbackCapturingDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, wrapDispatcher)

	err := executor.HandleTimeout(context.Background(), "inst-1", "approval-1")
	if err != nil {
		t.Fatalf("HandleTimeout returned error: %v", err)
	}

	capturedReq = wrapDispatcher.lastFallbackReq
	if capturedReq == nil {
		t.Fatal("expected fallback request to be captured")
	}

	// Verify the fallback approver receives the same payload.
	if capturedReq.Title != "Important Purchase" {
		t.Errorf("expected title 'Important Purchase', got %s", capturedReq.Title)
	}
	if capturedReq.Summary != "Need to buy equipment" {
		t.Errorf("expected summary 'Need to buy equipment', got %s", capturedReq.Summary)
	}
	if capturedReq.RequesterID != "user-123" {
		t.Errorf("expected requester_id 'user-123', got %s", capturedReq.RequesterID)
	}
	if len(capturedReq.HintRules) != 1 || capturedReq.HintRules[0] != "Auto-approve if amount < 1000" {
		t.Errorf("expected hint_rules to be preserved, got %v", capturedReq.HintRules)
	}

	_ = cd // suppress unused
}

// fallbackCapturingDispatcher captures the request sent to fallback.
type fallbackCapturingDispatcher struct {
	lastFallbackReq *ApprovalRequest
}

func (d *fallbackCapturingDispatcher) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	return nil
}
func (d *fallbackCapturingDispatcher) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	d.lastFallbackReq = req
	return nil
}

func TestHandleTimeout_AuditTrailRecordsFallbackDetails(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID:           "inst-1",
			VersionID:    "ver-1",
			Status:       InstanceRunning,
			InstanceData: map[string]interface{}{},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_ = executor.HandleTimeout(context.Background(), "inst-1", "approval-1")

	// Find the fallback_routed entry and verify its details.
	for _, entry := range auditStore.entries {
		if entry.EventType == "fallback_routed" {
			if !strings.Contains(entry.Details, "timeout") {
				t.Errorf("expected fallback_routed details to contain 'timeout', got %s", entry.Details)
			}
			if !strings.Contains(entry.Details, "ve-fallback") {
				t.Errorf("expected fallback_routed details to contain 've-fallback', got %s", entry.Details)
			}
			if !strings.Contains(entry.Details, "ve-primary") {
				t.Errorf("expected fallback_routed details to contain original approver 've-primary', got %s", entry.Details)
			}
			if entry.ActorID != "ve-fallback" {
				t.Errorf("expected actor_id to be 've-fallback', got %s", entry.ActorID)
			}
			return
		}
	}
	t.Error("fallback_routed audit entry not found")
}
