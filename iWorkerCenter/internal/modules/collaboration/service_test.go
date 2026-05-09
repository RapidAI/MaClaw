package collaboration

import (
	"testing"
	"time"

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

func seedColleagues(t *testing.T, p *db.Provider) {
	t.Helper()
	rolRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = rolRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-test", Name: "Test Role", Code: "test",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = rolRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-delivery", Name: "Delivery", Code: "delivery",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})

	cr := colleagueRepo.New(p.Write, p.Read)
	for _, pair := range []struct {
		id     string
		roleID string
	}{
		{id: "col-a", roleID: "role-test"},
		{id: "col-b", roleID: "role-test"},
		{id: "col-c", roleID: "role-test"},
		{id: "col-d", roleID: "role-delivery"},
	} {
		_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
			ID: pair.id, Name: pair.id, RoleID: pair.roleID,
			Strengths: []string{}, Tasks: []string{},
			Status: "active", CreatedAt: now, UpdatedAt: now,
		})
	}
}

func newTestService(p *db.Provider) *Service {
	colRp := colleagueRepo.New(p.Write, p.Read)
	return NewService(NewRepo(p.Write, p.Read), colRp)
}

type workflowTransitionCall struct {
	action string
	tenant string
	step   string
	actor  string
	value  string
}

type fakeWorkflowTransitioner struct {
	calls []workflowTransitionCall
}

func (f *fakeWorkflowTransitioner) StartOrResumeStep(tenantID string, stepInstanceID, actorID, note string) error {
	f.calls = append(f.calls, workflowTransitionCall{action: "start", tenant: tenantID, step: stepInstanceID, actor: actorID, value: note})
	return nil
}

func (f *fakeWorkflowTransitioner) CompleteStep(tenantID string, stepInstanceID, actorID, result string) error {
	f.calls = append(f.calls, workflowTransitionCall{action: "complete", tenant: tenantID, step: stepInstanceID, actor: actorID, value: result})
	return nil
}

func (f *fakeWorkflowTransitioner) RejectStep(tenantID string, stepInstanceID, actorID, note string) error {
	f.calls = append(f.calls, workflowTransitionCall{action: "reject", tenant: tenantID, step: stepInstanceID, actor: actorID, value: note})
	return nil
}

func TestCreateTask_Validation(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(p)

	_, err := svc.Create(testTenantID, CreateRequest{FromColleagueID: "a", ToColleagueID: "b"})
	if err == nil {
		t.Error("expected error for missing title")
	}

	_, err = svc.Create(testTenantID, CreateRequest{Title: "test", ToColleagueID: "b"})
	if err == nil {
		t.Error("expected error for missing from_colleague_id")
	}

	_, err = svc.Create(testTenantID, CreateRequest{Title: "test", FromColleagueID: "a"})
	if err == nil {
		t.Error("expected error for missing assignee")
	}
}

func TestCreateTask_Success(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	task, err := svc.Create(testTenantID, CreateRequest{
		Title: "weekly summary", FromColleagueID: "col-a", ToColleagueID: "col-b", ToRoleCode: "office",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Status != StatusPending {
		t.Errorf("expected pending, got %s", task.Status)
	}

	events, _ := svc.ListEvents(testTenantID, task.ID)
	if len(events) != 1 || events[0].Event != "created" {
		t.Errorf("expected 1 created event, got %d", len(events))
	}
}

func TestCreateTask_ByRoleRouting(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	task, err := svc.Create(testTenantID, CreateRequest{
		Title: "delivery handoff", FromColleagueID: "col-a", ToRoleCode: "delivery",
	})
	if err != nil {
		t.Fatalf("create by role: %v", err)
	}
	if task.ToColleagueID != "col-d" {
		t.Fatalf("to_colleague_id = %q, want col-d", task.ToColleagueID)
	}
	if task.ToRoleCode != "delivery" {
		t.Fatalf("to_role_code = %q, want delivery", task.ToRoleCode)
	}
}
func TestCreateTask_ByRoleRoutingChoosesLeastLoadedColleague(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-shared", Name: "Shared Ops", Code: "shared-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-load-a", Name: "Alpha", RoleID: "role-shared",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-load-b", Name: "Beta", RoleID: "role-shared",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})

	for i := 0; i < 2; i++ {
		if _, err := svc.Create(testTenantID, CreateRequest{
			Title: "busy", FromColleagueID: "col-a", ToColleagueID: "col-load-a", ToRoleCode: "shared-ops",
		}); err != nil {
			t.Fatalf("seed busy tasks: %v", err)
		}
	}

	task, err := svc.Create(testTenantID, CreateRequest{
		Title: "balanced route", FromColleagueID: "col-a", ToRoleCode: "shared-ops",
	})
	if err != nil {
		t.Fatalf("create balanced route: %v", err)
	}
	if task.ToColleagueID != "col-load-b" {
		t.Fatalf("to_colleague_id = %q, want col-load-b", task.ToColleagueID)
	}
}

