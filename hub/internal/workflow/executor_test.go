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
	version          *WorkflowVersion
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
	if m.version != nil {
		return m.version, nil
	}
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
	createdExecs    []*NodeExecution
	createErr       error
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
	if m.createdInstance != nil && m.createdInstance.ID == id {
		m.createdInstance.InstanceData = data
	}
	return nil
}
func (m *mockInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	m.createdExecs = append(m.createdExecs, exec)
	return nil
}
func (m *mockInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	for _, exec := range m.createdExecs {
		if exec.ID == id {
			exec.Status = status
			exec.Result = result
			exec.FailReason = failReason
			return nil
		}
	}
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

type mockDispatcher struct {
	dispatched      []string
	dispatchErr     error
	failApprovers   map[string]error // per-approver dispatch failure
	fallbackIDs     []string
	fallbackErr     error
}

func (m *mockDispatcher) Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error {
	m.dispatched = append(m.dispatched, approverID)
	if m.failApprovers != nil {
		if err, ok := m.failApprovers[approverID]; ok && err != nil {
			return err
		}
	}
	return m.dispatchErr
}
func (m *mockDispatcher) DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error {
	m.fallbackIDs = append(m.fallbackIDs, fallbackID)
	return m.fallbackErr
}

type mockApproverResolver struct {
	values map[string][]string
}

func (m mockApproverResolver) ResolveApproverIDs(ctx context.Context, approverIDs []string) ([]string, error) {
	var out []string
	for _, id := range approverIDs {
		if resolved := m.values[id]; len(resolved) > 0 {
			out = append(out, resolved...)
			continue
		}
		out = append(out, id)
	}
	return out, nil
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
	if exec.Status != NodeCompleted {
		t.Errorf("Trigger NodeExecution Status = %q, want %q", exec.Status, NodeCompleted)
	}
	if len(instStore.createdExecs) < 2 || instStore.createdExecs[1].NodeID != "approval-1" || instStore.createdExecs[1].Status != NodeRunning {
		t.Errorf("Approval NodeExecution = %#v, want running approval-1", instStore.createdExecs)
	}
	if exec.InstanceID != instance.ID {
		t.Errorf("NodeExecution InstanceID = %q, want %q", exec.InstanceID, instance.ID)
	}
}

func TestStartInstance_PrimaryDispatchFailure_QueuesEscalation(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-primary"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Review", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{dispatchErr: errors.New("machine offline")}
	notifier := &mockNotifier{}
	escMgr := NewEscalationManager(dispatcher, auditStore, &mockHumanChecker{available: false})
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher,
		WithNotifier(notifier),
		WithEscalationManager(escMgr),
	)

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{"title":"Leave"}`)
	if err != nil {
		t.Fatalf("StartInstance should absorb dispatch failure via escalation: %v", err)
	}
	if inst.Status != InstanceRunning {
		t.Fatalf("status=%s want running", inst.Status)
	}
	if !escMgr.HasPendingForInstance(inst.ID, "approval-1") {
		t.Fatal("expected pending escalation for primary approver")
	}
	// Instance data may be on createdInstance after UpdateInstanceData.
	data := inst.InstanceData
	if instStore.createdInstance != nil && instStore.createdInstance.InstanceData != nil {
		data = instStore.createdInstance.InstanceData
	}
	if pending, _ := data["escalation_pending"].(bool); !pending {
		t.Fatalf("expected escalation_pending, data=%#v", data)
	}
	hasDispatchFailed := false
	for _, e := range auditStore.entries {
		if e.EventType == "dispatch_failed" {
			hasDispatchFailed = true
		}
	}
	if !hasDispatchFailed {
		t.Fatal("expected dispatch_failed audit")
	}
}

func TestStartInstance_CountersignPartialFailure_QueuesEscalationAndContinues(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-a", "ve-b", "ve-c"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Countersign", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{
		failApprovers: map[string]error{"ve-b": errors.New("ve-b offline")},
	}
	notifier := &mockNotifier{}
	escMgr := NewEscalationManager(dispatcher, auditStore, &mockHumanChecker{available: false})
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher,
		WithNotifier(notifier),
		WithEscalationManager(escMgr),
	)

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{}`)
	if err != nil {
		t.Fatalf("StartInstance: %v", err)
	}
	// ve-a and ve-c should still receive; ve-b failed then escalated.
	if len(dispatcher.dispatched) != 3 {
		t.Fatalf("dispatched=%v want all three attempted", dispatcher.dispatched)
	}
	if inst.Status != InstanceRunning {
		t.Fatalf("status=%s want running (not soft-blocked)", inst.Status)
	}
	if !escMgr.HasPendingForInstance(inst.ID, "approval-1") {
		t.Fatal("expected escalation pending for ve-b")
	}
	hasPartial := false
	for _, e := range auditStore.entries {
		if e.EventType == "dispatch_partial_failure" {
			hasPartial = true
		}
	}
	if !hasPartial {
		t.Fatal("expected dispatch_partial_failure audit")
	}
}

