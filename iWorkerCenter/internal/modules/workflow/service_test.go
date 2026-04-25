package workflow

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	colleagueDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	roleDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

const testTenantID = "tenant-test-001"

func setupTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func seedRolesAndColleagues(t *testing.T, p *db.Provider) {
	t.Helper()
	rr := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = rr.Insert(testTenantID, &roleDomain.Role{
		ID: "role-quality", Name: "Quality", Code: "quality",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = rr.Insert(testTenantID, &roleDomain.Role{
		ID: "role-production", Name: "Production", Code: "production",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = rr.Insert(testTenantID, &roleDomain.Role{
		ID: "role-office", Name: "Office", Code: "office",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})

	cr := colleagueRepo.New(p.Write, p.Read)
	_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-xiaozhou", Name: "Xiaozhou", RoleID: "role-quality",
		Strengths: []string{}, Tasks: []string{}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-laochen", Name: "Laochen", RoleID: "role-production",
		Strengths: []string{}, Tasks: []string{}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-xiaodi", Name: "Xiaodi", RoleID: "role-office",
		Strengths: []string{}, Tasks: []string{}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	})
}

func newTestService(t *testing.T, p *db.Provider) *Service {
	t.Helper()
	wfRepo := NewRepo(p.Write, p.Read)
	collabRepo := collaboration.NewRepo(p.Write, p.Read)
	colRepo := colleagueRepo.New(p.Write, p.Read)
	return NewService(wfRepo, p, collabRepo, colRepo)
}

func TestCreateDefinition_Atomic(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Quality loop",
		Steps: []CreateStepDefRequest{
			{StepName: "Issue analysis", AssigneeRoleCode: "quality"},
			{StepName: "Fix plan", AssigneeRoleCode: "production"},
			{StepName: "Archive notice", AssigneeRoleCode: "office"},
		},
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if def.Status != DefStatusDraft {
		t.Errorf("expected draft, got %s", def.Status)
	}

	steps, err := svc.ListStepDefinitions(testTenantID, def.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].AssigneeRoleCode != "quality" {
		t.Errorf("step 0 role: expected quality, got %s", steps[0].AssigneeRoleCode)
	}
}

func TestResolveAssignee_UsesLeastLoadedColleague(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(t, p)
	rr := roleRepo.New(p.Write, p.Read)
	cr := colleagueRepo.New(p.Write, p.Read)
	now := time.Now()

	_ = rr.Insert(testTenantID, &roleDomain.Role{
		ID: "role-shared", Name: "Shared Delivery", Code: "shared-delivery",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-shared-a", Name: "Alpha", RoleID: "role-shared",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-shared-b", Name: "Beta", RoleID: "role-shared",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	if err := svc.collabRepo.InsertTask(testTenantID, &collaboration.Task{
		ID:              "task-busy-a",
		Title:           "busy-a",
		FromColleagueID: "col-shared-b",
		ToColleagueID:   "col-shared-a",
		ToRoleCode:      "shared-delivery",
		Status:          collaboration.StatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("seed collaboration task: %v", err)
	}

	assignee, err := svc.resolveAssignee(testTenantID, &StepDefinition{StepCode: "shared", AssigneeRoleCode: "shared-delivery"})
	if err != nil {
		t.Fatalf("resolve assignee: %v", err)
	}
	if assignee != "col-shared-b" {
		t.Fatalf("assignee = %q, want col-shared-b", assignee)
	}
}

func TestWorkflowLifecycle_FullRun(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Quality loop",
		Steps: []CreateStepDefRequest{
			{StepName: "Issue analysis", AssigneeRoleCode: "quality"},
			{StepName: "Fix plan", AssigneeRoleCode: "production"},
			{StepName: "Archive notice", AssigneeRoleCode: "office"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}

	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{
		DefinitionID: def.ID,
		Title:        "Quality loop #1",
		InitiatorID:  "col-xiaozhou",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if inst.Status != InstStatusRunning {
		t.Errorf("expected running, got %s", inst.Status)
	}

	stepInstances, _ := svc.ListStepInstances(testTenantID, inst.ID)
	if len(stepInstances) != 1 {
		t.Fatalf("expected 1 step instance, got %d", len(stepInstances))
	}
	if stepInstances[0].AssigneeColleagueID != "col-xiaozhou" {
		t.Errorf("step 1 assignee: expected col-xiaozhou, got %s", stepInstances[0].AssigneeColleagueID)
	}

	if err := svc.CompleteStep(testTenantID, stepInstances[0].ID, "col-xiaozhou", "analysis done"); err != nil {
		t.Fatalf("complete step 1: %v", err)
	}

	stepInstances, _ = svc.ListStepInstances(testTenantID, inst.ID)
	if len(stepInstances) != 2 {
		t.Fatalf("expected 2 step instances, got %d", len(stepInstances))
	}
	if stepInstances[1].AssigneeColleagueID != "col-laochen" {
		t.Errorf("step 2 assignee: expected col-laochen, got %s", stepInstances[1].AssigneeColleagueID)
	}

	if err := svc.CompleteStep(testTenantID, stepInstances[1].ID, "col-laochen", "plan done"); err != nil {
		t.Fatalf("complete step 2: %v", err)
	}

	stepInstances, _ = svc.ListStepInstances(testTenantID, inst.ID)
	if len(stepInstances) != 3 {
		t.Fatalf("expected 3 step instances, got %d", len(stepInstances))
	}
	if stepInstances[2].AssigneeColleagueID != "col-xiaodi" {
		t.Errorf("step 3 assignee: expected col-xiaodi, got %s", stepInstances[2].AssigneeColleagueID)
	}

	if err := svc.CompleteStep(testTenantID, stepInstances[2].ID, "col-xiaodi", "archived"); err != nil {
		t.Fatalf("complete step 3: %v", err)
	}

	inst, _ = svc.GetInstance(testTenantID, inst.ID)
	if inst.Status != InstStatusCompleted {
		t.Errorf("expected completed, got %s", inst.Status)
	}

	events, _ := svc.ListEvents(testTenantID, inst.ID)
	if len(events) < 4 {
		t.Errorf("expected at least 4 events, got %d", len(events))
	}
}

func TestWorkflowReject_TerminatesProcess(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, _ := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Reject path",
		Steps: []CreateStepDefRequest{
			{StepName: "Step 1", AssigneeRoleCode: "quality"},
			{StepName: "Step 2", AssigneeRoleCode: "production"},
		},
	})
	_ = svc.PublishDefinition(testTenantID, def.ID)

	inst, _ := svc.StartInstance(testTenantID, StartInstanceRequest{
		DefinitionID: def.ID, InitiatorID: "col-xiaozhou",
	})
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	if err := svc.RejectStep(testTenantID, steps[0].ID, "col-xiaozhou", "invalid input"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	inst, _ = svc.GetInstance(testTenantID, inst.ID)
	if inst.Status != InstStatusRejected {
		t.Errorf("expected rejected, got %s", inst.Status)
	}

	if err := svc.CompleteStep(testTenantID, steps[0].ID, "col-xiaozhou", ""); err == nil {
		t.Error("expected error completing rejected step")
	}
}

func TestTerminalStepCannotTransition(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, _ := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Single step",
		Steps: []CreateStepDefRequest{
			{StepName: "Only step", AssigneeRoleCode: "quality"},
		},
	})
	_ = svc.PublishDefinition(testTenantID, def.ID)

	inst, _ := svc.StartInstance(testTenantID, StartInstanceRequest{
		DefinitionID: def.ID, InitiatorID: "col-xiaozhou",
	})
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	_ = svc.CompleteStep(testTenantID, steps[0].ID, "col-xiaozhou", "done")

	if err := svc.CompleteStep(testTenantID, steps[0].ID, "col-xiaozhou", "again"); err == nil {
		t.Error("expected error on double-complete")
	}
}

func TestUnpublishedDefinitionCannotStart(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(t, p)

	def, _ := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Draft only",
		Steps: []CreateStepDefRequest{
			{StepName: "Step 1", AssigneeRoleCode: "quality"},
		},
	})

	_, err := svc.StartInstance(testTenantID, StartInstanceRequest{
		DefinitionID: def.ID, InitiatorID: "someone",
	})
	if err == nil {
		t.Error("expected error starting unpublished definition")
	}
}
