package guiapp

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestTaskOrchestrator2MigratesPayloadDatabaseToRows(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plans.json")
	dbPath := taskOrchestratorDBPath(planPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE task_plans (id TEXT PRIMARY KEY, payload_json TEXT NOT NULL, updated_at INTEGER NOT NULL);
		CREATE TABLE task_plans_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().Add(-time.Hour)
	legacy := TaskPlan{
		ID: "legacy-plan", Description: "payload plan", Status: orchestratorTaskStatusRunning,
		RuntimeStatus: "running", CreatedAt: createdAt,
		SubTasks: []PlanSubTask{
			{ID: "a", Description: "first", Status: orchestratorTaskStatusCompleted},
			{ID: "b", Description: "second", DependsOn: []string{"a"}, Result: "waiting"},
		},
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO task_plans(id, payload_json, updated_at) VALUES(?,?,?)`, legacy.ID, payload, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	o := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	defer o.Close()
	got, err := o.GetStatus(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != legacy.Description || len(got.SubTasks) != 2 || len(got.SubTasks[1].DependsOn) != 1 || got.SubTasks[1].DependsOn[0] != "a" {
		t.Fatalf("migrated plan = %#v", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt, createdAt)
	}
	var payloadColumns int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('task_plans') WHERE name='payload_json'`).Scan(&payloadColumns); err != nil {
		t.Fatal(err)
	}
	if payloadColumns != 0 {
		t.Fatal("payload_json column remains after migration")
	}
	if _, err := os.Stat(planPath); !os.IsNotExist(err) {
		t.Fatalf("migration created a JSON plan file: %v", err)
	}
}

func TestTaskOrchestrator2ImportsLegacyJSONIntoRows(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plans.json")
	legacy := map[string]*TaskPlan{
		"json-plan": {
			ID: "json-plan", Description: "JSON plan", Status: orchestratorTaskStatusPending, CreatedAt: time.Now(),
			SubTasks: []PlanSubTask{{ID: "a", Description: "first"}, {ID: "b", Description: "second", DependsOn: []string{"a"}}},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	o := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	defer o.Close()
	got, err := o.GetStatus("json-plan")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "JSON plan" || len(got.SubTasks) != 2 || len(got.SubTasks[1].DependsOn) != 1 {
		t.Fatalf("imported plan = %#v", got)
	}
	var subtasks int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM task_plan_subtasks WHERE plan_id='json-plan'`).Scan(&subtasks); err != nil {
		t.Fatal(err)
	}
	if subtasks != 2 {
		t.Fatalf("subtask rows = %d", subtasks)
	}
	if _, err := os.Stat(planPath + ".migrated.bak"); err != nil {
		t.Fatalf("legacy JSON backup missing: %v", err)
	}
}

func TestTaskOrchestrator2IncrementalPersistenceAndSignatureBackfill(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plans.json")
	o := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	t.Cleanup(o.Close)
	first, err := o.CreatePlan("first", []PlanSubTask{{ID: "a", Description: "first step"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := o.CreatePlan("second", []PlanSubTask{{ID: "b", Description: "second step"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`
CREATE TABLE task_plan_write_audit(changes INTEGER NOT NULL);
INSERT INTO task_plan_write_audit VALUES(0);
CREATE TRIGGER task_plan_audit_insert AFTER INSERT ON task_plans BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER task_plan_audit_update AFTER UPDATE ON task_plans BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER task_plan_audit_delete AFTER DELETE ON task_plans BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER subtask_audit_insert AFTER INSERT ON task_plan_subtasks BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER subtask_audit_update AFTER UPDATE ON task_plan_subtasks BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER subtask_audit_delete AFTER DELETE ON task_plan_subtasks BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER dependency_audit_insert AFTER INSERT ON task_plan_dependencies BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER dependency_audit_update AFTER UPDATE ON task_plan_dependencies BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
CREATE TRIGGER dependency_audit_delete AFTER DELETE ON task_plan_dependencies BEGIN UPDATE task_plan_write_audit SET changes=changes+1; END;
`); err != nil {
		t.Fatal(err)
	}
	changes := func() int {
		var count int
		if err := o.db.QueryRow(`SELECT changes FROM task_plan_write_audit`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := o.savePlans(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 0 {
		t.Fatalf("unchanged plans rewrote %d rows", got)
	}
	o.mu.Lock()
	first.Status = orchestratorTaskStatusCompleted
	first.SubTasks[0].Status = orchestratorTaskStatusCompleted
	o.mu.Unlock()
	if err := o.savePlans(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 3 {
		var storedSignature string
		_ = o.db.QueryRow(`SELECT signature FROM task_plans WHERE id=?`, first.ID).Scan(&storedSignature)
		currentSignature, _ := taskPlanSignature(first)
		t.Fatalf("one changed plan wrote %d rows, want plan update plus one subtask delete+insert; stored=%s current=%s plan=%#v", got, storedSignature, currentSignature, first)
	}
	if err := o.savePlans(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 3 {
		t.Fatalf("second unchanged save wrote rows: %d", got)
	}
	o.Close()

	reopened := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	if _, err := reopened.db.Exec(`ALTER TABLE task_plans DROP COLUMN signature`); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	backfilled := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	defer backfilled.Close()
	for _, id := range []string{first.ID, second.ID} {
		var signature string
		if err := backfilled.db.QueryRow(`SELECT signature FROM task_plans WHERE id=?`, id).Scan(&signature); err != nil {
			t.Fatal(err)
		}
		if signature == "" {
			t.Fatalf("plan %s signature was not backfilled", id)
		}
	}
	got, err := backfilled.GetStatus(first.ID)
	if err != nil || got.Status != orchestratorTaskStatusCompleted {
		t.Fatalf("backfilled plan = %#v, %v", got, err)
	}
}

func TestTaskOrchestrator2CreatePlanIDsStayUniqueOnWindowsClockTicks(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	seen := make(map[string]struct{}, 200)
	for index := 0; index < 200; index++ {
		plan, err := o.CreatePlan("rapid create", []PlanSubTask{{Description: "step"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, duplicate := seen[plan.ID]; duplicate {
			t.Fatalf("duplicate plan id %q at iteration %d", plan.ID, index)
		}
		seen[plan.ID] = struct{}{}
	}
	if len(o.plans) != len(seen) {
		t.Fatalf("plans were overwritten: map=%d unique=%d", len(o.plans), len(seen))
	}
}