func TestCreateTask_ByRoleRoutingRespectsPrimaryFirst(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-priority", Name: "Priority Ops", Code: "priority-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-primary-a", Name: "PrimaryA", RoleID: "role-priority",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{
		ID: "col-primary-b", Name: "PrimaryB", RoleID: "role-priority",
		Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	for i := 0; i < 2; i++ {
		if _, err := svc.Create(testTenantID, CreateRequest{
			Title: "busy primary", FromColleagueID: "col-a", ToColleagueID: "col-primary-a", ToRoleCode: "priority-ops",
		}); err != nil {
			t.Fatalf("seed busy tasks: %v", err)
		}
	}

	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		DefaultStrategy:        StrategyLeastLoaded,
		RoleStrategies:         map[string]string{"priority-ops": StrategyPrimaryFirst},
		PrimaryColleagueByRole: map[string]string{"priority-ops": "col-primary-a"},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	task, err := svc.Create(testTenantID, CreateRequest{
		Title: "primary route", FromColleagueID: "col-a", ToRoleCode: "priority-ops",
	})
	if err != nil {
		t.Fatalf("create primary route: %v", err)
	}
	if task.ToColleagueID != "col-primary-a" {
		t.Fatalf("to_colleague_id = %q, want col-primary-a", task.ToColleagueID)
	}
}
func TestCreateTask_ByRoleRoutingFailsOverFromUnhealthyPrimary(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-failover", Name: "Failover Ops", Code: "failover-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-failover-a", Name: "NodeA", RoleID: "role-failover", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-failover-b", Name: "NodeB", RoleID: "role-failover", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})

	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		DefaultStrategy:         StrategyLeastLoaded,
		RoleStrategies:          map[string]string{"failover-ops": StrategyPrimaryFirst},
		PrimaryColleagueByRole:  map[string]string{"failover-ops": "col-failover-a"},
		RuntimeStateByColleague: map[string]string{"col-failover-a": RuntimeStateUnhealthy, "col-failover-b": RuntimeStateActive},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	task, err := svc.Create(testTenantID, CreateRequest{Title: "fail over", FromColleagueID: "col-a", ToRoleCode: "failover-ops"})
	if err != nil {
		t.Fatalf("create failover task: %v", err)
	}
	if task.ToColleagueID != "col-failover-b" {
		t.Fatalf("to_colleague_id = %q, want col-failover-b", task.ToColleagueID)
	}
}

func TestCreateTask_ByRoleRoutingUsesStandbyOnlyWhenNoActiveRemain(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-standby", Name: "Standby Ops", Code: "standby-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-standby-a", Name: "ActiveNode", RoleID: "role-standby", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-standby-b", Name: "ReserveNode", RoleID: "role-standby", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})

	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		DefaultStrategy:         StrategyLeastLoaded,
		RuntimeStateByColleague: map[string]string{"col-standby-a": RuntimeStateUnhealthy, "col-standby-b": RuntimeStateStandby},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	task, err := svc.Create(testTenantID, CreateRequest{Title: "standby take over", FromColleagueID: "col-a", ToRoleCode: "standby-ops"})
	if err != nil {
		t.Fatalf("create standby task: %v", err)
	}
	if task.ToColleagueID != "col-standby-b" {
		t.Fatalf("to_colleague_id = %q, want col-standby-b", task.ToColleagueID)
	}
}
func TestRecordHeartbeatMarksColleagueHealthy(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	if err := svc.RecordHeartbeat(testTenantID, "col-d", time.Now()); err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}
	settings, err := svc.GetRoutingSettings(testTenantID)
	if err != nil {
		t.Fatalf("get routing settings: %v", err)
	}
	if settings.LastHeartbeatByColleague["col-d"] == "" {
		t.Fatal("expected heartbeat timestamp for col-d")
	}
}

