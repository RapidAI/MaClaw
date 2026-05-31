package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Task 3.7 — Recoverable mid-graph execution (Finding 1.7 / Requirement 2.7).
//
// These tests exercise the StartInstance recursion path (executeNode + node
// handlers) and assert the mechanism-level guarantee: critical state/audit
// writes are NOT silently dropped, and a mid-graph write failure leaves the
// instance in a consistent, resumable state.
//
// The mechanism distinguishes two write classes:
//   - "where-to-resume" writes (UpdateCurrentNode, CreateNodeExecution) are
//     propagated as fatal: on failure the node is NOT executed and the cursor
//     is NOT advanced past it, so the instance stays resumable at a clean
//     pre-execution boundary.
//   - post-execution bookkeeping writes (node-completion status, audit events)
//     are surfaced via a "critical_write_failed" audit breadcrumb rather than
//     dropped with `_ =`.

// midGraphMockInstanceStore is an InstanceStore that injects write failures at
// specific points so we can observe the executor's mid-graph behavior. It
// tracks the persisted current node and the node-execution records created.
type midGraphMockInstanceStore struct {
	instance *WorkflowInstance

	persistedCurrentNode string
	createdExecNodes     []string
	statusUpdates        []InstanceStatus

	failUpdateCurrentNodeFor string // node ID whose UpdateCurrentNode should fail
	failCreateNodeExecFor    string // node ID whose CreateNodeExecution should fail
}

func (m *midGraphMockInstanceStore) Create(_ context.Context, inst *WorkflowInstance) error {
	m.instance = inst
	m.persistedCurrentNode = inst.CurrentNodeID
	return nil
}

func (m *midGraphMockInstanceStore) Get(_ context.Context, _ string) (*WorkflowInstance, error) {
	return m.instance, nil
}

func (m *midGraphMockInstanceStore) UpdateStatus(_ context.Context, _ string, status InstanceStatus) error {
	m.statusUpdates = append(m.statusUpdates, status)
	if m.instance != nil {
		m.instance.Status = status
	}
	return nil
}

func (m *midGraphMockInstanceStore) UpdateCurrentNode(_ context.Context, _, nodeID string) error {
	if m.failUpdateCurrentNodeFor != "" && nodeID == m.failUpdateCurrentNodeFor {
		return errors.New("simulated UpdateCurrentNode failure")
	}
	m.persistedCurrentNode = nodeID
	return nil
}

func (m *midGraphMockInstanceStore) UpdateInstanceData(_ context.Context, _ string, data map[string]interface{}) error {
	if m.instance != nil {
		m.instance.InstanceData = data
	}
	return nil
}

func (m *midGraphMockInstanceStore) CreateNodeExecution(_ context.Context, exec *NodeExecution) error {
	if m.failCreateNodeExecFor != "" && exec.NodeID == m.failCreateNodeExecFor {
		return errors.New("simulated CreateNodeExecution failure")
	}
	m.createdExecNodes = append(m.createdExecNodes, exec.NodeID)
	return nil
}

func (m *midGraphMockInstanceStore) UpdateNodeExecution(_ context.Context, _ string, _ NodeStatus, _ json.RawMessage, _ string) error {
	return nil
}

func (m *midGraphMockInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return nil, nil
}
func (m *midGraphMockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *midGraphMockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *midGraphMockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *midGraphMockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// midGraphFailEventAuditStore records audit entries but fails Append for a
// single targeted event type, so we can verify the executor surfaces (rather
// than drops) a failed post-execution audit write.
type midGraphFailEventAuditStore struct {
	entries       []*AuditEntry
	failEventType string
}

func (m *midGraphFailEventAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	if m.failEventType != "" && entry.EventType == m.failEventType {
		return errors.New("simulated audit append failure for " + entry.EventType)
	}
	m.entries = append(m.entries, entry)
	return nil
}
func (m *midGraphFailEventAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *midGraphFailEventAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *midGraphFailEventAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *midGraphFailEventAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

// buildLinearGraph builds a non-blocking trigger -> action-a -> action-b graph
// so StartInstance recurses through multiple nodes (mid-graph execution).
func buildLinearGraph() WorkflowGraph {
	return WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "action-a", Type: NodeAction, Label: "Step A"},
			{ID: "action-b", Type: NodeAction, Label: "Step B"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "action-a"},
			{ID: "e2", SourceID: "action-a", TargetID: "action-b"},
		},
	}
}

