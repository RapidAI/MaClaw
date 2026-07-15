package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Approval E2E checklist (docs/approval-maclaw-app-e2e-improvement-plan-zh.md §生产联调清单)
// covered as automated unit-level scenarios. Real multi-machine WS delivery remains
// a manual/prod check; this file locks the Hub-side contracts those checks rely on.

// Checklist #2 — timeout + fallback cascade → escalation/overdue-style block path.
func TestChecklist_TimeoutFallbackExhaustion_BlocksAndNotifies(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID: "inst-chk-2", VersionID: "ver-1", Status: InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Expense"},
		},
	}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{fallbackErr: errors.New("offline")}
	notifier := &mockNotifier{}
	// No escalation manager → immediate block with initiator notify (legacy path still valid).
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher, WithNotifier(notifier))
	if err := exec.HandleTimeout(context.Background(), "inst-chk-2", "approval-1"); err != nil {
		t.Fatal(err)
	}
	if instStore.updatedStatus != InstanceBlocked {
		t.Fatalf("status=%s", instStore.updatedStatus)
	}
	if len(notifier.notifications) == 0 {
		t.Fatal("expected initiator notify")
	}
	if !strings.Contains(strings.ToLower(notifier.notifications[0].Reason), "blocked") {
		t.Fatalf("reason=%q", notifier.notifications[0].Reason)
	}
}

// Checklist #2 variant — with EscalationManager: queue first, then max-retries → block.
func TestChecklist_TimeoutFallback_EscalationQueueThenBlock(t *testing.T) {
	graph := buildTimeoutTestGraph("ve-fallback")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	instStore := &mockInstanceStoreForTimeout{
		instance: &WorkflowInstance{
			ID: "inst-chk-2b", VersionID: "ver-1", Status: InstanceRunning,
			InstanceData: map[string]interface{}{"title": "Leave"},
		},
	}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcherForTimeout{fallbackErr: errors.New("offline")}
	notifier := &mockNotifier{}
	esc := NewEscalationManager(dispatcher, audit, &mockHumanChecker{available: false})
	esc.maxRetries = 2
	esc.retryInterval = time.Millisecond
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher,
		WithNotifier(notifier), WithEscalationManager(esc))
	if err := exec.HandleTimeout(context.Background(), "inst-chk-2b", "approval-1"); err != nil {
		t.Fatal(err)
	}
	if instStore.updatedStatus == InstanceBlocked {
		t.Fatal("must not block while escalation pending")
	}
	if !esc.HasPendingForInstance("inst-chk-2b", "approval-1") {
		t.Fatal("expected pending escalation")
	}
	for i := 0; i < 3; i++ {
		esc.mu.Lock()
		for _, req := range esc.queue {
			req.LastAttemptAt = time.Now().Add(-time.Minute)
		}
		esc.mu.Unlock()
		esc.processPendingEscalations()
	}
	if instStore.updatedStatus != InstanceBlocked {
		t.Fatalf("after exhaust status=%s", instStore.updatedStatus)
	}
}

// Checklist #3 — EscalationManager max retries records failure + notifies.
func TestChecklist_EscalationMaxRetries_NotifiesInitiator(t *testing.T) {
	dispatcher := &mockDispatcherForEsc{}
	audit := &mockAuditStoreForEsc{}
	notifier := &captureEscNotifier{}
	mgr := NewEscalationManager(dispatcher, audit, &mockHumanChecker{available: false}).SetNotifier(notifier)
	mgr.maxRetries = 2
	mgr.retryInterval = time.Millisecond
	req := &ApprovalRequest{ID: "req", InstanceID: "inst-chk-3", NodeID: "n1"}
	_ = mgr.Escalate(context.Background(), req, "human-1")
	for i := 0; i < 2; i++ {
		mgr.mu.Lock()
		for _, r := range mgr.queue {
			r.LastAttemptAt = time.Now().Add(-time.Minute)
		}
		mgr.mu.Unlock()
		mgr.processPendingEscalations()
	}
	if audit.countByEventType("escalation_failed") != 1 {
		t.Fatalf("escalation_failed count=%d", audit.countByEventType("escalation_failed"))
	}
	if notifier.callCount() != 1 {
		t.Fatalf("NotifyInitiator calls=%d", notifier.callCount())
	}
}