func TestGetRoutingOverviewReportsEffectiveStates(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	now := time.Now().UTC().Truncate(time.Second)
	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		DefaultStrategy:         StrategyLeastLoaded,
		HeartbeatTimeoutSeconds: 30,
		RuntimeStateByColleague: map[string]string{
			"col-a": RuntimeStateStandby,
			"col-b": RuntimeStateUnhealthy,
		},
		LastHeartbeatByColleague: map[string]string{
			"col-c": now.Add(-2 * time.Minute).Format(time.RFC3339),
			"col-d": now.Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	overview, err := svc.GetRoutingOverview(testTenantID)
	if err != nil {
		t.Fatalf("get routing overview: %v", err)
	}
	if overview.ActiveCount != 1 || overview.StandbyCount != 1 || overview.UnhealthyCount != 2 {
		t.Fatalf("counts = active:%d standby:%d unhealthy:%d, want 1/1/2", overview.ActiveCount, overview.StandbyCount, overview.UnhealthyCount)
	}
	if got := overview.StatusByColleague["col-a"]; got.EffectiveState != RuntimeStateStandby || got.Reason != "manual_standby" {
		t.Fatalf("col-a status = %+v", got)
	}
	if got := overview.StatusByColleague["col-b"]; got.EffectiveState != RuntimeStateUnhealthy || got.Reason != "manual_unhealthy" {
		t.Fatalf("col-b status = %+v", got)
	}
	if got := overview.StatusByColleague["col-c"]; got.EffectiveState != RuntimeStateUnhealthy || got.Reason != "heartbeat_timeout" {
		t.Fatalf("col-c status = %+v", got)
	}
	if got := overview.StatusByColleague["col-d"]; got.EffectiveState != RuntimeStateActive || got.Reason != "heartbeat_healthy" {
		t.Fatalf("col-d status = %+v", got)
	}
}

func TestExecuteRoleRoutingActionPromotesStandby(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-action", Name: "Action Ops", Code: "action-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-action-a", Name: "Primary", RoleID: "role-action", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-action-b", Name: "Standby", RoleID: "role-action", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{RuntimeStateByColleague: map[string]string{"col-action-b": RuntimeStateStandby}}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	settings, err := svc.ExecuteRoleRoutingAction(testTenantID, "action-ops", RoleActionPromoteStandby)
	if err != nil {
		t.Fatalf("execute role action: %v", err)
	}
	if settings.RuntimeStateByColleague["col-action-b"] != RuntimeStateActive {
		t.Fatalf("runtime state = %q, want active", settings.RuntimeStateByColleague["col-action-b"])
	}
	if settings.PrimaryColleagueByRole["action-ops"] != "col-action-b" {
		t.Fatalf("primary colleague = %q, want col-action-b", settings.PrimaryColleagueByRole["action-ops"])
	}
	if settings.RoleStrategies["action-ops"] != StrategyPrimaryFirst {
		t.Fatalf("strategy = %q, want %q", settings.RoleStrategies["action-ops"], StrategyPrimaryFirst)
	}
}

func TestCreateTask_ByRoleRoutingTreatsExpiredHeartbeatAsUnhealthy(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-heartbeat", Name: "Heartbeat Ops", Code: "heartbeat-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-heart-a", Name: "HeartA", RoleID: "role-heartbeat", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-heart-b", Name: "HeartB", RoleID: "role-heartbeat", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})

	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		DefaultStrategy:         StrategyLeastLoaded,
		HeartbeatTimeoutSeconds: 30,
		LastHeartbeatByColleague: map[string]string{
			"col-heart-a": time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339),
			"col-heart-b": time.Now().UTC().Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	task, err := svc.Create(testTenantID, CreateRequest{Title: "heartbeat route", FromColleagueID: "col-a", ToRoleCode: "heartbeat-ops"})
	if err != nil {
		t.Fatalf("create heartbeat route: %v", err)
	}
	if task.ToColleagueID != "col-heart-b" {
		t.Fatalf("to_colleague_id = %q, want col-heart-b", task.ToColleagueID)
	}
}
func TestCreateTask_ByRoleRoutingRequiresActiveColleague(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	_, err := svc.Create(testTenantID, CreateRequest{
		Title: "finance review", FromColleagueID: "col-a", ToRoleCode: "finance",
	})
	if err == nil {
		t.Fatal("expected error when role has no active colleague")
	}
}

