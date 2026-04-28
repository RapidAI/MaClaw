package goalwatch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	centerdb "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func newTestService(t *testing.T, cfg Config) (*Service, *collaboration.Repo) {
	t.Helper()
	provider, err := centerdb.Open(filepath.Join(t.TempDir(), "center.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := centerdb.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedColleagues(t, provider)
	repo := collaboration.NewRepo(provider.Write, provider.Read)
	return NewService(repo, cfg), repo
}

func seedColleagues(t *testing.T, provider *centerdb.Provider) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.Exec(`INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-worker", "tenant-a", "Worker", "worker", "active", now, now); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	for _, id := range []string{"planner", "worker-a"} {
		if _, err := provider.Write.Exec(`INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, "tenant-a", id, "role-worker", "active", now, now); err != nil {
			t.Fatalf("insert colleague %s: %v", id, err)
		}
	}
}

func TestCheckTenantPushesStalledTask(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-1", Title: "finish report", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	result, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if result.Checked != 1 || result.Pushed != 1 || len(result.Pushes) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	events, err := repo.ListEvents("tenant-a", "task-1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Event != EventGoalPush || events[0].ActorID != "iworkercenter.goalwatch" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestCheckTenantRespectsPushCooldown(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-1", Title: "finish report", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusPending, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-1", TaskID: "task-1", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", CreatedAt: now.Add(-2 * time.Minute)}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	result, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if result.Checked != 1 || result.Pushed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckTenantSkipsTerminalTasks(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	task := &collaboration.Task{ID: "task-1", Title: "done", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusCompleted, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	result, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if result.Checked != 0 || result.Pushed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestListPushesForColleagueReturnsInbox(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	openTask := &collaboration.Task{ID: "task-open", Title: "continue analysis", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	doneTask := &collaboration.Task{ID: "task-done", Title: "done task", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusCompleted, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	otherTask := &collaboration.Task{ID: "task-other", Title: "other worker", FromColleagueID: "planner", ToColleagueID: "planner", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	for _, task := range []*collaboration.Task{openTask, doneTask, otherTask} {
		if err := repo.InsertTask("tenant-a", task); err != nil {
			t.Fatalf("insert task %s: %v", task.ID, err)
		}
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-open", TaskID: "task-open", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=task_in_progress_stalled age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert open event: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-done", TaskID: "task-done", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=done age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert done event: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-other", TaskID: "task-other", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=other age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert other event: %v", err)
	}

	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes: %v", err)
	}
	if len(pushes) != 1 || pushes[0].EventID != "evt-open" || pushes[0].Reason != "task_in_progress_stalled" || pushes[0].AgeSeconds != 600 {
		t.Fatalf("unexpected pushes: %+v", pushes)
	}
}

func TestAckPushHidesPushFromInbox(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	task := &collaboration.Task{ID: "task-open", Title: "continue analysis", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-open", TaskID: "task-open", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=task_in_progress_stalled age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	ack, err := svc.AckPush("tenant-a", "worker-a", "evt-open", "resumed", "continuing now", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ack push: %v", err)
	}
	if ack.EventID != "evt-open" || ack.TaskID != "task-open" || ack.Status != "resumed" || ack.AckEventID == "" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes: %v", err)
	}
	if len(pushes) != 0 {
		t.Fatalf("expected acked push to be hidden, got %+v", pushes)
	}
}

func TestAckPushRequiresAssignedColleague(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	task := &collaboration.Task{ID: "task-open", Title: "continue analysis", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-open", TaskID: "task-open", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=task_in_progress_stalled age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := svc.AckPush("tenant-a", "planner", "evt-open", "resumed", "wrong worker", now.Add(time.Minute)); err == nil {
		t.Fatal("expected non-assigned colleague ack to fail")
	}
}
