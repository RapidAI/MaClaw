package goalwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/agentruntime"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	centerdb "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func newTestService(t *testing.T, cfg Config) (*Service, *collaboration.Repo) {
	svc, repo, _ := newTestServiceWithProvider(t, cfg)
	return svc, repo
}

func newTestServiceWithProvider(t *testing.T, cfg Config) (*Service, *collaboration.Repo, *centerdb.Provider) {
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
	return NewService(repo, cfg), repo, provider
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

func TestCheckTenantPushIncludesWorkflowStepInstanceID(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-workflow-step", Title: "finish workflow step", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, WorkflowStepInstanceID: "wfsi-1", CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	result, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if result.Pushed != 1 || len(result.Pushes) != 1 || result.Pushes[0].WorkflowStepInstanceID != "wfsi-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Pushes[0].RecommendedAction != "resume_workflow_step" || result.Pushes[0].RecoveryMethod != "POST" || result.Pushes[0].RecoveryPath != "/runtime/workflows/steps/wfsi-1/resume" {
		t.Fatalf("unexpected recovery fields: %+v", result.Pushes[0])
	}
	events, err := repo.ListEvents("tenant-a", "task-workflow-step")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || parseNoteValue(events[0].Note, "workflow_step_instance_id") != "wfsi-1" || parseNoteValue(events[0].Note, "recovery_path") != "/runtime/workflows/steps/wfsi-1/resume" {
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

func TestListPushesForColleagueBackfillsWorkflowStepInstanceID(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	task := &collaboration.Task{ID: "task-workflow-backfill", Title: "workflow push", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, WorkflowStepInstanceID: "wfsi-backfill", CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-workflow-backfill", TaskID: "task-workflow-backfill", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=task_in_progress_stalled age_seconds=600", CreatedAt: now}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes: %v", err)
	}
	if len(pushes) != 1 || pushes[0].WorkflowStepInstanceID != "wfsi-backfill" {
		t.Fatalf("unexpected pushes: %+v", pushes)
	}
	if pushes[0].RecommendedAction != "resume_workflow_step" || pushes[0].RecoveryPath != "/runtime/workflows/steps/wfsi-backfill/resume" {
		t.Fatalf("unexpected recovery fields: %+v", pushes[0])
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

func TestCheckTenantPushesWhenAssignedExecutorOffline(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: 10 * time.Minute, PushCooldown: 10 * time.Minute})
	runtimeSvc := agentruntime.NewService(agentruntime.NewRepo(provider.Write, provider.Read))
	svc.SetAgentRuntime(runtimeSvc)
	if _, err := runtimeSvc.Heartbeat("tenant-a", agentruntime.HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor", Status: "online"}, now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	task := &collaboration.Task{ID: "task-executor-offline", Title: "resume automation", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-30 * time.Second)}
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
	if result.Pushes[0].Reason != "assigned_executor_offline" {
		t.Fatalf("reason = %q, want assigned_executor_offline", result.Pushes[0].Reason)
	}
	if result.Pushes[0].RecommendedAction != "restart_executor" || result.Pushes[0].ExecutorStatus != "offline" {
		t.Fatalf("unexpected remediation fields: %+v", result.Pushes[0])
	}
}

func TestCheckTenantDoesNotPushWhenAssignedExecutorOnlineAndTaskFresh(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: 10 * time.Minute, PushCooldown: 10 * time.Minute})
	runtimeSvc := agentruntime.NewService(agentruntime.NewRepo(provider.Write, provider.Read))
	svc.SetAgentRuntime(runtimeSvc)
	if _, err := runtimeSvc.Heartbeat("tenant-a", agentruntime.HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor", Status: "online"}, now.Add(-10*time.Second)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	task := &collaboration.Task{ID: "task-fresh", Title: "fresh task", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-30 * time.Second)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	result, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if result.Checked != 1 || result.Pushed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestListPushesForColleagueBackfillsRecommendedAction(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute})
	task := &collaboration.Task{ID: "task-offline", Title: "offline executor", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := repo.InsertEvent("tenant-a", &collaboration.TaskEvent{ID: "evt-offline", TaskID: "task-offline", Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: "reason=assigned_executor_offline age_seconds=30 executor_status=offline executor_heartbeat_age_seconds=120", CreatedAt: now}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes: %v", err)
	}
	if len(pushes) != 1 || pushes[0].RecommendedAction != "restart_executor" || pushes[0].ExecutorHeartbeatAgeSeconds != 120 {
		t.Fatalf("unexpected pushes: %+v", pushes)
	}
}

func TestCheckTenantShardSplitsTasks(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	for _, taskID := range []string{"task-shard-a", "task-shard-b", "task-shard-c", "task-shard-d"} {
		task := &collaboration.Task{ID: taskID, Title: taskID, FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
		if err := repo.InsertTask("tenant-a", task); err != nil {
			t.Fatalf("insert task %s: %v", taskID, err)
		}
	}

	left, err := svc.CheckTenantShard("tenant-a", now, 0, 2)
	if err != nil {
		t.Fatalf("left shard: %v", err)
	}
	right, err := svc.CheckTenantShard("tenant-a", now, 1, 2)
	if err != nil {
		t.Fatalf("right shard: %v", err)
	}
	if left.Checked+right.Checked != 4 || left.Pushed+right.Pushed != 4 {
		t.Fatalf("split result checked=%d pushed=%d, want 4/4", left.Checked+right.Checked, left.Pushed+right.Pushed)
	}
}

func TestCheckTenantDeterministicPushIDPreventsDuplicatePush(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-dedupe", Title: "dedupe", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	first, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	second, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if first.Pushed != 1 || second.Pushed != 0 {
		t.Fatalf("pushed first/second = %d/%d, want 1/0", first.Pushed, second.Pushed)
	}
	events, err := repo.ListEvents("tenant-a", "task-dedupe")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].ID != deterministicPushEventID("tenant-a", "task-dedupe", now, 10*time.Minute) {
		t.Fatalf("events = %+v", events)
	}
}

func TestMonitorRecommendedShardCountScalesAndClamps(t *testing.T) {
	svc, _ := newTestService(t, Config{WorkersPerShard: 2, MaxWatchers: 3})
	monitor := NewMonitor(svc, nil)

	checks := []struct {
		iworkers int
		want     int
	}{
		{iworkers: 0, want: 1},
		{iworkers: 1, want: 1},
		{iworkers: 2, want: 1},
		{iworkers: 3, want: 2},
		{iworkers: 6, want: 3},
		{iworkers: 99, want: 3},
	}
	for _, check := range checks {
		if got := monitor.recommendedShardCountForIWorkers(check.iworkers); got != check.want {
			t.Fatalf("recommendedShardCountForIWorkers(%d) = %d, want %d", check.iworkers, got, check.want)
		}
	}
}

func TestMonitorStatusCapturesShardedRun(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute, WorkersPerShard: 1, MaxWatchers: 4})
	runtimeSvc := agentruntime.NewService(agentruntime.NewRepo(provider.Write, provider.Read))
	svc.SetAgentRuntime(runtimeSvc)
	for _, workerID := range []string{"worker-a", "worker-b", "worker-c"} {
		if _, err := runtimeSvc.Heartbeat("tenant-a", agentruntime.HeartbeatRequest{WorkerID: workerID, InstanceID: workerID + ":executor", Role: "executor", Status: "online"}, now); err != nil {
			t.Fatalf("heartbeat %s: %v", workerID, err)
		}
	}
	for _, taskID := range []string{"task-status-a", "task-status-b", "task-status-c"} {
		task := &collaboration.Task{ID: taskID, Title: taskID, FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
		if err := repo.InsertTask("tenant-a", task); err != nil {
			t.Fatalf("insert task %s: %v", taskID, err)
		}
	}

	monitor := NewMonitor(svc, nil)
	monitor.checkTenantSharded("tenant-a", now)
	status := monitor.Status()

	if status.Config.WorkersPerShard != 1 || status.Config.MaxWatchers != 4 {
		t.Fatalf("unexpected config status: %+v", status.Config)
	}
	if len(status.Tenants) != 1 {
		t.Fatalf("tenant statuses = %d, want 1", len(status.Tenants))
	}
	tenantStatus := status.Tenants[0]
	if tenantStatus.IWorkerCount != 3 || tenantStatus.ShardCount != 3 || tenantStatus.Checked != 3 || tenantStatus.Pushed != 3 {
		t.Fatalf("unexpected tenant status: %+v", tenantStatus)
	}
	if len(tenantStatus.ShardStatuses) != 3 {
		t.Fatalf("shard statuses = %d, want 3", len(tenantStatus.ShardStatuses))
	}
	for i, shard := range tenantStatus.ShardStatuses {
		if shard.ShardIndex != i || shard.ShardCount != 3 || shard.Error != "" {
			t.Fatalf("unexpected shard status at %d: %+v", i, shard)
		}
	}
}

func TestMonitorSkipsShardWhenLeaseIsHeldByAnotherNode(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute, LeaseTTL: 5 * time.Minute})
	task := &collaboration.Task{ID: "task-lease-held", Title: "lease held", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	lease, _ := json.Marshal(shardLease{Owner: "other-node", ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339Nano)})
	if _, err := provider.Write.Exec(`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, goalWatchShardLeaseKey("tenant-a", 0, 1), string(lease)); err != nil {
		t.Fatalf("insert lease: %v", err)
	}

	monitor := NewMonitor(svc, nil)
	monitor.checkTenantSharded("tenant-a", now)
	status := monitor.Status()
	if len(status.Tenants) != 1 || status.Tenants[0].Checked != 0 || status.Tenants[0].Pushed != 0 || len(status.Tenants[0].ShardStatuses) != 1 || status.Tenants[0].ShardStatuses[0].LeaseHeld {
		t.Fatalf("unexpected skipped status: %+v", status)
	}
	events, err := repo.ListEvents("tenant-a", "task-lease-held")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no push events while lease is held, got %+v", events)
	}
}

func TestMonitorAcquiresExpiredShardLease(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute, LeaseTTL: 5 * time.Minute})
	task := &collaboration.Task{ID: "task-lease-expired", Title: "lease expired", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	lease, _ := json.Marshal(shardLease{Owner: "dead-node", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	if _, err := provider.Write.Exec(`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, goalWatchShardLeaseKey("tenant-a", 0, 1), string(lease)); err != nil {
		t.Fatalf("insert lease: %v", err)
	}

	monitor := NewMonitor(svc, nil)
	monitor.checkTenantSharded("tenant-a", now)
	status := monitor.Status()
	if len(status.Tenants) != 1 || status.Tenants[0].Checked != 1 || status.Tenants[0].Pushed != 1 || len(status.Tenants[0].ShardStatuses) != 1 || !status.Tenants[0].ShardStatuses[0].LeaseHeld || status.Tenants[0].ShardStatuses[0].LeaseOwner == "" {
		t.Fatalf("unexpected acquired status: %+v", status)
	}
}