// Checklist multi-failure — countersign accumulates escalation_approvers.
func TestChecklist_CountersignMultiOffline_ListsApprovers(t *testing.T) {
	cfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs: []string{"m1", "m2", "m3"}, Mode: ModeCountersign, TimeoutHours: 8,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t", Type: NodeTrigger},
			{ID: "a", Type: NodeApproval, Label: "CS", Config: cfg},
		},
		Edges: []WorkflowEdge{{ID: "e", SourceID: "t", TargetID: "a"}},
	}
	ver := &WorkflowVersion{ID: "v1", WorkflowID: "wf", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: ver, version: ver}
	instStore := &mockInstanceStore{}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcher{failApprovers: map[string]error{
		"m1": errors.New("down"), "m3": errors.New("down"),
	}}
	esc := NewEscalationManager(dispatcher, audit, &mockHumanChecker{available: false})
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher, WithEscalationManager(esc))
	if _, err := exec.StartInstance(context.Background(), "wf", `{}`); err != nil {
		t.Fatal(err)
	}
	data := instStore.createdInstance.InstanceData
	list := stringSliceFromInstanceData(data["escalation_approvers"])
	if len(list) != 2 {
		t.Fatalf("approvers=%v", list)
	}
}

// Checklist #4 (contract) — empty approver list must not invent production assignees.
// Designer empty-state is covered by hub/web/approval_workflow/workflow-editor.test.js.
func TestChecklist_EmptyApprovalRoles_NoInventedAssignees(t *testing.T) {
	// Runtime path: zero resolvable approvers fails closed (no silent auto-assignee).
	cfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs: nil, Mode: ModeSingle, TimeoutHours: 8,
	})
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t", Type: NodeTrigger},
			{ID: "a", Type: NodeApproval, Config: cfg},
		},
		Edges: []WorkflowEdge{{ID: "e", SourceID: "t", TargetID: "a"}},
	}
	ver := &WorkflowVersion{ID: "v1", WorkflowID: "wf", Status: VersionPublished, Graph: graph}
	wfStore := &mockWorkflowStore{publishedVersion: ver, version: ver}
	instStore := &mockInstanceStore{}
	audit := &mockAuditStore{}
	dispatcher := &mockDispatcher{}
	exec := NewWorkflowExecutor(wfStore, instStore, audit, dispatcher)
	_, err := exec.StartInstance(context.Background(), "wf", `{}`)
	if err == nil {
		t.Fatal("expected error when no resolvable approvers")
	}
	if !strings.Contains(err.Error(), "no resolvable approvers") {
		t.Fatalf("err=%v", err)
	}
	if len(dispatcher.dispatched) != 0 {
		t.Fatalf("must not invent approvers: %v", dispatcher.dispatched)
	}
}

// Checklist directory escalation markers for initiator views.
func TestChecklist_ApplyEscalationFieldsToDirectoryItem(t *testing.T) {
	item := DirectoryItem{Status: "running", Urgency: UrgencyNormal}
	ApplyEscalationFieldsToDirectoryItem(&item, map[string]interface{}{
		"escalation_pending":   true,
		"escalation_approvers": []string{"m1", "m2"},
	})
	if !item.EscalationPending || len(item.EscalationApprovers) != 2 {
		t.Fatalf("%#v", item)
	}
	if item.Urgency != UrgencyApproachingTimeout {
		t.Fatalf("urgency=%s", item.Urgency)
	}
	if item.Result != "escalation pending" {
		t.Fatalf("result=%s", item.Result)
	}
}

// Checklist #5-related contract — markNodeBlocked clears escalation markers so
// reconcile does not see stale escalation_pending after terminal block.
func TestChecklist_MarkBlocked_ClearsEscalationMarkers(t *testing.T) {
	graph := buildTimeoutTestGraph("")
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &mockWorkflowStoreWithVersion{version: ver}
	inst := &WorkflowInstance{
		ID: "inst-chk-5", VersionID: "ver-1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{
			"escalation_pending":   true,
			"escalation_approver":  "ve-x",
			"escalation_approvers": []string{"ve-x", "ve-y"},
			"escalation_reason":    "partial_dispatch",
		},
	}
	instStore := &mockInstanceStoreForTimeout{instance: inst}
	audit := &mockAuditStore{}
	exec := NewWorkflowExecutor(wfStore, instStore, audit, &mockDispatcherForTimeout{}, WithNotifier(&mockNotifier{}))
	node := findNodeByID(&graph, "approval-1")
	if err := exec.markNodeBlocked(context.Background(), inst, node, "timeout", "no fallback"); err != nil {
		t.Fatal(err)
	}
	if _, ok := inst.InstanceData["escalation_pending"]; ok {
		t.Fatalf("escalation_pending should be cleared: %#v", inst.InstanceData)
	}
	if _, ok := inst.InstanceData["escalation_approvers"]; ok {
		t.Fatalf("escalation_approvers should be cleared: %#v", inst.InstanceData)
	}
	if inst.InstanceData["blocked_reason"] != "timeout" {
		t.Fatalf("blocked_reason=%v", inst.InstanceData["blocked_reason"])
	}
}
