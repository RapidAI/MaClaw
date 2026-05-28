package main

import (
	"strings"
	"testing"
)

type fakeExternalChecker struct {
	available bool
}

func (f *fakeExternalChecker) IsExternalToolAvailable(string, string) bool {
	return f.available
}

func TestResolveExecutionMode_NoChecker_ReturnsDirect(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	mode := o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Errorf("expected direct, got %s", mode)
	}
}

func TestResolveExecutionMode_CheckerAvailable_ReturnsDirect(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	mode := o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Errorf("expected direct, got %s", mode)
	}
}

func TestResolveExecutionMode_CheckerUnavailable_ReturnsDirect(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: false}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	mode := o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Errorf("expected direct, got %s", mode)
	}
}

func TestResolveExecutionMode_NoTool_ReturnsDirect(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "")
	mode := o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Errorf("expected direct when tool is empty, got %s", mode)
	}
}

func TestResolveExecutionMode_CachedOnceResolved(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")

	mode := o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Fatalf("expected direct, got %s", mode)
	}

	o.ExternalChecker = &fakeExternalChecker{available: false}
	mode = o.ResolveExecutionMode()
	if mode != TaskExecModeDirect {
		t.Errorf("expected cached direct even after checker changed, got %s", mode)
	}
}

func TestDegradeCurrentToDirectMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	o.ResolveExecutionMode()

	ok := o.DegradeCurrentToDirectMode()
	if ok {
		t.Error("expected degradation to be unnecessary")
	}
	if o.CurrentExecutionMode() != TaskExecModeDirect {
		t.Errorf("expected direct, got %s", o.CurrentExecutionMode())
	}

	ok = o.DegradeCurrentToDirectMode()
	if ok {
		t.Error("expected second degradation to return false")
	}
}

func TestBuildExecutionGuide_DirectMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "T1", Status: TaskExecPending}}, "", "", "/proj", "")
	o.ResolveExecutionMode()

	injection := o.BuildSystemInjection()
	if !strings.Contains(injection, "CodingSubAgent") {
		t.Error("expected CodingSubAgent guide in injection")
	}
	if strings.Contains(injection, "create_session") {
		t.Error("direct mode injection should not mention create_session")
	}
}

func TestBuildExecutionGuide_CodingSubAgentMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1", Status: TaskExecPending}}, "", "", "/proj", "claude")
	o.ResolveExecutionMode()

	injection := o.BuildSystemInjection()
	if !strings.Contains(injection, "CodingSubAgent") {
		t.Error("expected CodingSubAgent in execution guide")
	}
	if strings.Contains(injection, "create_session") {
		t.Error("execution guide should not mention create_session")
	}
}

func TestDirectModeSessionBlocklist(t *testing.T) {
	sessionTools := []string{
		"create_session", "send_and_observe", "control_session",
		"get_session_output", "get_session_events",
		"interrupt_session", "kill_session", "list_sessions",
	}
	for _, tool := range sessionTools {
		if !isDirectModeBlockedTool(tool) {
			t.Errorf("expected %s to be blocked in direct mode", tool)
		}
	}

	codingTools := []string{"bash", "write_file", "edit_file", "read_file", "list_directory"}
	for _, tool := range codingTools {
		if isDirectModeBlockedTool(tool) {
			t.Errorf("expected %s to NOT be blocked in direct mode", tool)
		}
	}
}

func TestDirectModeSessionBlocklist_CodingToolsPreserved(t *testing.T) {
	directCodingTools := []string{"bash", "write_file", "edit_file", "read_file", "list_directory", "craft_tool", "memory", "web_search"}
	for _, name := range directCodingTools {
		if isDirectModeBlockedTool(name) {
			t.Errorf("%s must NOT be in directModeSessionBlocklist", name)
		}
	}
}

