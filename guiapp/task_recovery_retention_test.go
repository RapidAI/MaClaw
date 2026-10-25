package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestTaskListUsesPersistedAgentRecovery(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app := &App{aiConversationMemory: memory}
	result := &ProjectSearchResult{ProjectPath: "D:/work/task", HasOutput: true}
	memory.UpsertUnfinishedSlot(projectSessionOwnerID(result.ProjectPath), &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1", Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})

	app.applyTaskListWorkflow(result, result.ProjectPath, nil, nil, nil, nil)

	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "interrupted" || result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("task recovery status = %#v", result.ActiveWorkflow)
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