// TestExecuteNode_UpdateCurrentNodeFailure_PropagatesAndLeavesResumableState
// asserts that a failure of the "where-to-resume" write (UpdateCurrentNode)
// mid-graph is propagated (not dropped) and the cursor is NOT advanced past the
// failed node, leaving the instance resumable at its prior consistent position.
func TestExecuteNode_UpdateCurrentNodeFailure_PropagatesAndLeavesResumableState(t *testing.T) {
	graph := buildLinearGraph()
	wfStore := &mockWorkflowStore{publishedVersion: &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}}
	instStore := &midGraphMockInstanceStore{failUpdateCurrentNodeFor: "action-a"}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	inst, err := executor.StartInstance(context.Background(), "wf-1", "")
	if err == nil {
		t.Fatal("expected StartInstance to propagate the UpdateCurrentNode failure, got nil")
	}
	if !strings.Contains(err.Error(), "advance current node to action-a") {
		t.Fatalf("expected error to identify the failed advance, got: %v", err)
	}

	// Resumable state: the persisted cursor must not have drifted past the last
	// node that successfully advanced (trigger-1). action-a must not have run.
	if instStore.persistedCurrentNode != "trigger-1" {
		t.Errorf("persisted current node = %q, want %q (no drift past the failed advance)", instStore.persistedCurrentNode, "trigger-1")
	}
	if inst != nil && inst.CurrentNodeID != "trigger-1" {
		t.Errorf("in-memory current node = %q, want %q (cursor not advanced on failure)", inst.CurrentNodeID, "trigger-1")
	}
	// action-a must NOT have been executed: no node-execution record for it.
	for _, n := range instStore.createdExecNodes {
		if n == "action-a" {
			t.Errorf("action-a node execution was created despite the advance failure; node should not have executed")
		}
	}
}

// TestExecuteNode_CreateNodeExecutionFailure_PropagatesBeforeRunningNode asserts
// that a failure to record the in-flight node-execution (the durable account of
// the node running) is propagated and the node is NOT executed, so the instance
// is left consistent and resumable.
func TestExecuteNode_CreateNodeExecutionFailure_PropagatesBeforeRunningNode(t *testing.T) {
	graph := buildLinearGraph()
	wfStore := &mockWorkflowStore{publishedVersion: &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}}
	instStore := &midGraphMockInstanceStore{failCreateNodeExecFor: "action-a"}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-1", "")
	if err == nil {
		t.Fatal("expected StartInstance to propagate the CreateNodeExecution failure, got nil")
	}
	if !strings.Contains(err.Error(), "create node execution for action-a") {
		t.Fatalf("expected error to identify the failed node-exec creation, got: %v", err)
	}

	// action-a did not execute, so the downstream node (action-b) must never
	// have been reached: no node-execution record for action-b, and the
	// instance was never marked completed.
	for _, n := range instStore.createdExecNodes {
		if n == "action-b" {
			t.Errorf("action-b executed even though action-a's node-exec write failed; execution should have stopped")
		}
	}
	for _, s := range instStore.statusUpdates {
		if s == InstanceCompleted {
			t.Errorf("instance was marked completed despite a mid-graph node-exec write failure")
		}
	}
}

// TestExecuteNode_PostExecutionAuditFailure_IsSurfacedNotDropped asserts that a
// failed post-execution bookkeeping write (the node_completed audit event) is
// NOT silently dropped: the executor records a "critical_write_failed"
// breadcrumb identifying the dropped op, keeping a mid-graph crash diagnosable.
func TestExecuteNode_PostExecutionAuditFailure_IsSurfacedNotDropped(t *testing.T) {
	graph := buildLinearGraph()
	wfStore := &mockWorkflowStore{publishedVersion: &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}}
	instStore := &midGraphMockInstanceStore{}
	// Fail the node_completed audit write; the surfaced breadcrumb
	// (critical_write_failed) must still be recorded.
	auditStore := &midGraphFailEventAuditStore{failEventType: "node_completed"}
	dispatcher := &mockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	_, err := executor.StartInstance(context.Background(), "wf-1", "")
	if err != nil {
		t.Fatalf("post-execution audit failures are non-fatal; StartInstance should not error, got: %v", err)
	}

	var found bool
	for _, e := range auditStore.entries {
		if e.EventType == "critical_write_failed" && strings.Contains(e.Details, "audit_node_completed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a critical_write_failed breadcrumb for the dropped node_completed audit write; entries=%v", auditStore.entries)
	}
}
