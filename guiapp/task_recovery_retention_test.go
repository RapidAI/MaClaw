package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
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

	app.applyTaskListWorkflow(result, result.ProjectPath, nil, nil, nil, nil)

	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "interrupted" || result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("task recovery status = %#v", result.ActiveWorkflow)
	}

	// The task was resumed and its session is executing now. The recovery slot
	// describes the attempt that stopped, so the row reports the live run.
	live := map[string]struct{}{normalizeRecentTaskPathKey(result.ProjectPath): {}}
	app.applyTaskListWorkflow(result, result.ProjectPath, nil, nil, nil, live)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != string(codingruntime.TaskRunning) {
		t.Fatalf("task running status = %#v, want running over the recovery slot", result.ActiveWorkflow)
	}
	if result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("task running id = %q, want the slot id kept", result.ActiveWorkflow.ID)
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
