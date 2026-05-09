package workflow

import (
	"context"
	"errors"
	"strings"
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

func TestResolveAssignee_PrefersCapabilityResolver(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(t, p)
	seedRolesAndColleagues(t, p)
	svc.SetCapabilityAssigneeResolver(fakeCapabilityResolver{selected: "col-xiaodi", ok: true})

	assignee, err := svc.resolveAssignee(testTenantID, &StepDefinition{StepCode: "finance_check", StepName: "Finance check", StepType: "processing", AssigneeRoleCode: "quality"})
	if err != nil {
		t.Fatalf("resolve assignee: %v", err)
	}
	if assignee != "col-xiaodi" {
		t.Fatalf("assignee = %q, want col-xiaodi", assignee)
	}
}

func TestResolveAssignee_FallsBackToRoleWhenCapabilityMisses(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(t, p)
	seedRolesAndColleagues(t, p)
	svc.SetCapabilityAssigneeResolver(fakeCapabilityResolver{ok: false})

	assignee, err := svc.resolveAssignee(testTenantID, &StepDefinition{StepCode: "issue", StepName: "Issue analysis", AssigneeRoleCode: "quality"})
	if err != nil {
		t.Fatalf("resolve assignee: %v", err)
	}
	if assignee != "col-xiaozhou" {
		t.Fatalf("assignee = %q, want col-xiaozhou", assignee)
	}
}

type fakeCapabilityResolver struct {
	selected string
	ok       bool
	err      error
}

func (f fakeCapabilityResolver) SelectWorkerForTask(context.Context, string, string, string) (string, bool, error) {
	return f.selected, f.ok, f.err
}

type fakeCapabilityMemoryRecorder struct {
	called        bool
	err           error
	tenantID      string
	workerID      string
	orgUnitID     string
	capabilityID  string
	taskTitle     string
	workflowName  string
	status        string
	resultSummary string
}

func (f *fakeCapabilityMemoryRecorder) RecordCapabilityExecutionMemory(_ context.Context, tenantID, workerID, orgUnitID, capabilityID, taskTitle, workflowName, status, resultSummary, _ string) error {
	f.called = true
	f.tenantID = tenantID
	f.workerID = workerID
	f.orgUnitID = orgUnitID
	f.capabilityID = capabilityID
	f.taskTitle = taskTitle
	f.workflowName = workflowName
	f.status = status
	f.resultSummary = resultSummary
	return f.err
}

type fakeCapabilityUsageRecorder struct {
	err                    error
	capabilityID           string
	colleagueID            string
	workflowInstanceID     string
	workflowStepInstanceID string
	status                 string
	resultSummary          string
	errorMessage           string
	latencyMs              int64
	qualityScore           int
	qualityReason          string
	called                 bool
}

func (f *fakeCapabilityUsageRecorder) RecordCapabilityUsage(_ context.Context, _, capabilityID, colleagueID, workflowInstanceID, workflowStepInstanceID, status, resultSummary, errorMessage string, latencyMs int64, qualityScore int, qualityReason string) error {
	f.called = true
	f.capabilityID = capabilityID
	f.colleagueID = colleagueID
	f.workflowInstanceID = workflowInstanceID
	f.workflowStepInstanceID = workflowStepInstanceID
	f.status = status
	f.resultSummary = resultSummary
	f.errorMessage = errorMessage
	f.latencyMs = latencyMs
	f.qualityScore = qualityScore
	f.qualityReason = qualityReason
	return f.err
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
	tasks, err := svc.collabRepo.ListAll(testTenantID)
	if err != nil {
		t.Fatalf("list collaboration tasks: %v", err)
	}
	if len(tasks) != 1 || !strings.Contains(tasks[0].Description, "Workflow step is ready for processing: Issue analysis") {
		t.Fatalf("first collaboration description = %+v", tasks)
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
	tasks, err = svc.collabRepo.ListAll(testTenantID)
	if err != nil {
		t.Fatalf("list collaboration tasks after step 1: %v", err)
	}
	if len(tasks) != 2 || !strings.Contains(tasks[0].Description+tasks[1].Description, "Workflow step is ready for processing: Fix plan") {
		t.Fatalf("next collaboration description = %+v", tasks)
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

func TestCompleteStepRecordsCapabilityUsageFeedback(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	recorder := &fakeCapabilityUsageRecorder{}
	memoryRecorder := &fakeCapabilityMemoryRecorder{}
	svc.SetCapabilityUsageRecorder(recorder)
	svc.SetCapabilityExecutionMemoryRecorder(memoryRecorder)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Capability feedback",
		Steps: []CreateStepDefRequest{{StepName: "Revenue check", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	err = svc.CompleteStepWithInput(testTenantID, steps[0].ID, CompleteStepInput{
		ActorID:             "col-xiaozhou",
		Result:              "forecast ready",
		CapabilityID:        "cap-revenue",
		CapabilityStatus:    "ok",
		CapabilityLatencyMs: 1234,
		QualityScore:        92,
		QualityReason:       "passed validation",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !recorder.called {
		t.Fatalf("expected capability usage recorder to be called")
	}
	if recorder.capabilityID != "cap-revenue" || recorder.colleagueID != "col-xiaozhou" || recorder.status != "success" || recorder.resultSummary != "forecast ready" || recorder.latencyMs != 1234 || recorder.qualityScore != 92 || recorder.qualityReason != "passed validation" {
		t.Fatalf("unexpected recorder payload: %+v", recorder)
	}
	if recorder.workflowInstanceID != inst.ID || recorder.workflowStepInstanceID != steps[0].ID {
		t.Fatalf("workflow refs = %q/%q, want %q/%q", recorder.workflowInstanceID, recorder.workflowStepInstanceID, inst.ID, steps[0].ID)
	}
	if !memoryRecorder.called {
		t.Fatalf("expected capability execution memory recorder to be called")
	}
	if memoryRecorder.workerID != "col-xiaozhou" || memoryRecorder.orgUnitID != "quality" || memoryRecorder.capabilityID != "cap-revenue" || memoryRecorder.status != "success" {
		t.Fatalf("unexpected memory recorder payload: %+v", memoryRecorder)
	}
}

func TestCompleteStepReturnsCapabilityRecorderErrors(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	recorder := &fakeCapabilityUsageRecorder{err: errors.New("usage store unavailable")}
	memoryRecorder := &fakeCapabilityMemoryRecorder{err: errors.New("memory store unavailable")}
	svc.SetCapabilityUsageRecorder(recorder)
	svc.SetCapabilityExecutionMemoryRecorder(memoryRecorder)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Capability feedback failure",
		Steps: []CreateStepDefRequest{{StepName: "Revenue check", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	err = svc.CompleteStepWithInput(testTenantID, steps[0].ID, CompleteStepInput{
		ActorID:      "col-xiaozhou",
		Result:       "forecast ready",
		CapabilityID: "cap-revenue",
	})
	if err == nil {
		t.Fatal("expected recorder errors to be returned")
	}
	errText := err.Error()
	if !strings.Contains(errText, "record capability usage") || !strings.Contains(errText, "record capability execution memory") {
		t.Fatalf("error = %q, want both recorder failures", errText)
	}
	if !recorder.called || !memoryRecorder.called {
		t.Fatalf("recorders called usage=%t memory=%t", recorder.called, memoryRecorder.called)
	}
}

func TestCompleteStepRejectsMissingActorID(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Missing actor",
		Steps: []CreateStepDefRequest{{StepName: "Revenue check", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	if err := svc.CompleteStepWithInput(testTenantID, steps[0].ID, CompleteStepInput{ActorID: " ", Result: "done"}); err == nil {
		t.Fatal("expected missing actor_id to fail")
	}
	unchanged, err := svc.GetStepInstance(testTenantID, steps[0].ID)
	if err != nil {
		t.Fatalf("get step: %v", err)
	}
	if unchanged.Status != StepPending {
		t.Fatalf("step status = %s, want pending", unchanged.Status)
	}
}

func TestWorkflowStepActionsRequireAssignedActor(t *testing.T) {
	tests := []struct {
		name   string
		action func(svc *Service, stepID string) error
	}{
		{
			name: "resume",
			action: func(svc *Service, stepID string) error {
				return svc.StartOrResumeStep(testTenantID, stepID, "col-laochen", "not my step")
			},
		},
		{
			name: "complete",
			action: func(svc *Service, stepID string) error {
				return svc.CompleteStep(testTenantID, stepID, "col-laochen", "not my result")
			},
		},
		{
			name: "reject",
			action: func(svc *Service, stepID string) error {
				return svc.RejectStep(testTenantID, stepID, "col-laochen", "not my rejection")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := setupTestDB(t)
			seedRolesAndColleagues(t, p)
			svc := newTestService(t, p)

			def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
				Name:  "Assigned actor only",
				Steps: []CreateStepDefRequest{{StepName: "Quality review", AssigneeMode: "fixed_colleague", AssigneeColleagueID: "col-xiaozhou", AssigneeRoleCode: "quality"}},
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
				t.Fatalf("publish: %v", err)
			}
			inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-laochen"})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			steps, err := svc.ListStepInstances(testTenantID, inst.ID)
			if err != nil || len(steps) != 1 {
				t.Fatalf("steps = %+v err=%v", steps, err)
			}

			err = tt.action(svc, steps[0].ID)
			if !errors.Is(err, ErrStepActorForbidden) {
				t.Fatalf("action error = %v, want assigned actor failure", err)
			}
			unchangedStep, err := svc.GetStepInstance(testTenantID, steps[0].ID)
			if err != nil {
				t.Fatalf("get step: %v", err)
			}
			if unchangedStep.Status != StepPending || unchangedStep.Result != "" {
				t.Fatalf("step after unauthorized action = %+v, want pending without result", unchangedStep)
			}
			unchangedInst, err := svc.GetInstance(testTenantID, inst.ID)
			if err != nil {
				t.Fatalf("get instance: %v", err)
			}
			if unchangedInst.Status != InstStatusRunning || unchangedInst.CurrentStepID != steps[0].ID {
				t.Fatalf("instance after unauthorized action = %+v", unchangedInst)
			}
		})
	}
}

func TestRejectStepRejectsMissingActorID(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Reject missing actor",
		Steps: []CreateStepDefRequest{{StepName: "Quality review", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)

	if err := svc.RejectStep(testTenantID, steps[0].ID, " ", "invalid input"); err == nil || !strings.Contains(err.Error(), "actor_id is required") {
		t.Fatalf("RejectStep error = %v, want missing actor_id", err)
	}
	unchanged, err := svc.GetStepInstance(testTenantID, steps[0].ID)
	if err != nil {
		t.Fatalf("get step: %v", err)
	}
	if unchanged.Status != StepPending {
		t.Fatalf("step status = %s, want pending", unchanged.Status)
	}
}

func TestWorkflowRepoRejectsCorruptStepTimestamp(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Corrupt timestamp",
		Steps: []CreateStepDefRequest{{StepName: "Revenue check", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)
	if _, err := p.Write.Exec(`UPDATE workflow_step_instances SET updated_at=? WHERE tenant_id=? AND id=?`, "not-rfc3339", testTenantID, steps[0].ID); err != nil {
		t.Fatalf("corrupt timestamp: %v", err)
	}
	if _, err := svc.GetStepInstance(testTenantID, steps[0].ID); err == nil || !strings.Contains(err.Error(), "parse workflow step instance") {
		t.Fatalf("GetStepInstance error = %v, want parse failure", err)
	}
}

func TestStartOrResumeStepSyncsWorkflowAndCollaborationTask(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Resume path", Steps: []CreateStepDefRequest{{StepName: "Recoverable step", AssigneeRoleCode: "quality"}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start instance: %v", err)
	}
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)
	if err := svc.StartOrResumeStep(testTenantID, steps[0].ID, "col-xiaozhou", "goalwatch resume"); err != nil {
		t.Fatalf("start step: %v", err)
	}

	steps, _ = svc.ListStepInstances(testTenantID, inst.ID)
	if steps[0].Status != StepInProgress {
		t.Fatalf("step status = %s", steps[0].Status)
	}
	task, err := svc.collabRepo.GetByID(testTenantID, steps[0].CollaborationTaskID)
	if err != nil {
		t.Fatalf("get collab task: %v", err)
	}
	if task.Status != collaboration.StatusInProgress {
		t.Fatalf("collab task status = %s", task.Status)
	}
	if err := svc.StartOrResumeStep(testTenantID, steps[0].ID, "col-xiaozhou", "still working"); err != nil {
		t.Fatalf("resume step: %v", err)
	}
	events, _ := svc.ListEvents(testTenantID, inst.ID)
	foundStarted := false
	foundResumed := false
	for _, event := range events {
		if event.Event == "step_started" {
			foundStarted = true
		}
		if event.Event == "step_resumed" {
			foundResumed = true
		}
	}
	if !foundStarted || !foundResumed {
		t.Fatalf("events = %+v", events)
	}
}

func TestStartOrResumeStepRejectsTerminalStep(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	def, _ := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Terminal path", Steps: []CreateStepDefRequest{{StepName: "Only", AssigneeRoleCode: "quality"}}})
	_ = svc.PublishDefinition(testTenantID, def.ID)
	inst, _ := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)
	if err := svc.CompleteStep(testTenantID, steps[0].ID, "col-xiaozhou", "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := svc.StartOrResumeStep(testTenantID, steps[0].ID, "col-xiaozhou", "too late"); err == nil {
		t.Fatal("expected terminal step resume to fail")
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

func TestWorkflowRejectFailsWhenCollaborationTaskCannotSync(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	def, _ := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name: "Reject sync failure",
		Steps: []CreateStepDefRequest{
			{StepName: "Step 1", AssigneeRoleCode: "quality"},
		},
	})
	_ = svc.PublishDefinition(testTenantID, def.ID)
	inst, _ := svc.StartInstance(testTenantID, StartInstanceRequest{
		DefinitionID: def.ID, InitiatorID: "col-xiaozhou",
	})
	steps, _ := svc.ListStepInstances(testTenantID, inst.ID)
	if len(steps) != 1 || strings.TrimSpace(steps[0].CollaborationTaskID) == "" {
		t.Fatalf("step missing collaboration task: %+v", steps)
	}
	if _, err := p.Write.Exec(`DELETE FROM collaboration_task_events WHERE tenant_id=? AND task_id=?`, testTenantID, steps[0].CollaborationTaskID); err != nil {
		t.Fatalf("delete collaboration task events: %v", err)
	}
	if _, err := p.Write.Exec(`DELETE FROM collaboration_tasks WHERE tenant_id=? AND id=?`, testTenantID, steps[0].CollaborationTaskID); err != nil {
		t.Fatalf("delete collaboration task: %v", err)
	}

	if err := svc.RejectStep(testTenantID, steps[0].ID, "col-xiaozhou", "invalid input"); err == nil || !strings.Contains(err.Error(), "reject collab task") {
		t.Fatalf("RejectStep error = %v, want collab sync failure", err)
	}
	inst, _ = svc.GetInstance(testTenantID, inst.ID)
	if inst.Status == InstStatusRejected {
		t.Fatalf("workflow instance was rejected despite collaboration sync failure")
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