func TestTransition_HappyPath(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	task, _ := svc.Create(testTenantID, CreateRequest{
		Title: "test", FromColleagueID: "col-a", ToColleagueID: "col-b",
	})

	if err := svc.Transition(testTenantID, task.ID, StatusAccepted, "col-b", "", ""); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if err := svc.Transition(testTenantID, task.ID, StatusInProgress, "col-b", "", ""); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := svc.Transition(testTenantID, task.ID, StatusCompleted, "col-b", "done", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if err := svc.Transition(testTenantID, task.ID, StatusAccepted, "col-b", "", ""); err == nil {
		t.Error("expected error transitioning from terminal status")
	}
}

func TestTransitionRequiresActorID(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	task, err := svc.Create(testTenantID, CreateRequest{
		Title:           "handoff needs actor",
		FromColleagueID: "col-a",
		ToColleagueID:   "col-b",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Transition(testTenantID, task.ID, StatusAccepted, " ", "", "missing actor"); err == nil {
		t.Fatalf("expected missing actor error")
	}
	unchanged, err := svc.GetByID(testTenantID, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if unchanged.Status != StatusPending {
		t.Fatalf("status = %q, want pending", unchanged.Status)
	}
}

func TestTransition_DirectStartAndCompleteFromPending(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	startTask, _ := svc.Create(testTenantID, CreateRequest{
		Title: "start now", FromColleagueID: "col-a", ToColleagueID: "col-b",
	})
	if err := svc.Transition(testTenantID, startTask.ID, StatusInProgress, "col-b", "", "start from iWorker"); err != nil {
		t.Fatalf("direct start from pending: %v", err)
	}

	completeTask, _ := svc.Create(testTenantID, CreateRequest{
		Title: "finish now", FromColleagueID: "col-a", ToColleagueID: "col-b",
	})
	if err := svc.Transition(testTenantID, completeTask.ID, StatusCompleted, "col-b", "done", "completed from iWorker"); err != nil {
		t.Fatalf("direct complete from pending: %v", err)
	}
	updated, err := svc.GetByID(testTenantID, completeTask.ID)
	if err != nil {
		t.Fatalf("get completed task: %v", err)
	}
	if updated.Status != StatusCompleted || updated.Result != "done" {
		t.Fatalf("completed task = %+v, want completed with result", updated)
	}
}

func TestTransition_WorkflowBackedTaskDelegatesRuntimeTransitions(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	fake := &fakeWorkflowTransitioner{}
	svc.SetWorkflowStepTransitioner(fake)
	now := time.Now()

	for _, task := range []*Task{
		{ID: "collab-start", Title: "start workflow", FromColleagueID: "col-a", ToColleagueID: "col-b", Status: StatusPending, WorkflowStepInstanceID: "wf-step-start", CreatedAt: now, UpdatedAt: now},
		{ID: "collab-complete", Title: "complete workflow", FromColleagueID: "col-a", ToColleagueID: "col-b", Status: StatusInProgress, WorkflowStepInstanceID: "wf-step-complete", CreatedAt: now, UpdatedAt: now},
		{ID: "collab-reject", Title: "reject workflow", FromColleagueID: "col-a", ToColleagueID: "col-b", Status: StatusAccepted, WorkflowStepInstanceID: "wf-step-reject", CreatedAt: now, UpdatedAt: now},
	} {
		if err := svc.repo.InsertTask(testTenantID, task); err != nil {
			t.Fatalf("insert task %s: %v", task.ID, err)
		}
	}

	if err := svc.Transition(testTenantID, "collab-start", StatusInProgress, "col-b", "", "start from iWorker"); err != nil {
		t.Fatalf("start workflow-backed task: %v", err)
	}
	if err := svc.Transition(testTenantID, "collab-complete", StatusCompleted, "col-b", "final result", "done"); err != nil {
		t.Fatalf("complete workflow-backed task: %v", err)
	}
	if err := svc.Transition(testTenantID, "collab-reject", StatusRejected, "col-b", "", "not enough context"); err != nil {
		t.Fatalf("reject workflow-backed task: %v", err)
	}

	want := []workflowTransitionCall{
		{action: "start", tenant: testTenantID, step: "wf-step-start", actor: "col-b", value: "start from iWorker"},
		{action: "complete", tenant: testTenantID, step: "wf-step-complete", actor: "col-b", value: "final result"},
		{action: "reject", tenant: testTenantID, step: "wf-step-reject", actor: "col-b", value: "not enough context"},
	}
	if len(fake.calls) != len(want) {
		t.Fatalf("workflow calls = %+v, want %+v", fake.calls, want)
	}
	for i := range want {
		if fake.calls[i] != want[i] {
			t.Fatalf("workflow call %d = %+v, want %+v", i, fake.calls[i], want[i])
		}
	}
}

func TestTransition_RejectFromAnyNonTerminal(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	t1, _ := svc.Create(testTenantID, CreateRequest{Title: "t1", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	if err := svc.Transition(testTenantID, t1.ID, StatusRejected, "col-b", "", "not fit"); err != nil {
		t.Fatalf("reject from pending: %v", err)
	}

	t2, _ := svc.Create(testTenantID, CreateRequest{Title: "t2", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	_ = svc.Transition(testTenantID, t2.ID, StatusAccepted, "col-b", "", "")
	if err := svc.Transition(testTenantID, t2.ID, StatusRejected, "col-b", "", "cancelled"); err != nil {
		t.Fatalf("reject from accepted: %v", err)
	}

	t3, _ := svc.Create(testTenantID, CreateRequest{Title: "t3", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	_ = svc.Transition(testTenantID, t3.ID, StatusAccepted, "col-b", "", "")
	_ = svc.Transition(testTenantID, t3.ID, StatusInProgress, "col-b", "", "")
	if err := svc.Transition(testTenantID, t3.ID, StatusRejected, "col-b", "", "abandoned"); err != nil {
		t.Fatalf("reject from in_progress: %v", err)
	}
}

func TestListByColleague(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)

	t1, _ := svc.Create(testTenantID, CreateRequest{Title: "t1", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	svc.Create(testTenantID, CreateRequest{Title: "t2", FromColleagueID: "col-a", ToColleagueID: "col-c"})
	svc.Create(testTenantID, CreateRequest{Title: "t3", FromColleagueID: "col-c", ToColleagueID: "col-b"})
	completed, _ := svc.Create(testTenantID, CreateRequest{Title: "done", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	rejected, _ := svc.Create(testTenantID, CreateRequest{Title: "rejected", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	if err := svc.Transition(testTenantID, completed.ID, StatusCompleted, "col-b", "done", "done"); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if err := svc.Transition(testTenantID, rejected.ID, StatusRejected, "col-b", "", "not needed"); err != nil {
		t.Fatalf("reject task: %v", err)
	}
	if err := svc.Transition(testTenantID, t1.ID, StatusAccepted, "col-b", "", "accepted"); err != nil {
		t.Fatalf("accept task: %v", err)
	}

	tasks, _ := svc.ListByColleague(testTenantID, "col-b")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 open tasks for colleague b, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Status == StatusCompleted || task.Status == StatusRejected {
			t.Fatalf("terminal task leaked into client inbox: %+v", task)
		}
	}

	tasks, _ = svc.ListByColleague(testTenantID, "col-c")
	if len(tasks) != 1 {
		t.Errorf("expected 1 task for colleague c, got %d", len(tasks))
	}
}