func TestOnEscalationFailed_PeerExhaustedKeepsInstanceRunning(t *testing.T) {
	// Two peers queued; first exhausts max retries — instance must stay running
	// while the second peer remains pending.
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{{ID: "approval-1", Type: NodeApproval, Label: "CS"}},
	}
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	inst := &WorkflowInstance{
		ID: "inst-multi-fail", VersionID: "ver-1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{
			"escalation_pending":   true,
			"escalation_approvers": []string{"human-a", "human-b"},
			"escalation_approver":  "human-b",
		},
	}
	instStore := &mockInstanceStoreForTimeout{instance: inst}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	checker := &mockHumanChecker{available: false}
	esc := NewEscalationManager(dispatcher, audit, checker)
	esc.maxRetries = 2
	esc.retryInterval = time.Millisecond
	notifier := &mockNotifier{}
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher,
		WithNotifier(notifier),
		WithEscalationManager(esc),
	)
	// Seed both peers into the queue.
	req := &ApprovalRequest{ID: "r", InstanceID: inst.ID, NodeID: "approval-1"}
	_ = esc.Escalate(context.Background(), req, "human-a")
	_ = esc.Escalate(context.Background(), req, "human-b")
	// Exhaust only human-a by driving processPending until a is gone but b remains.
	// Both have same attempts; process both — set maxRetries high and manually fail a.
	// Simpler: call onEscalationFailed directly for human-a while b still queued.
	esc.mu.Lock()
	var escA *EscalationRequest
	for _, r := range esc.queue {
		if r.HumanApprover == "human-a" {
			escA = r
			delete(esc.queue, r.ID) // simulate markEscalationFailed queue removal
			break
		}
	}
	esc.mu.Unlock()
	if escA == nil {
		t.Fatal("human-a not in queue")
	}
	escA.Attempts = 5
	exec.onEscalationFailed(context.Background(), escA)

	if instStore.updatedStatus == InstanceBlocked {
		t.Fatal("must not block while human-b still pending")
	}
	list := stringSliceFromInstanceData(inst.InstanceData["escalation_approvers"])
	for _, id := range list {
		if id == "human-a" {
			t.Fatalf("human-a should be removed from approvers: %v", list)
		}
	}
	if !esc.HasPendingApprover(inst.ID, "approval-1", "human-b") {
		t.Fatal("human-b should still be pending in manager")
	}
	if len(notifier.notifications) < 1 {
		t.Fatal("expected peer-failed notify")
	}
}

func TestOnEscalationFailed_LastPeerBlocks(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{{ID: "approval-1", Type: NodeApproval, Label: "A"}},
	}
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	inst := &WorkflowInstance{
		ID: "inst-last", VersionID: "ver-1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{
			"escalation_pending":   true,
			"escalation_approvers": []string{"human-only"},
		},
	}
	instStore := &mockInstanceStoreForTimeout{instance: inst}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	esc := NewEscalationManager(dispatcher, audit, &mockHumanChecker{available: false})
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher,
		WithNotifier(&mockNotifier{}),
		WithEscalationManager(esc),
	)
	exec.onEscalationFailed(context.Background(), &EscalationRequest{
		InstanceID: inst.ID, NodeID: "approval-1", HumanApprover: "human-only", Attempts: 5,
	})
	if instStore.updatedStatus != InstanceBlocked {
		t.Fatalf("status=%s want blocked when last peer fails", instStore.updatedStatus)
	}
}

func TestEnqueueEscalationOrBlock_ImmediateDeliverDoesNotMarkOtherPeerPending(t *testing.T) {
	// human-a is offline (queued); human-b comes online and Escalate delivers
	// immediately — must not append human-b to escalation_approvers.
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{{ID: "approval-1", Type: NodeApproval, Label: "CS"}},
	}
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	inst := &WorkflowInstance{
		ID: "inst-peer", VersionID: "ver-1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{},
	}
	instStore := &mockInstanceStoreForTimeout{instance: inst}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{}
	// Checker toggled per Escalate: start false so first queues.
	checker := &mockHumanChecker{available: false}
	esc := NewEscalationManager(dispatcher, audit, checker)
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher, WithEscalationManager(esc))
	node := &WorkflowNode{ID: "approval-1", Label: "CS"}
	req := &ApprovalRequest{ID: "areq", InstanceID: inst.ID, NodeID: node.ID}

	if err := exec.enqueueEscalationOrBlock(context.Background(), inst, node, req, "human-a", "partial_dispatch", "a down"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := inst.InstanceData["escalation_pending"].(bool); !pending {
		t.Fatal("expected pending after human-a queue")
	}
	checker.setAvailable(true)
	if err := exec.enqueueEscalationOrBlock(context.Background(), inst, node, req, "human-b", "partial_dispatch", "b ok"); err != nil {
		t.Fatal(err)
	}
	list := stringSliceFromInstanceData(inst.InstanceData["escalation_approvers"])
	for _, id := range list {
		if id == "human-b" {
			t.Fatalf("human-b delivered immediately but listed in approvers: %v", list)
		}
	}
	if len(list) != 1 || list[0] != "human-a" {
		t.Fatalf("approvers=%v want only human-a", list)
	}
}

