package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestTaskListUsesPersistedAgentRecovery(t *testing.T) {
	app := newProjectSearchTestApp(t)
	// Settle the path-bound state before injecting the memory. The first data
	// directory lookup applies the config, and a data directory left behind by
	// another test makes that apply reset path-bound state and drop the
	// injected memory in the middle of the status lookup.
	app.ensureMemoryStore()
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app.aiConversationMemory = memory
	result := &ProjectSearchResult{ProjectPath: "D:/work/task", HasOutput: true}
	memory.UpsertUnfinishedSlot(projectSessionOwnerID(result.ProjectPath), &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1", Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})

	app.applyTaskListWorkflow(result, result.ProjectPath, nil, nil, nil, nil, nil)

	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "interrupted" || result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("task recovery status = %#v", result.ActiveWorkflow)
	}

	// The task was resumed and its session is executing now. The recovery slot
	// describes the attempt that stopped, so the row reports the live run.
	live := map[string]struct{}{normalizeRecentTaskPathKey(result.ProjectPath): {}}
	app.applyTaskListWorkflow(result, result.ProjectPath, nil, nil, nil, nil, live)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != string(codingruntime.TaskRunning) {
		t.Fatalf("task running status = %#v, want running over the recovery slot", result.ActiveWorkflow)
	}
	if result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("task running id = %q, want the slot id kept", result.ActiveWorkflow.ID)
	}
}