func TestHandlerStatusEndpointReturnsMonitorStatus(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, Config{WorkersPerShard: 7, MaxWatchers: 9})
	monitor := NewMonitor(svc, nil)
	monitor.recordStatus(TenantMonitorStatus{TenantID: "tenant-a", StartedAt: now, FinishedAt: now.Add(time.Second), IWorkerCount: 14, ShardCount: 2, Checked: 5, Pushed: 1})
	handler := NewHandler(svc)
	handler.SetMonitor(monitor)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/goalwatch/status", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status code = %d, body=%s", res.Code, res.Body.String())
	}
	var body MonitorStatus
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Config.WorkersPerShard != 7 || body.Config.MaxWatchers != 9 {
		t.Fatalf("unexpected config: %+v", body.Config)
	}
	if len(body.Tenants) != 1 || body.Tenants[0].TenantID != "tenant-a" || body.Tenants[0].ShardCount != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestMonitorHealthReportsStaleAndShardErrors(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, Config{TickInterval: time.Minute})
	monitor := NewMonitor(svc, nil)
	monitor.recordStatus(TenantMonitorStatus{
		TenantID:   "tenant-a",
		StartedAt:  now.Add(-10 * time.Minute),
		FinishedAt: now.Add(-10 * time.Minute),
		ShardCount: 2,
		Error:      "one or more shards failed",
		ShardStatuses: []MonitorShardStatus{
			{ShardIndex: 0, ShardCount: 2, LeaseHeld: true, Checked: 1},
			{ShardIndex: 1, ShardCount: 2, LeaseHeld: true, Error: "boom"},
		},
	})

	health := monitor.Health(now)
	if health.Level != "critical" || !containsString(health.Reasons, "goalwatch_errors_detected") || !containsString(health.Reasons, "goalwatch_stale") || health.LastRunAgeSeconds <= health.StaleThresholdSeconds {
		t.Fatalf("health = %+v", health)
	}
}

