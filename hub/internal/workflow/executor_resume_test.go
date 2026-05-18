package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- Enhanced mocks for ResumeInstance tests ---

type resumeTestMockWorkflowStore struct {
	version *WorkflowVersion
}

func (m *resumeTestMockWorkflowStore) CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	return nil
}
func (m *resumeTestMockWorkflowStore) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return nil, nil
}
func (m *resumeTestMockWorkflowStore) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	return nil, nil
}
func (m *resumeTestMockWorkflowStore) CreateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *resumeTestMockWorkflowStore) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	return m.version, nil
}
func (m *resumeTestMockWorkflowStore) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	return m.version, nil
}
func (m *resumeTestMockWorkflowStore) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	return nil
}
func (m *resumeTestMockWorkflowStore) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	return nil, nil
}
func (m *resumeTestMockWorkflowStore) ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	return nil, 0, nil
}

type resumeTestMockInstanceStore struct {
	instance     *WorkflowInstance
	statusUpdate InstanceStatus
}

func (m *resumeTestMockInstanceStore) Create(ctx context.Context, inst *WorkflowInstance) error {
	m.instance = inst
	return nil
}
func (m *resumeTestMockInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	return m.instance, nil
}
func (m *resumeTestMockInstanceStore) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	m.statusUpdate = status
	if m.instance != nil {
		m.instance.Status = status
	}
	return nil
}
func (m *resumeTestMockInstanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	return nil
}
func (m *resumeTestMockInstanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	if m.instance != nil {
		m.instance.InstanceData = data
	}
	return nil
}
func (m *resumeTestMockInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	return nil
}
func (m *resumeTestMockInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	return nil
}
func (m *resumeTestMockInstanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	return nil, nil
}
func (m *resumeTestMockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *resumeTestMockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *resumeTestMockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *resumeTestMockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

type resumeTestMockDispatcher struct {
	dispatched []string // approver IDs that received dispatches
}

func (m *resumeTestMockDispatcher) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	m.dispatched = append(m.dispatched, approverID)
	return nil
}
func (m *resumeTestMockDispatcher) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	return nil
}

// --- Helper to build test graph ---

func buildApprovalGraph(approvalCfg ApprovalNodeConfig) WorkflowGraph {
	cfgJSON, _ := json.Marshal(approvalCfg)
	return WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Review", Config: cfgJSON},
			{ID: "action-1", Type: NodeAction, Label: "Done"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"},
			{ID: "e2", SourceID: "approval-1", TargetID: "action-1"},
		},
	}
}

// --- Single Mode Tests ---

func TestResumeInstance_SingleMode_Approve(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  map[string]interface{}{"title": "Test Request"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision:   "approve",
		ApproverID: "ve-1",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance returned error: %v", err)
	}

	// Should advance to completion (action-1 → no more nodes → completed)
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("Expected instance status %q, got %q", InstanceCompleted, instStore.statusUpdate)
	}

	// Verify audit trail has approval_decision and node_completed entries
	hasDecision := false
	hasNodeCompleted := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "approval_decision" && entry.Decision == "approve" {
			hasDecision = true
		}
		if entry.EventType == "node_completed" && entry.NodeID == "approval-1" {
			hasNodeCompleted = true
		}
	}
	if !hasDecision {
		t.Error("Expected approval_decision audit entry")
	}
	if !hasNodeCompleted {
		t.Error("Expected node_completed audit entry for approval node")
	}
}

func TestResumeInstance_SingleMode_Reject(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision:   "reject",
		ApproverID: "ve-1",
		Rationale:  "Amount too high",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance returned error: %v", err)
	}

	// Should mark instance as failed
	if instStore.statusUpdate != InstanceFailed {
		t.Errorf("Expected instance status %q, got %q", InstanceFailed, instStore.statusUpdate)
	}
}

// --- Countersign Mode Tests ---

func TestResumeInstance_Countersign_AllApprove(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// First approver approves — should NOT advance yet
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("First approve error: %v", err)
	}
	if instStore.statusUpdate == InstanceCompleted {
		t.Error("Should not complete after first approval in countersign mode")
	}

	// Second approver approves — should NOT advance yet
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-2", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Second approve error: %v", err)
	}
	if instStore.statusUpdate == InstanceCompleted {
		t.Error("Should not complete after second approval in countersign mode")
	}

	// Third approver approves — NOW should advance
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-3", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Third approve error: %v", err)
	}
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("Expected instance completed after all approvals, got %q", instStore.statusUpdate)
	}
}

func TestResumeInstance_Countersign_RejectImmediately(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// First approver rejects — should immediately fail
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "reject", ApproverID: "ve-1", Rationale: "Not acceptable", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance returned error: %v", err)
	}
	if instStore.statusUpdate != InstanceFailed {
		t.Errorf("Expected instance failed on first reject in countersign, got %q", instStore.statusUpdate)
	}
}

// --- Any N of M Mode Tests ---

