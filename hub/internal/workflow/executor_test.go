package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// --- Mock stores for testing ---

type mockWorkflowStore struct {
	publishedVersion *WorkflowVersion
	publishedErr     error
}

func (m *mockWorkflowStore) CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	return nil
}
func (m *mockWorkflowStore) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return nil, nil
}
func (m *mockWorkflowStore) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	return nil, nil
}
func (m *mockWorkflowStore) CreateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *mockWorkflowStore) UpdateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *mockWorkflowStore) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	return nil, nil
}
func (m *mockWorkflowStore) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	return m.publishedVersion, m.publishedErr
}
func (m *mockWorkflowStore) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	return nil
}
func (m *mockWorkflowStore) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	return nil, nil
}
func (m *mockWorkflowStore) ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	return nil, 0, nil
}

type mockInstanceStore struct {
	createdInstance *WorkflowInstance
	createdExecs   []*NodeExecution
	createErr      error
}

func (m *mockInstanceStore) Create(ctx context.Context, inst *WorkflowInstance) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.createdInstance = inst
	return nil
}
func (m *mockInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	return m.createdInstance, nil
}
func (m *mockInstanceStore) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	return nil
}
func (m *mockInstanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	return nil
}
func (m *mockInstanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	return nil
}
func (m *mockInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	m.createdExecs = append(m.createdExecs, exec)
	return nil
}
func (m *mockInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	return nil
}
func (m *mockInstanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	return nil, nil
}
func (m *mockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

type mockAuditStore struct {
	entries []*AuditEntry
}

func (m *mockAuditStore) Append(ctx context.Context, entry *AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}
func (m *mockAuditStore) QueryByInstance(ctx context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *mockAuditStore) QueryByApprover(ctx context.Context, approverID string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *mockAuditStore) QueryByTimeRange(ctx context.Context, start, end time.Time, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *mockAuditStore) QueryByDecision(ctx context.Context, decision string, page, pageSize int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

type mockDispatcher struct{}

func (m *mockDispatcher) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	return nil
}
func (m *mockDispatcher) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	return nil
}

// --- Tests ---

func TestStartInstance_Success(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-approver-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Review", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"},
		},
	}

	wfStore := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:            "ver-123",
			WorkflowID:    "wf-001",
			VersionNumber: "1.0.0",
			Status:        VersionPublished,
			Graph:         graph,
		},
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	instance, err := executor.StartInstance(context.Background(), "wf-001", `{"event":"test"}`)
	if err != nil {
		t.Fatalf("StartInstance returned error: %v", err)
	}

	// Verify instance was created correctly.
	if instance == nil {
		t.Fatal("StartInstance returned nil instance")
	}
	if instance.WorkflowID != "wf-001" {
		t.Errorf("WorkflowID = %q, want %q", instance.WorkflowID, "wf-001")
	}
	if instance.VersionID != "ver-123" {
		t.Errorf("VersionID = %q, want %q", instance.VersionID, "ver-123")
	}
	if instance.Status != InstanceRunning {
		t.Errorf("Status = %q, want %q", instance.Status, InstanceRunning)
	}
	if instance.CurrentNodeID != "approval-1" {
		t.Errorf("CurrentNodeID = %q, want %q (approval node is now current, waiting for response)", instance.CurrentNodeID, "approval-1")
	}
	if instance.TriggerData != `{"event":"test"}` {
		t.Errorf("TriggerData = %q, want %q", instance.TriggerData, `{"event":"test"}`)
	}
	if instance.ID == "" {
		t.Error("Instance ID should not be empty")
	}
	if instance.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// Verify instance was persisted.
	if instStore.createdInstance == nil {
		t.Fatal("Instance was not persisted to store")
	}

	// Verify audit entries were recorded.
	// Expected: instance_created + node_completed (trigger)
	// The approval node does NOT get node_completed (it's blocking, waiting for response).
	if len(auditStore.entries) < 1 {
		t.Fatalf("Expected at least 1 audit entry, got %d", len(auditStore.entries))
	}
	entry := auditStore.entries[0]
	if entry.InstanceID != instance.ID {
		t.Errorf("Audit entry InstanceID = %q, want %q", entry.InstanceID, instance.ID)
	}
	if entry.EventType != "instance_created" {
		t.Errorf("Audit entry EventType = %q, want %q", entry.EventType, "instance_created")
	}

	// Verify node executions were created (trigger + approval).
	if len(instStore.createdExecs) < 1 {
		t.Fatalf("Expected at least 1 node execution, got %d", len(instStore.createdExecs))
	}
	exec := instStore.createdExecs[0]
	if exec.NodeID != "trigger-1" {
		t.Errorf("First NodeExecution NodeID = %q, want %q", exec.NodeID, "trigger-1")
	}
	if exec.Status != NodeRunning {
		t.Errorf("NodeExecution Status = %q, want %q", exec.Status, NodeRunning)
	}
	if exec.InstanceID != instance.ID {
		t.Errorf("NodeExecution InstanceID = %q, want %q", exec.InstanceID, instance.ID)
	}
}

func TestStartInstance_NoPublishedVersion(t *testing.T) {
	wfStore := &mockWorkflowStore{publishedVersion: nil}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-001", "")
	if !errors.Is(err, ErrNoPublishedVersion) {
		t.Errorf("Expected ErrNoPublishedVersion, got: %v", err)
	}
}

func TestStartInstance_NoTriggerNode(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "approval-1", Type: NodeApproval, Label: "Review"},
		},
	}

	wfStore := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:         "ver-123",
			WorkflowID: "wf-001",
			Status:     VersionPublished,
			Graph:      graph,
		},
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-001", "")
	if !errors.Is(err, ErrNoTriggerNode) {
		t.Errorf("Expected ErrNoTriggerNode, got: %v", err)
	}
}

func TestStartInstance_MultipleTriggerNodes(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start 1"},
			{ID: "trigger-2", Type: NodeTrigger, Label: "Start 2"},
		},
	}

	wfStore := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:         "ver-123",
			WorkflowID: "wf-001",
			Status:     VersionPublished,
			Graph:      graph,
		},
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-001", "")
	if !errors.Is(err, ErrMultipleTriggers) {
		t.Errorf("Expected ErrMultipleTriggers, got: %v", err)
	}
}

func TestStartInstance_StoreError(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
		},
	}

	wfStore := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:         "ver-123",
			WorkflowID: "wf-001",
			Status:     VersionPublished,
			Graph:      graph,
		},
	}
	instStore := &mockInstanceStore{createErr: errors.New("database connection failed")}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-001", "")
	if err == nil {
		t.Fatal("Expected error from instance store, got nil")
	}
	if !errors.Is(err, instStore.createErr) {
		t.Errorf("Expected wrapped store error, got: %v", err)
	}
}

func TestStartInstance_GetPublishedVersionError(t *testing.T) {
	wfStore := &mockWorkflowStore{publishedErr: errors.New("network timeout")}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-001", "")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}
