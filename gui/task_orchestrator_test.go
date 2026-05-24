package main

import (
	"testing"
)

func TestTaskOrchestrator2_CreatePlan(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, err := o.CreatePlan("test plan", []PlanSubTask{
		{Description: "task 1", Tool: "claude"},
		{Description: "task 2", Tool: "codex"},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == "" {
		t.Error("plan ID is empty")
	}
	if plan.Status != "planning" {
		t.Errorf("status = %s, want planning", plan.Status)
	}
	if len(plan.SubTasks) != 2 {
		t.Errorf("subtasks len = %d, want 2", len(plan.SubTasks))
	}
	// Auto-assigned IDs
	if plan.SubTasks[0].ID != "task_1" {
		t.Errorf("subtask[0].ID = %s, want task_1", plan.SubTasks[0].ID)
	}
	if plan.SubTasks[1].ID != "task_2" {
		t.Errorf("subtask[1].ID = %s, want task_2", plan.SubTasks[1].ID)
	}
}

func TestTaskOrchestrator2_CreatePlan_Empty(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	_, err := o.CreatePlan("empty", nil)
	if err == nil {
		t.Error("expected error for empty subtasks")
	}
}

func TestTaskOrchestrator2_GetStatus(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("test", []PlanSubTask{{Description: "t1"}})
	got, err := o.GetStatus(plan.ID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if got.ID != plan.ID {
		t.Errorf("ID = %s, want %s", got.ID, plan.ID)
	}
}

func TestTaskOrchestrator2_GetStatus_NotFound(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	_, err := o.GetStatus("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

func TestTaskOrchestrator2_Cancel(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("test", []PlanSubTask{{Description: "t1"}})
	if err := o.Cancel(plan.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, _ := o.GetStatus(plan.ID)
	if got.Status != "cancelled" {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
}

func TestTaskOrchestrator2_Cancel_NotFound(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	err := o.Cancel("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

func TestTaskOrchestrator2_Execute_NoDeps(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("queued test", []PlanSubTask{
		{Description: "task A", Tool: "claude"},
		{Description: "task B", Tool: "codex"},
	})
	// Execute with nil manager — subtasks will get "session manager not available" error
	err := o.Execute(plan.ID)
	if err == nil {
		t.Error("expected error with nil manager")
	}
}

func TestTaskOrchestrator2_Execute_NotFound(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	err := o.Execute("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

func TestTaskOrchestrator2_Execute_BlockedDependencyFailsFast(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("blocked deps", []PlanSubTask{
		{ID: "b", Description: "blocked", DependsOn: []string{"missing"}},
	})

	err := o.Execute(plan.ID)
	if err == nil {
		t.Fatal("expected blocked dependency error")
	}
	if got, _ := o.GetStatus(plan.ID); got.Status != orchestratorTaskStatusFailed {
		t.Fatalf("plan status = %s, want failed", got.Status)
	} else if got.SubTasks[0].Status != orchestratorTaskStatusFailed || got.SubTasks[0].Result == "" {
		t.Fatalf("blocked subtask should be marked failed with reason, got %#v", got.SubTasks[0])
	}
}

func TestTaskOrchestrator2_SubTaskDependencies(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("deps test", []PlanSubTask{
		{ID: "a", Description: "first"},
		{ID: "b", Description: "second", DependsOn: []string{"a"}},
	})
	if len(plan.SubTasks[1].DependsOn) != 1 || plan.SubTasks[1].DependsOn[0] != "a" {
		t.Error("dependency not preserved")
	}
}