func TestResumeInstance_AnyNofM_PassWhenNReached(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3", "ve-4", "ve-5"},
		Mode:         ModeAnyNofM,
		MinApprovals: 3,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// First approve — not enough yet
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate == InstanceCompleted {
		t.Error("Should not complete after 1 approval (need 3)")
	}

	// Second approve — still not enough
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-2", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate == InstanceCompleted {
		t.Error("Should not complete after 2 approvals (need 3)")
	}

	// Third approve — NOW should advance (3 of 5 reached)
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-3", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("Expected completed when N approvals reached, got %q", instStore.statusUpdate)
	}
}

func TestResumeInstance_AnyNofM_RejectWhenImpossible(t *testing.T) {
	// 3 of 5 required. If 3 reject, only 2 remaining can approve → impossible to reach 3.
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3", "ve-4", "ve-5"},
		Mode:         ModeAnyNofM,
		MinApprovals: 3,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// Three rejections: remaining=2, approvalCount=0, maxPossible=0+2=2 < 3 → reject
	for _, id := range []string{"ve-1", "ve-2"} {
		err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
			Decision: "reject", ApproverID: id, DecidedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Error: %v", err)
		}
	}
	// After 2 rejections: remaining=3, approvalCount=0, maxPossible=0+3=3 >= 3 → still possible
	if instStore.statusUpdate == InstanceFailed {
		t.Error("Should not fail after 2 rejections (still possible with 3 remaining)")
	}

	// Third rejection: remaining=2, approvalCount=0, maxPossible=0+2=2 < 3 → impossible
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "reject", ApproverID: "ve-3", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate != InstanceFailed {
		t.Errorf("Expected failed when impossible to reach N, got %q", instStore.statusUpdate)
	}
}

// --- Sequential Mode Tests ---

func TestResumeInstance_Sequential_AllApprove(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:   []string{"ve-1", "ve-2", "ve-3"},
		Mode:          ModeSequential,
		ApproverOrder: []string{"ve-1", "ve-2", "ve-3"},
		TimeoutHours:  24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// First approver approves — should dispatch to second
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(dispatcher.dispatched) != 1 || dispatcher.dispatched[0] != "ve-2" {
		t.Errorf("Expected dispatch to ve-2, got %v", dispatcher.dispatched)
	}
	if instStore.statusUpdate == InstanceCompleted {
		t.Error("Should not complete after first sequential approval")
	}

	// Second approver approves — should dispatch to third
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-2", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(dispatcher.dispatched) != 2 || dispatcher.dispatched[1] != "ve-3" {
		t.Errorf("Expected dispatch to ve-3, got %v", dispatcher.dispatched)
	}

	// Third (last) approver approves — should advance workflow
	err = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-3", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("Expected completed after last sequential approval, got %q", instStore.statusUpdate)
	}
}

func TestResumeInstance_Sequential_RejectImmediately(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:   []string{"ve-1", "ve-2", "ve-3"},
		Mode:          ModeSequential,
		ApproverOrder: []string{"ve-1", "ve-2", "ve-3"},
		TimeoutHours:  24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// Second approver rejects — should immediately fail, no dispatch to third
	// First, simulate first approver approved
	_ = executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})

	// Second approver rejects
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "reject", ApproverID: "ve-2", Rationale: "Policy violation", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if instStore.statusUpdate != InstanceFailed {
		t.Errorf("Expected failed on sequential reject, got %q", instStore.statusUpdate)
	}
	// Should NOT have dispatched to ve-3
	for _, id := range dispatcher.dispatched {
		if id == "ve-3" {
			t.Error("Should not dispatch to ve-3 after ve-2 rejected")
		}
	}
}

// --- Audit Trail Tests ---

func TestResumeInstance_AuditTrailRecordsDecision(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}

	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision:    "approve",
		ApproverID:  "ve-1",
		Rationale:   "Looks good",
		MatchedRule: "rule_001",
		DecidedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	// Find the approval_decision entry
	var decisionEntry *AuditEntry
	for _, entry := range auditStore.entries {
		if entry.EventType == "approval_decision" {
			decisionEntry = entry
			break
		}
	}
	if decisionEntry == nil {
		t.Fatal("Expected approval_decision audit entry")
	}
	if decisionEntry.ActorID != "ve-1" {
		t.Errorf("ActorID = %q, want %q", decisionEntry.ActorID, "ve-1")
	}
	if decisionEntry.Decision != "approve" {
		t.Errorf("Decision = %q, want %q", decisionEntry.Decision, "approve")
	}
	if decisionEntry.MatchedRule != "rule_001" {
		t.Errorf("MatchedRule = %q, want %q", decisionEntry.MatchedRule, "rule_001")
	}
	if decisionEntry.Rationale != "Looks good" {
		t.Errorf("Rationale = %q, want %q", decisionEntry.Rationale, "Looks good")
	}
	if decisionEntry.NodeID != "approval-1" {
		t.Errorf("NodeID = %q, want %q", decisionEntry.NodeID, "approval-1")
	}
	if decisionEntry.InstanceID != "inst-1" {
		t.Errorf("InstanceID = %q, want %q", decisionEntry.InstanceID, "inst-1")
	}
}
