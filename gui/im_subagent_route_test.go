package main

import "testing"

func TestSubAgentRouteKeepsIncompleteRunsActiveForResume(t *testing.T) {
	if shouldDeactivateSubAgentOrchestratorAfterRun(false, false) {
		t.Fatal("incomplete non-cancelled SubAgent run should remain active for retry/resume")
	}
	if shouldSaveSubAgentWorkflowOutput(false, false, true) {
		t.Fatal("incomplete SubAgent run must not be saved as workflow phase output")
	}
}

func TestSubAgentRouteFinalizesCompletedOrCancelledRuns(t *testing.T) {
	if !shouldDeactivateSubAgentOrchestratorAfterRun(true, false) {
		t.Fatal("completed SubAgent run should deactivate orchestrator")
	}
	if !shouldSaveSubAgentWorkflowOutput(true, false, true) {
		t.Fatal("completed non-cancelled SubAgent run should save workflow output")
	}
	if shouldSaveSubAgentWorkflowOutput(true, false, false) {
		t.Fatal("completed SubAgent run with no passed tasks must not save workflow output")
	}
	if !shouldDeactivateSubAgentOrchestratorAfterRun(false, true) {
		t.Fatal("cancelled SubAgent run should deactivate orchestrator")
	}
	if shouldSaveSubAgentWorkflowOutput(true, true, true) {
		t.Fatal("cancelled SubAgent run must not save workflow output")
	}
}

func TestSubAgentRouteRequiresPassedIntegrationBeforeWorkflowSave(t *testing.T) {
	if !subAgentIntegrationPassed(&CodingSubAgentResult{Status: TaskExecPassed}) {
		t.Fatal("passed integration should allow workflow save")
	}
	if subAgentIntegrationPassed(&CodingSubAgentResult{Status: TaskExecFailed, Error: "build failed"}) {
		t.Fatal("failed integration must block workflow save")
	}
	if subAgentIntegrationPassed(nil) {
		t.Fatal("nil integration result must block workflow save")
	}
}