func TestStartInstance_CountersignMultiFailure_AccumulatesApproversList(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-a", "ve-b", "ve-c"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Countersign", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{
		failApprovers: map[string]error{
			"ve-a": errors.New("a offline"),
			"ve-c": errors.New("c offline"),
		},
	}
	escMgr := NewEscalationManager(dispatcher, auditStore, &mockHumanChecker{available: false})
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher,
		WithEscalationManager(escMgr),
	)

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{}`)
	if err != nil {
		t.Fatalf("StartInstance: %v", err)
	}
	data := inst.InstanceData
	if instStore.createdInstance != nil && instStore.createdInstance.InstanceData != nil {
		data = instStore.createdInstance.InstanceData
	}
	list := stringSliceFromInstanceData(data["escalation_approvers"])
	if len(list) != 2 {
		t.Fatalf("escalation_approvers=%v want [ve-a ve-c]", list)
	}
	seen := map[string]bool{}
	for _, id := range list {
		seen[id] = true
	}
	if !seen["ve-a"] || !seen["ve-c"] {
		t.Fatalf("list=%v", list)
	}
	// ve-b succeeded so only a,c pending.
	if escMgr.PendingCount() != 2 {
		t.Fatalf("PendingCount=%d want 2", escMgr.PendingCount())
	}
}

func TestStartInstance_AnyNofMPartialFailure_LegacySoftBlockWithoutManager(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-a", "ve-b"},
		Mode:         ModeAnyNofM,
		MinApprovals: 1,
		TimeoutHours: 24,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "AnyN", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{
		failApprovers: map[string]error{"ve-b": errors.New("offline")},
	}
	// No EscalationManager → legacy soft-block after first failure (stops fan-out).
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{}`)
	if err != nil {
		t.Fatalf("StartInstance: %v", err)
	}
	if inst.Status != InstanceBlocked {
		// Status is set on in-memory inst; mock UpdateStatus doesn't mutate inst.
		// Check via audit / that only ve-a was attempted then stop... actually
		// UpdateStatus is called and inst.Status is set on the local object returned.
		t.Fatalf("status=%s want blocked", inst.Status)
	}
	// First approver ok, second fails → stop without dispatching further (only 2 total).
	if len(dispatcher.dispatched) != 2 {
		t.Fatalf("dispatched=%v", dispatcher.dispatched)
	}
}

func TestStartInstance_PrimaryDispatchFailure_RoutesToFallback(t *testing.T) {
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:      []string{"ve-primary"},
		Mode:             ModeSingle,
		TimeoutHours:     24,
		FallbackApprover: "ve-fallback",
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Review", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{dispatchErr: errors.New("primary offline")}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{}`)
	if err != nil {
		t.Fatalf("StartInstance: %v", err)
	}
	if len(dispatcher.fallbackIDs) != 1 || dispatcher.fallbackIDs[0] != "ve-fallback" {
		t.Fatalf("fallback = %#v, want ve-fallback", dispatcher.fallbackIDs)
	}
	if inst.Status != InstanceRunning {
		t.Fatalf("status=%s", inst.Status)
	}
}

func TestApprovalRoleReferenceResolvesBeforeDispatchAndDecision(t *testing.T) {
	roleID := "role:function:finance:finance_approver"
	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{roleID},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: NodeApproval, Label: "Review", Config: approvalCfg},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"}},
	}
	version := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: version, version: version}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithApprovalApproverResolver(mockApproverResolver{values: map[string][]string{
		roleID: {"machine-finance-1"},
	}}))

	inst, err := executor.StartInstance(context.Background(), "wf-1", `{}`)
	if err != nil {
		t.Fatalf("StartInstance returned error: %v", err)
	}
	if len(dispatcher.dispatched) != 1 || dispatcher.dispatched[0] != "machine-finance-1" {
		t.Fatalf("dispatched approvers = %#v, want machine-finance-1", dispatcher.dispatched)
	}
	if len(instStore.createdExecs) < 2 || len(instStore.createdExecs[1].Result) == 0 {
		t.Fatalf("approval node execution missing runtime metadata: %#v", instStore.createdExecs)
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(instStore.createdExecs[1].Result, &metadata); err != nil {
		t.Fatalf("decode approval runtime metadata: %v", err)
	}
	if got := stringSliceFromAny(metadata["original_approvers"]); len(got) != 1 || got[0] != roleID {
		t.Fatalf("original_approvers = %#v, want %s", got, roleID)
	}
	if got := stringSliceFromAny(metadata["approver_ids"]); len(got) != 1 || got[0] != "machine-finance-1" {
		t.Fatalf("approver_ids = %#v, want machine-finance-1", got)
	}

	err = executor.ResumeInstance(context.Background(), inst.ID, "approval-1", ApprovalResponse{
		ApproverID: "machine-finance-1",
		Decision:   approvalDecisionApprove,
	})
	if err != nil {
		t.Fatalf("ResumeInstance with resolved approver returned error: %v", err)
	}
}

func stringSliceFromAny(value interface{}) []string {
	items, _ := value.([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
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
