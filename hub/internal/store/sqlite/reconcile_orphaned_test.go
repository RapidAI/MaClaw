package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	_ "modernc.org/sqlite"
)

// TestReconcileOrphanedInstances_RepairsCompletedInstanceWithoutConfirmations is
// a focused end-to-end test for task 3.5 (Requirement 2.5 / Finding 1.5).
//
// It reproduces the documented crash window in executeTerminalNode: an instance
// is marked completed BEFORE StartTracking creates its confirmation records. If
// the process crashes in that window, the instance is "completed" but has zero
// rows in the confirmations table — orphaned. The reminder loop's FindOverdue
// query only inspects the confirmations table, so it can never see such an
// instance. ReconcileOrphanedInstances must detect and repair it.
//
// This test uses the REAL production stores (sqlite instanceStore implementing
// OrphanedInstanceFinder, PgConfirmationStore, WorkflowStoreSQLite) so it
// exercises the actual orphan query and the StartTracking re-derivation path —
// not a mock.
func TestReconcileOrphanedInstances_RepairsCompletedInstanceWithoutConfirmations(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	ctx := context.Background()

	instStore := NewInstanceStore(db)
	confStore := workflow.NewPgConfirmationStore(db)
	wfStore := NewWorkflowStore(db)
	auditStore := NewAuditStore(db)

	// --- Build a published version whose terminal node configures executors. ---
	terminalCfg, _ := json.Marshal(workflow.TerminalNodeConfig{
		ResultExecutors: []workflow.ExecutorConfig{
			{UserID: "exec-1", TimeoutHours: 48, MaxReminders: 3, ReminderInterval: 24},
			{UserID: "exec-2", TimeoutHours: 48, MaxReminders: 3, ReminderInterval: 24},
		},
		Notifiers: []workflow.NotifierConfig{
			{UserID: "notif-1", TimeoutHours: 72, MaxReminders: 2, ReminderInterval: 24},
		},
	})
	graph := workflow.WorkflowGraph{
		Nodes: []workflow.WorkflowNode{
			{ID: "trigger-1", Type: workflow.NodeTrigger, Label: "Start"},
			{ID: "terminal-1", Type: workflow.NodeTypeTerminal, Label: "End", Config: terminalCfg},
		},
		Edges: []workflow.WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "terminal-1"}},
	}

	now := time.Now().UTC()
	def := &workflow.WorkflowDefinition{ID: "wf-1", OwnerID: "owner-1", Name: "Leave Approval", CreatedAt: now, UpdatedAt: now}
	if err := wfStore.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &workflow.WorkflowVersion{
		ID: "ver-1", WorkflowID: "wf-1", VersionNumber: "1.0.0",
		Status: workflow.VersionPublished, Graph: graph, CreatedAt: now, UpdatedAt: now,
	}
	if err := wfStore.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// --- An orphaned completed instance: completed, within the retention
	// window, with NO confirmation rows (the crash window). ---
	completedAt := now.Add(-2 * time.Hour)
	orphan := &workflow.WorkflowInstance{
		ID:            "inst-orphan",
		WorkflowID:    "wf-1",
		VersionID:     "ver-1",
		Status:        workflow.InstanceCompleted,
		CurrentNodeID: "terminal-1",
		InstanceData:  map[string]interface{}{"initiator_id": "owner-1"},
		CreatedAt:     now.Add(-3 * time.Hour),
		CompletedAt:   &completedAt,
	}
	if err := instStore.Create(ctx, orphan); err != nil {
		t.Fatalf("Create orphan instance: %v", err)
	}

	// --- Build the tracker as production wires it, with the workflow store
	// injected so reconciliation can re-derive the terminal node config. ---
	notifDispatcher := workflow.NewNotificationDispatcher(nil, nil, auditStore, nil)
	tracker := workflow.NewConfirmationTracker(confStore, instStore, notifDispatcher, auditStore).
		SetWorkflowStore(wfStore)

	// Precondition: no confirmation rows for the orphaned instance.
	beforeRows, err := confStore.ListByInstance(ctx, "inst-orphan")
	if err != nil {
		t.Fatalf("ListByInstance (before): %v", err)
	}
	if len(beforeRows) != 0 {
		t.Fatalf("setup invariant: expected 0 confirmations before reconcile, got %d", len(beforeRows))
	}

	// --- Reconcile. ---
	if err := tracker.ReconcileOrphanedInstances(ctx); err != nil {
		t.Fatalf("ReconcileOrphanedInstances: %v", err)
	}

	// --- The missing confirmation records (2 executors + 1 notifier = 3) must
	// now exist, created via the existing StartTracking. ---
	afterRows, err := confStore.ListByInstance(ctx, "inst-orphan")
	if err != nil {
		t.Fatalf("ListByInstance (after): %v", err)
	}
	if len(afterRows) != 3 {
		t.Fatalf("expected 3 confirmation records after reconcile (2 executors + 1 notifier), got %d", len(afterRows))
	}

	// Verify the records mirror the terminal node config (recipients + types).
	gotExec := map[string]bool{}
	gotNotif := map[string]bool{}
	for _, c := range afterRows {
		if c.Status != workflow.ConfirmPending {
			t.Errorf("reconciled confirmation %s has status %q, want pending", c.ID, c.Status)
		}
		switch c.Type {
		case workflow.ConfirmTypeExecutor:
			gotExec[c.RecipientID] = true
		case workflow.ConfirmTypeNotifier:
			gotNotif[c.RecipientID] = true
		}
	}
	if !gotExec["exec-1"] || !gotExec["exec-2"] {
		t.Errorf("expected executor confirmations for exec-1 and exec-2, got %v", gotExec)
	}
	if !gotNotif["notif-1"] {
		t.Errorf("expected notifier confirmation for notif-1, got %v", gotNotif)
	}
}