func TestMonitorHealthWarnsWhenAllShardsSkippedByLease(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, Config{TickInterval: time.Minute})
	monitor := NewMonitor(svc, nil)
	monitor.recordStatus(TenantMonitorStatus{TenantID: "tenant-a", StartedAt: now.Add(-time.Second), FinishedAt: now.Add(-time.Second), ShardCount: 2, ShardStatuses: []MonitorShardStatus{{ShardIndex: 0, ShardCount: 2}, {ShardIndex: 1, ShardCount: 2}}})

	health := monitor.Health(now)
	if health.Level != "warning" || !containsString(health.Reasons, "all_goalwatch_shards_skipped_by_active_lease") {
		t.Fatalf("health = %+v", health)
	}
}

func TestHandlerHealthEndpointReportsMissingMonitor(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/goalwatch/health", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var health MonitorHealth
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Level != "warning" || !containsString(health.Reasons, "goalwatch_monitor_not_configured") || health.Config.LeaseTTLSeconds <= 0 {
		t.Fatalf("health = %+v", health)
	}
}

func TestHandlerUsesTenantHeaderForClientPushesAndAck(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-tenant-header", Title: "tenant scoped push", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	check, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if check.Pushed != 1 || len(check.Pushes) != 1 {
		t.Fatalf("unexpected check result: %+v", check)
	}

	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/client/goalwatch/pushes?colleague_id=worker-a", nil)
	listReq.Header.Set("X-Tenant-ID", "tenant-a")
	listRes := httptest.NewRecorder()
	mux.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	var listBody struct {
		Pushes []CenterPushForTest `json:"pushes"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listBody.Pushes) != 1 || listBody.Pushes[0].EventID == "" {
		t.Fatalf("unexpected pushes: %+v", listBody.Pushes)
	}

	body := bytes.NewBufferString(`{"colleague_id":"worker-a","status":"resumed","note":"tenant_header_ack"}`)
	ackReq := httptest.NewRequest(http.MethodPost, "/client/goalwatch/pushes/"+listBody.Pushes[0].EventID+"/ack", body)
	ackReq.Header.Set("X-Tenant-ID", "tenant-a")
	ackRes := httptest.NewRecorder()
	mux.ServeHTTP(ackRes, ackReq)
	if ackRes.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackRes.Code, ackRes.Body.String())
	}

	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list after ack: %v", err)
	}
	if len(pushes) != 0 {
		t.Fatalf("expected tenant-a push to be acked, got %+v", pushes)
	}
}

func TestHandlerRecoverPushRunsWorkflowRecoveryAndAck(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: time.Minute, PushCooldown: 10 * time.Minute})
	task := &collaboration.Task{ID: "task-recover", Title: "recover workflow", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, WorkflowStepInstanceID: "wfsi-recover", CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-6 * time.Minute)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	check, err := svc.CheckTenant("tenant-a", now)
	if err != nil {
		t.Fatalf("check tenant: %v", err)
	}
	if check.Pushed != 1 || len(check.Pushes) != 1 {
		t.Fatalf("unexpected check result: %+v", check)
	}
	recovery := &fakeRecoveryExecutor{}
	handler := NewHandler(svc)
	handler.SetRecoveryExecutor(recovery)
	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	pushes, err := svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes: %v", err)
	}
	if len(pushes) != 1 || pushes[0].EventID == "" {
		t.Fatalf("unexpected pushes: %+v", pushes)
	}
	body := bytes.NewBufferString(`{"colleague_id":"worker-a","note":"recover_now"}`)
	req := httptest.NewRequest(http.MethodPost, "/client/goalwatch/pushes/"+pushes[0].EventID+"/recover", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("recover status=%d body=%s", res.Code, res.Body.String())
	}
	var result RecoverResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode recover result: %v", err)
	}
	if !recovery.called || recovery.tenantID != "tenant-a" || recovery.stepID != "wfsi-recover" || recovery.actorID != "worker-a" {
		t.Fatalf("unexpected recovery call: %+v", recovery)
	}
	if result.Ack.Status != "recovered" || result.Push.RecoveryPath != "/runtime/workflows/steps/wfsi-recover/resume" {
		t.Fatalf("unexpected recover result: %+v", result)
	}
	pushes, err = svc.ListPushesForColleague("tenant-a", "worker-a", 10)
	if err != nil {
		t.Fatalf("list pushes after recover: %v", err)
	}
	if len(pushes) != 0 {
		t.Fatalf("expected recovered push to be acked, got %+v", pushes)
	}
}

type fakeRecoveryExecutor struct {
	called   bool
	tenantID string
	stepID   string
	actorID  string
	note     string
}

func (f *fakeRecoveryExecutor) StartOrResumeStep(tenantID string, stepInstanceID, actorID, note string) error {
	f.called = true
	f.tenantID = tenantID
	f.stepID = stepInstanceID
	f.actorID = actorID
	f.note = note
	return nil
}

func TestHandlerManualCheckWithTemporaryThresholdKeepsAgentRuntime(t *testing.T) {
	now := time.Now().UTC()
	svc, repo, provider := newTestServiceWithProvider(t, Config{StalledAfter: 10 * time.Minute, PushCooldown: 10 * time.Minute})
	runtimeSvc := agentruntime.NewService(agentruntime.NewRepo(provider.Write, provider.Read))
	svc.SetAgentRuntime(runtimeSvc)
	if _, err := runtimeSvc.Heartbeat("tenant-a", agentruntime.HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor", Status: "online"}, now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	task := &collaboration.Task{ID: "task-manual-runtime", Title: "manual check sees offline executor", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-30 * time.Second)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/admin/goalwatch/check?stalled_after_minutes=30", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body CheckResult
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Pushed != 1 || len(body.Pushes) != 1 || body.Pushes[0].Reason != "assigned_executor_offline" {
		t.Fatalf("expected offline executor push, got %+v", body)
	}
}

type CenterPushForTest struct {
	EventID string `json:"event_id"`
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestTenantPolicyPersistsInSystemSettings(t *testing.T) {
	svc, _ := newTestService(t, Config{TickInterval: 30 * time.Second, StalledAfter: 2 * time.Minute, PushCooldown: 3 * time.Minute, LeaseTTL: 45 * time.Second, WorkersPerShard: 7, MaxWatchers: 5})
	policy, err := svc.SaveTenantPolicy(context.Background(), "tenant-a", TenantPolicy{Enabled: true, SingleFlight: true, MaxRunSeconds: 120, ScaleByWorkerCount: true})
	if err != nil {
		t.Fatalf("save policy: %v", err)
	}
	if policy.WorkersPerShard != 7 || policy.MaxWatchers != 5 || policy.TickIntervalSeconds != 30 || policy.StalledAfterSeconds != 120 || policy.PushCooldownSeconds != 180 || policy.LeaseTTLSeconds != 45 {
		t.Fatalf("policy defaults not merged: %+v", policy)
	}

	loaded, ok, err := svc.GetTenantPolicy(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if !ok || loaded.MaxRunSeconds != 120 || !loaded.Enabled || !loaded.SingleFlight || !loaded.ScaleByWorkerCount || loaded.WorkersPerShard != 7 {
		t.Fatalf("loaded policy = %+v ok=%t", loaded, ok)
	}
}

func TestHandlerPolicyEndpointRoundTripsTenantPolicy(t *testing.T) {
	svc, _ := newTestService(t, Config{WorkersPerShard: 9, MaxWatchers: 4})
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	body := bytes.NewBufferString(`{"enabled":true,"single_flight":true,"max_run_seconds":90,"scale_by_worker_count":true}`)
	putReq := httptest.NewRequest(http.MethodPut, "/admin/goalwatch/policy", body)
	putReq.Header.Set("X-Tenant-ID", "tenant-a")
	putRes := httptest.NewRecorder()
	mux.ServeHTTP(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRes.Code, putRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/goalwatch/policy", nil)
	getReq.Header.Set("X-Tenant-ID", "tenant-a")
	getRes := httptest.NewRecorder()
	mux.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	var resp struct {
		Policy    TenantPolicy `json:"policy"`
		Persisted bool         `json:"persisted"`
	}
	if err := json.NewDecoder(getRes.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Persisted || resp.Policy.MaxRunSeconds != 90 || resp.Policy.WorkersPerShard != 9 || resp.Policy.MaxWatchers != 4 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestMonitorUsesTenantPolicyForShardScaling(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, _, provider := newTestServiceWithProvider(t, Config{WorkersPerShard: 50, MaxWatchers: 16})
	runtimeSvc := agentruntime.NewService(agentruntime.NewRepo(provider.Write, provider.Read))
	svc.SetAgentRuntime(runtimeSvc)
	for _, workerID := range []string{"worker-1", "worker-2", "worker-3", "worker-4", "worker-5"} {
		if _, err := runtimeSvc.Heartbeat("tenant-a", agentruntime.HeartbeatRequest{WorkerID: workerID, InstanceID: workerID + ":executor", Role: "executor", Status: "online"}, now); err != nil {
			t.Fatalf("heartbeat %s: %v", workerID, err)
		}
	}
	if _, err := svc.SaveTenantPolicy(context.Background(), "tenant-a", TenantPolicy{Enabled: true, SingleFlight: true, ScaleByWorkerCount: true, WorkersPerShard: 2, MaxWatchers: 3}); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	monitor := NewMonitor(svc, nil)
	monitor.checkTenantSharded("tenant-a", now)
	status := monitor.Status()
	if len(status.Tenants) != 1 {
		t.Fatalf("status tenants = %+v", status.Tenants)
	}
	tenantStatus := status.Tenants[0]
	if !tenantStatus.PolicyPersisted || tenantStatus.Policy.WorkersPerShard != 2 || tenantStatus.ShardCount != 3 || len(tenantStatus.ShardStatuses) != 3 {
		t.Fatalf("tenant status did not use persisted policy: %+v", tenantStatus)
	}
}

func TestMonitorUsesTenantPolicyForStalledThreshold(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	svc, repo := newTestService(t, Config{StalledAfter: 10 * time.Minute, PushCooldown: 10 * time.Minute})
	if _, err := svc.SaveTenantPolicy(context.Background(), "tenant-a", TenantPolicy{Enabled: true, SingleFlight: true, ScaleByWorkerCount: true, StalledAfterSeconds: 60, PushCooldownSeconds: 600}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	task := &collaboration.Task{ID: "task-policy-threshold", Title: "policy threshold", FromColleagueID: "planner", ToColleagueID: "worker-a", Status: collaboration.StatusInProgress, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-90 * time.Second)}
	if err := repo.InsertTask("tenant-a", task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	monitor := NewMonitor(svc, nil)
	monitor.checkTenantSharded("tenant-a", now)
	status := monitor.Status()
	if len(status.Tenants) != 1 || status.Tenants[0].Pushed != 1 || status.Tenants[0].Checked != 1 {
		t.Fatalf("expected policy threshold to push task, got %+v", status.Tenants)
	}
}
