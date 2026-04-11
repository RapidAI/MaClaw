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
		ID: "role-test", Name: "测试角色", Code: "test",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})

	cr := colleagueRepo.New(p.Write, p.Read)
	for _, id := range []string{"col-a", "col-b", "col-c"} {
		_ = cr.Insert(testTenantID, &colleagueDomain.Colleague{
			ID: id, Name: id, RoleID: "role-test",
			Strengths: []string{}, Tasks: []string{},
			Status: "active", CreatedAt: now, UpdatedAt: now,
		})
	}
}

func TestCreateTask_Validation(t *testing.T) {
	p := setupTestDB(t)
	svc := NewService(NewRepo(p.Write, p.Read))

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
		t.Error("expected error for missing to_colleague_id")
	}
}

func TestCreateTask_Success(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := NewService(NewRepo(p.Write, p.Read))

	task, err := svc.Create(testTenantID, CreateRequest{
		Title: "周报整理", FromColleagueID: "col-a", ToColleagueID: "col-b", ToRoleCode: "office",
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

func TestTransition_HappyPath(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := NewService(NewRepo(p.Write, p.Read))

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

func TestTransition_InvalidTransition(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := NewService(NewRepo(p.Write, p.Read))

	task, _ := svc.Create(testTenantID, CreateRequest{
		Title: "test", FromColleagueID: "col-a", ToColleagueID: "col-b",
	})

	if err := svc.Transition(testTenantID, task.ID, StatusInProgress, "col-b", "", ""); err == nil {
		t.Error("expected error for invalid transition pending→in_progress")
	}

	if err := svc.Transition(testTenantID, task.ID, StatusCompleted, "col-b", "", ""); err == nil {
		t.Error("expected error for invalid transition pending→completed")
	}
}

func TestTransition_RejectFromAnyNonTerminal(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := NewService(NewRepo(p.Write, p.Read))

	t1, _ := svc.Create(testTenantID, CreateRequest{Title: "t1", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	if err := svc.Transition(testTenantID, t1.ID, StatusRejected, "col-b", "", "不合适"); err != nil {
		t.Fatalf("reject from pending: %v", err)
	}

	t2, _ := svc.Create(testTenantID, CreateRequest{Title: "t2", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	_ = svc.Transition(testTenantID, t2.ID, StatusAccepted, "col-b", "", "")
	if err := svc.Transition(testTenantID, t2.ID, StatusRejected, "col-b", "", "取消"); err != nil {
		t.Fatalf("reject from accepted: %v", err)
	}

	t3, _ := svc.Create(testTenantID, CreateRequest{Title: "t3", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	_ = svc.Transition(testTenantID, t3.ID, StatusAccepted, "col-b", "", "")
	_ = svc.Transition(testTenantID, t3.ID, StatusInProgress, "col-b", "", "")
	if err := svc.Transition(testTenantID, t3.ID, StatusRejected, "col-b", "", "放弃"); err != nil {
		t.Fatalf("reject from in_progress: %v", err)
	}
}

func TestListByColleague(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := NewService(NewRepo(p.Write, p.Read))

	svc.Create(testTenantID, CreateRequest{Title: "t1", FromColleagueID: "col-a", ToColleagueID: "col-b"})
	svc.Create(testTenantID, CreateRequest{Title: "t2", FromColleagueID: "col-a", ToColleagueID: "col-c"})
	svc.Create(testTenantID, CreateRequest{Title: "t3", FromColleagueID: "col-c", ToColleagueID: "col-b"})

	tasks, _ := svc.ListByColleague(testTenantID, "col-b")
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for colleague b, got %d", len(tasks))
	}

	tasks, _ = svc.ListByColleague(testTenantID, "col-c")
	if len(tasks) != 1 {
		t.Errorf("expected 1 task for colleague c, got %d", len(tasks))
	}
}