// TestReconcileOrphanedInstances_Idempotent verifies that running reconciliation
// twice does not create duplicate confirmation records: once an instance has
// confirmation rows it is no longer orphaned, so StartTracking is not invoked
// again for it.
func TestReconcileOrphanedInstances_Idempotent(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	ctx := context.Background()

	instStore := NewInstanceStore(db)
	confStore := workflow.NewPgConfirmationStore(db)
	wfStore := NewWorkflowStore(db)
	auditStore := NewAuditStore(db)

	terminalCfg, _ := json.Marshal(workflow.TerminalNodeConfig{
		ResultExecutors: []workflow.ExecutorConfig{{UserID: "exec-1", TimeoutHours: 48}},
	})
	graph := workflow.WorkflowGraph{
		Nodes: []workflow.WorkflowNode{
			{ID: "trigger-1", Type: workflow.NodeTrigger, Label: "Start"},
			{ID: "terminal-1", Type: workflow.NodeTypeTerminal, Label: "End", Config: terminalCfg},
		},
		Edges: []workflow.WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "terminal-1"}},
	}
	now := time.Now().UTC()
	if err := wfStore.CreateWorkflow(ctx, &workflow.WorkflowDefinition{ID: "wf-1", OwnerID: "owner-1", Name: "WF", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := wfStore.CreateVersion(ctx, &workflow.WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", VersionNumber: "1.0.0", Status: workflow.VersionPublished, Graph: graph, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	completedAt := now.Add(-1 * time.Hour)
	if err := instStore.Create(ctx, &workflow.WorkflowInstance{
		ID: "inst-orphan", WorkflowID: "wf-1", VersionID: "ver-1",
		Status: workflow.InstanceCompleted, CurrentNodeID: "terminal-1",
		InstanceData: map[string]interface{}{}, CreatedAt: now.Add(-2 * time.Hour), CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("Create orphan: %v", err)
	}

	tracker := workflow.NewConfirmationTracker(confStore, instStore, workflow.NewNotificationDispatcher(nil, nil, auditStore, nil), auditStore).
		SetWorkflowStore(wfStore)

	if err := tracker.ReconcileOrphanedInstances(ctx); err != nil {
		t.Fatalf("ReconcileOrphanedInstances (1st): %v", err)
	}
	if err := tracker.ReconcileOrphanedInstances(ctx); err != nil {
		t.Fatalf("ReconcileOrphanedInstances (2nd): %v", err)
	}

	rows, err := confStore.ListByInstance(ctx, "inst-orphan")
	if err != nil {
		t.Fatalf("ListByInstance: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 confirmation after two reconcile runs (idempotent), got %d", len(rows))
	}
}

// TestReconcileOrphanedInstances_SkipsInstanceWithExistingConfirmations verifies
// that a completed instance that ALREADY has confirmation records is not treated
// as orphaned (the orphan query excludes it), so no extra records are created.
func TestReconcileOrphanedInstances_SkipsInstanceWithExistingConfirmations(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	ctx := context.Background()

	instStore := NewInstanceStore(db)
	confStore := workflow.NewPgConfirmationStore(db)
	wfStore := NewWorkflowStore(db)
	auditStore := NewAuditStore(db)

	terminalCfg, _ := json.Marshal(workflow.TerminalNodeConfig{
		ResultExecutors: []workflow.ExecutorConfig{{UserID: "exec-1", TimeoutHours: 48}},
	})
	graph := workflow.WorkflowGraph{
		Nodes: []workflow.WorkflowNode{
			{ID: "trigger-1", Type: workflow.NodeTrigger, Label: "Start"},
			{ID: "terminal-1", Type: workflow.NodeTypeTerminal, Label: "End", Config: terminalCfg},
		},
		Edges: []workflow.WorkflowEdge{{ID: "e1", SourceID: "trigger-1", TargetID: "terminal-1"}},
	}
	now := time.Now().UTC()
	if err := wfStore.CreateWorkflow(ctx, &workflow.WorkflowDefinition{ID: "wf-1", OwnerID: "owner-1", Name: "WF", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := wfStore.CreateVersion(ctx, &workflow.WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", VersionNumber: "1.0.0", Status: workflow.VersionPublished, Graph: graph, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	completedAt := now.Add(-1 * time.Hour)
	if err := instStore.Create(ctx, &workflow.WorkflowInstance{
		ID: "inst-tracked", WorkflowID: "wf-1", VersionID: "ver-1",
		Status: workflow.InstanceCompleted, CurrentNodeID: "terminal-1",
		InstanceData: map[string]interface{}{}, CreatedAt: now.Add(-2 * time.Hour), CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("Create instance: %v", err)
	}
	// This instance already has a confirmation record (not orphaned). Use a
	// distinct recipient so we can detect any spurious duplicate.
	if err := confStore.Create(ctx, &workflow.Confirmation{
		ID: "conf-existing", InstanceID: "inst-tracked", RecipientID: "exec-original",
		Type: workflow.ConfirmTypeExecutor, Status: workflow.ConfirmPending, TimeoutHours: 48,
	}); err != nil {
		t.Fatalf("Create existing confirmation: %v", err)
	}

	tracker := workflow.NewConfirmationTracker(confStore, instStore, workflow.NewNotificationDispatcher(nil, nil, auditStore, nil), auditStore).
		SetWorkflowStore(wfStore)

	if err := tracker.ReconcileOrphanedInstances(ctx); err != nil {
		t.Fatalf("ReconcileOrphanedInstances: %v", err)
	}

	rows, err := confStore.ListByInstance(ctx, "inst-tracked")
	if err != nil {
		t.Fatalf("ListByInstance: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the existing 1 confirmation untouched (instance not orphaned), got %d", len(rows))
	}
	if rows[0].RecipientID != "exec-original" {
		t.Errorf("existing confirmation was altered: recipient = %q, want exec-original", rows[0].RecipientID)
	}
}