func TestLaterCodingSuccessReplacesStaleFailureAndRecoverySlot(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app.aiConversationMemory = memory

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	memory.UpsertUnfinishedSlot(owner, &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1", Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})
	result := &ProjectSearchResult{
		ProjectPath: projectPath,
		HasOutput:   true,
		Tags:        []string{"coding_dev", "working_dir:" + workDir},
	}
	failedSnapshot := map[string]*workflow.WorkflowState{
		owner: {UserID: owner, Status: "failed", ProjectPath: projectPath},
	}
	ledger := map[string]string{
		normalizeCodingRuntimeRef(workDir): string(codingruntime.TaskCompleted),
	}

	app.applyTaskListWorkflow(result, projectPath, failedSnapshot, ledger, nil, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != string(codingruntime.TaskCompleted) {
		t.Fatalf("task status = %#v, want the later completed run over the failed snapshot and the recovery slot", result.ActiveWorkflow)
	}

	// No later success: the recovery slot is still the row.
	empty := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true}
	app.applyTaskListWorkflow(empty, projectPath, nil, nil, nil, nil, nil)
	if empty.ActiveWorkflow == nil || empty.ActiveWorkflow.Status != "interrupted" {
		t.Fatalf("recovery status = %#v, want the unfinished slot when the ledger has no later success", empty.ActiveWorkflow)
	}
}

func TestTaskListKeepsTheNewerOfLedgerAndRecoverySlot(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app.aiConversationMemory = memory

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	memory.UpsertUnfinishedSlot(owner, &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1", Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})
	slot := memory.GetUnfinishedSlot(owner)
	if slot == nil || slot.UpdatedAt.IsZero() {
		t.Fatal("recovery slot has no time")
	}
	ref := normalizeCodingRuntimeRef(workDir)
	tags := []string{"coding_dev", "working_dir:" + workDir}
	ledger := map[string]string{ref: string(codingruntime.TaskCompleted)}

	// The completion is older than the slot. The slot is the attempt that
	// stopped after that run, and it has no ledger row of its own.
	older := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	app.applyTaskListWorkflow(older, projectPath, nil, ledger, map[string]time.Time{ref: slot.UpdatedAt.Add(-time.Hour)}, nil, nil)
	if older.ActiveWorkflow == nil || older.ActiveWorkflow.Status != "interrupted" {
		t.Fatalf("older completion hid the later interrupt: %#v", older.ActiveWorkflow)
	}

	// The completion is newer than the slot. The leftover interrupt is stale.
	newer := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	app.applyTaskListWorkflow(newer, projectPath, nil, ledger, map[string]time.Time{ref: slot.UpdatedAt.Add(time.Hour)}, nil, nil)
	if newer.ActiveWorkflow == nil || newer.ActiveWorkflow.Status != string(codingruntime.TaskCompleted) {
		t.Fatalf("later completion stayed behind the stale slot: %#v", newer.ActiveWorkflow)
	}

	// A workflow failure recorded after that completion is the row.
	failedAt := slot.UpdatedAt.Add(2 * time.Hour)
	failed := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	failedSnapshot := map[string]*workflow.WorkflowState{
		owner: {UserID: owner, Status: "failed", ProjectPath: projectPath, UpdatedAt: failedAt},
	}
	app.applyTaskListWorkflow(failed, projectPath, failedSnapshot, ledger, map[string]time.Time{ref: slot.UpdatedAt.Add(time.Hour)}, nil, nil)
	if failed.ActiveWorkflow == nil || failed.ActiveWorkflow.Status != "failed" {
		t.Fatalf("later failure was replaced by the older completion: %#v", failed.ActiveWorkflow)
	}
}

func TestLaterLedgerReplacesInterruptedExecutionPhase(t *testing.T) {
	app := newProjectSearchTestApp(t)
	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	ref := normalizeCodingRuntimeRef(workDir)
	tags := []string{"coding_dev", "working_dir:" + workDir}
	snapshotAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	// Suspended on the execution phase. The phase id contains "execute", so
	// the snapshot used to look live and hide every later ledger row.
	interrupted := map[string]*workflow.WorkflowState{
		owner: {
			UserID: owner, Status: "active", Suspended: true, ProjectPath: projectPath,
			UpdatedAt: snapshotAt, CurrentPhase: 0,
			Phases: []workflow.Phase{{ID: "execution"}},
		},
	}
	row := func() *ProjectSearchResult {
		return &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	}

	older := row()
	app.applyTaskListWorkflow(older, projectPath, interrupted, map[string]string{ref: string(codingruntime.TaskCompleted)}, map[string]time.Time{ref: snapshotAt.Add(-time.Hour)}, nil, nil)
	if older.ActiveWorkflow == nil || older.ActiveWorkflow.Status != "interrupted" || older.ActiveWorkflow.Phase != "execution" {
		t.Fatalf("older completion replaced the later interrupt: %#v", older.ActiveWorkflow)
	}

	undated := row()
	app.applyTaskListWorkflow(undated, projectPath, interrupted, map[string]string{ref: string(codingruntime.TaskCompleted)}, nil, nil, nil)
	if undated.ActiveWorkflow == nil || undated.ActiveWorkflow.Status != "interrupted" {
		t.Fatalf("undated completion replaced the execution-phase interrupt: %#v", undated.ActiveWorkflow)
	}

	newer := row()
	app.applyTaskListWorkflow(newer, projectPath, interrupted, map[string]string{ref: string(codingruntime.TaskCompleted)}, map[string]time.Time{ref: snapshotAt.Add(time.Hour)}, nil, nil)
	if newer.ActiveWorkflow == nil || newer.ActiveWorkflow.Status != string(codingruntime.TaskCompleted) {
		t.Fatalf("later completion stayed behind the execution-phase interrupt: %#v", newer.ActiveWorkflow)
	}

	// The open run's clock is its start. A later interrupt snapshot must not
	// hide it. A queued row uses the same displayed status and is not live.
	live := row()
	app.applyTaskListWorkflow(live, projectPath, interrupted, map[string]string{ref: string(codingruntime.TaskRunning)}, map[string]time.Time{ref: snapshotAt.Add(-time.Hour)}, nil, nil)
	if live.ActiveWorkflow == nil || live.ActiveWorkflow.Status != string(codingruntime.TaskRunning) {
		t.Fatalf("open run stayed behind the interrupt: %#v", live.ActiveWorkflow)
	}
	queued := row()
	app.applyTaskListWorkflow(queued, projectPath, interrupted, map[string]string{ref: string(codingruntime.TaskQueued)}, map[string]time.Time{ref: snapshotAt.Add(-time.Hour)}, nil, nil)
	if queued.ActiveWorkflow == nil || queued.ActiveWorkflow.Status != "interrupted" {
		t.Fatalf("queued row hid the later interrupt: %#v", queued.ActiveWorkflow)
	}

	// A workflow that is actually active keeps its phase over a later ledger.
	active := row()
	activeSnapshot := map[string]*workflow.WorkflowState{
		owner: {
			UserID: owner, Status: "active", ProjectPath: projectPath,
			UpdatedAt: snapshotAt, CurrentPhase: 0,
			Phases: []workflow.Phase{{ID: "review"}},
		},
	}
	app.applyTaskListWorkflow(active, projectPath, activeSnapshot, map[string]string{ref: string(codingruntime.TaskCompleted)}, map[string]time.Time{ref: snapshotAt.Add(time.Hour)}, nil, nil)
	if active.ActiveWorkflow == nil || active.ActiveWorkflow.Status != "active" || active.ActiveWorkflow.Phase != "review" {
		t.Fatalf("active workflow lost its phase to the ledger: %#v", active.ActiveWorkflow)
	}
}

func TestStaleTabWithRecoveryWorkIsRetained(t *testing.T) {
	if !tabSessionHasRecoveryWork(&TabSessionData{Conversation: []interface{}{
		map[string]interface{}{"role": "assistant", "resultStatus": "incomplete"},
	}}) {
		t.Fatal("incomplete tab was treated as disposable")
	}
	if tabSessionHasRecoveryWork(&TabSessionData{Conversation: []interface{}{
		map[string]interface{}{"role": "assistant", "resultStatus": "completed"},
	}}) {
		t.Fatal("completed tab was retained as unfinished")
	}
}

func TestCodingWorkbenchTTLRetentionState(t *testing.T) {
	if !codingWorkbenchHasUnfinishedWork(stickyCodingWorkbenchMemory{
		StepStatuses: []codingWorkbenchStepStatus{{Status: "passed"}, {Status: "pending"}},
	}) {
		t.Fatal("pending coding plan was treated as disposable")
	}
	if codingWorkbenchHasUnfinishedWork(stickyCodingWorkbenchMemory{
		StepStatuses: []codingWorkbenchStepStatus{{Status: "passed"}, {Status: "skipped"}},
	}) {
		t.Fatal("completed coding plan was treated as unfinished")
	}
}
