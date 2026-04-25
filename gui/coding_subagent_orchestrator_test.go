package main

import (
	"strings"
	"testing"
)

func TestShouldUseSubAgent_NilOrchestrator(t *testing.T) {
	if ShouldUseSubAgent(nil) {
		t.Error("nil orchestrator should return false")
	}
}

func TestShouldUseSubAgent_InactiveOrchestrator(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	if ShouldUseSubAgent(o) {
		t.Error("inactive orchestrator should return false")
	}
}

func TestShouldUseSubAgent_ActiveDirectMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	// ExternalChecker is nil → always resolves to direct mode.
	o.Activate([]*TaskItem{
		{Index: 1, Title: "test task", Status: TaskExecPending},
	}, "req", "design", "/project", "")

	if !ShouldUseSubAgent(o) {
		t.Error("active orchestrator with direct mode should return true")
	}
}

func TestShouldUseSubAgent_ActiveExternalMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	// Provide an ExternalChecker that says external tool is available.
	o.ExternalChecker = &mockExternalChecker{available: true}
	o.Activate([]*TaskItem{
		{Index: 1, Title: "test task", Status: TaskExecPending},
	}, "req", "design", "/project", "claude")

	if ShouldUseSubAgent(o) {
		t.Error("active orchestrator with external mode should return false")
	}
}

type mockExternalChecker struct {
	available bool
}

func (m *mockExternalChecker) IsExternalToolAvailable(toolName, projectPath string) bool {
	return m.available
}

func TestCollectPreviousOutputs(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 1, Title: "Player", Files: []string{"src/player.h", "src/player.cpp"}},
		{Index: 2, Title: "Level", Files: []string{"src/level.h"}},
		{Index: 3, Title: "Game", Files: []string{"src/game.h"}},
	}, "", "", "/project", "")

	// Set statuses after activation (Activate resets all to Pending).
	o.Tasks[0].Status = TaskExecPassed
	o.Tasks[1].Status = TaskExecPassed

	// Task 0 has ActualFiles (SubAgent tracked real modifications).
	o.Tasks[0].ActualFiles = []string{"src/player.h", "src/player.cpp", "CMakeLists.txt"}

	runner := &SubAgentTaskRunner{orchestrator: o}
	outputs := runner.collectPreviousOutputs()

	// Task 0: 3 actual files (prefers ActualFiles over Files).
	// Task 1: 1 declared file (no ActualFiles, falls back to Files).
	// Task 2: pending, not included.
	if len(outputs) != 4 {
		t.Fatalf("expected 4 outputs (3 actual + 1 declared), got %d: %v", len(outputs), outputs)
	}
	// CMakeLists.txt should be present (from ActualFiles, not in original Files).
	found := false
	for _, out := range outputs {
		if strings.Contains(out, "CMakeLists.txt") {
			found = true
		}
	}
	if !found {
		t.Error("ActualFiles should include CMakeLists.txt")
	}
}