func TestPhaseGateAndDirectMode_NoOverlap(t *testing.T) {
	criticalTools := []string{"bash", "write_file", "edit_file"}
	for _, name := range criticalTools {
		inPhaseGate := codingToolBlocklist[name]
		inDirectBlock := directModeSessionBlocklist[name]
		if !inPhaseGate {
			t.Errorf("%s should be in codingToolBlocklist", name)
		}
		if inDirectBlock {
			t.Errorf("%s must NOT be in directModeSessionBlocklist", name)
		}
	}

	sessionTools := []string{"create_session", "send_and_observe", "control_session"}
	for _, name := range sessionTools {
		if !codingToolBlocklist[name] {
			t.Errorf("%s should be in codingToolBlocklist", name)
		}
		if !directModeSessionBlocklist[name] {
			t.Errorf("%s should be in directModeSessionBlocklist", name)
		}
	}
}

func TestResolveExecutionModeForTaskRunRejectsStaleRun(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	oldTask := &TaskItem{Index: 0, Title: "old"}
	o.Activate([]*TaskItem{oldTask}, "", "", "/proj", "claude")
	task, runID := o.CurrentTaskHandle()

	o.Activate([]*TaskItem{{Index: 0, Title: "new"}}, "", "", "/proj", "claude")
	mode, ok := o.ResolveExecutionModeForTaskRun(task, runID)
	if ok {
		t.Fatalf("expected stale run to be rejected, got mode %s", mode)
	}
	if oldTask.ExecMode != "" {
		t.Fatalf("stale task mode should not be resolved, got %s", oldTask.ExecMode)
	}
}

func TestResolveExecutionModeForTaskRunCachesCurrentTask(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	task, runID := o.CurrentTaskHandle()

	mode, ok := o.ResolveExecutionModeForTaskRun(task, runID)
	if !ok || mode != TaskExecModeDirect {
		t.Fatalf("expected current run direct mode, got %s ok=%v", mode, ok)
	}

	o.ExternalChecker = &fakeExternalChecker{available: false}
	mode, ok = o.ResolveExecutionModeForTaskRun(task, runID)
	if !ok || mode != TaskExecModeDirect {
		t.Fatalf("expected cached direct mode, got %s ok=%v", mode, ok)
	}
}

func TestDegradeTaskToDirectModeForRunTargetsTask(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	tasks := []*TaskItem{
		{Index: 0, Title: "T1", ExecMode: TaskExecModeExternal},
		{Index: 1, Title: "T2", ExecMode: TaskExecModeExternal},
	}
	o.Activate(tasks, "", "", "/proj", "claude")
	tasks[0].ExecMode = TaskExecModeExternal
	tasks[1].ExecMode = TaskExecModeExternal
	runID := o.RunID
	o.CurrentIndex = 1

	if !o.DegradeTaskToDirectModeForRun(tasks[0], runID) {
		t.Fatal("expected degradation for target task")
	}
	if tasks[0].ExecMode != TaskExecModeDirect {
		t.Fatalf("expected target task direct mode, got %s", tasks[0].ExecMode)
	}
	if tasks[1].ExecMode != TaskExecModeExternal {
		t.Fatalf("current task should remain external, got %s", tasks[1].ExecMode)
	}
}

func TestTaskExecutionModeForRunRejectsForeignTask(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "T1", ExecMode: TaskExecModeExternal}}, "", "", "/proj", "claude")
	_, runID := o.CurrentTaskHandle()
	foreign := &TaskItem{Index: 99, Title: "foreign", ExecMode: TaskExecModeExternal}

	mode, ok := o.TaskExecutionModeForRun(foreign, runID)
	if ok {
		t.Fatalf("expected foreign task to be rejected, got mode %s", mode)
	}
	if o.DegradeTaskToDirectModeForRun(foreign, runID) {
		t.Fatal("expected foreign task degradation to be rejected")
	}
}

func TestRunAwareExecutionModeRejectsDeactivatedRun(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.Activate([]*TaskItem{{Index: 0, Title: "T1"}}, "", "", "/proj", "claude")
	task, runID := o.CurrentTaskHandle()
	o.Deactivate()

	if _, ok := o.ResolveExecutionModeForTaskRun(task, runID); ok {
		t.Fatal("expected deactivated run to reject mode resolution")
	}
	if _, ok := o.TaskExecutionModeForRun(task, runID); ok {
		t.Fatal("expected deactivated run to reject mode read")
	}
	if o.DegradeTaskToDirectModeForRun(task, runID) {
		t.Fatal("expected deactivated run to reject degradation")
	}
}
