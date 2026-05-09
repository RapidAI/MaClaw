package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchCenterGoalPushes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/client/goalwatch/pushes" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("colleague_id"); got != "worker-a" {
			t.Fatalf("colleague_id = %q", got)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pushes": []map[string]any{
				{"event_id": "gpush_1", "task_id": "task_1", "title": "stalled task", "to_colleague_id": "worker-a", "status": "in_progress", "reason": "assigned_executor_offline", "recommended_action": "restart_executor", "age_seconds": 600, "executor_status": "offline", "executor_heartbeat_age_seconds": 120},
			},
		})
	}))
	defer server.Close()

	pushes, err := fetchCenterGoalPushes(server.URL, "tenant-a", "worker-a", 10, 1)
	if err != nil {
		t.Fatalf("fetchCenterGoalPushes returned error: %v", err)
	}
	if len(pushes) != 1 || pushes[0].EventID != "gpush_1" || pushes[0].TaskID != "task_1" {
		t.Fatalf("unexpected pushes: %+v", pushes)
	}
	if pushes[0].RecommendedAction != "restart_executor" {
		t.Fatalf("RecommendedAction = %q, want restart_executor", pushes[0].RecommendedAction)
	}
	if pushes[0].ExecutorStatus != "offline" || pushes[0].ExecutorHeartbeatAgeSeconds != 120 {
		t.Fatalf("unexpected executor health fields: %+v", pushes[0])
	}
}

func TestFetchCenterGoalPushesRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pushes":[{"event_id":"gpush_1","task_id":"task_1"}]} {"pushes":[]}`))
	}))
	defer server.Close()

	_, err := fetchCenterGoalPushes(server.URL, "tenant-a", "worker-a", 10, 1)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("fetchCenterGoalPushes error = %v, want errCenterJSONTrailing", err)
	}
}

func TestFetchGoalPushesFallsBackToCacheWhenCenterReturnsInvalidJSON(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, TenantID: "tenant-a", DepartmentID: "ops", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeGoalPushesCache("tenant-a", "ops", "worker-a", []CenterGoalPush{{EventID: "cached-push", TaskID: "cached-task", Title: "cached work"}}); err != nil {
		t.Fatalf("write goal push cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pushes":[{"event_id":"live-push","task_id":"live-task"}]} {"pushes":[]}`))
	}))
	defer server.Close()
	settings, _ := readDiWorkerSettings()
	settings.Center.BaseURL = server.URL
	if err := writeDiWorkerSettings(settings); err != nil {
		t.Fatalf("write settings with server URL: %v", err)
	}

	app := NewApp()
	pushes, err := app.FetchGoalPushes(10)
	if err != nil {
		t.Fatalf("FetchGoalPushes returned error: %v", err)
	}
	if len(pushes) != 1 || pushes[0].EventID != "cached-push" || pushes[0].Source != "cache" || !pushes[0].Stale {
		t.Fatalf("pushes = %+v, want stale cached push", pushes)
	}
}

func TestAckCenterGoalPush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/client/goalwatch/pushes/gpush_1/ack" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q", got)
		}
		var req struct {
			ColleagueID string `json:"colleague_id"`
			Status      string `json:"status"`
			Note        string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		if req.ColleagueID != "worker-a" || req.Status != "resumed" || req.Note != "executor resumed" {
			t.Fatalf("unexpected ack request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"event_id": "gpush_1", "task_id": "task_1", "ack_event_id": "gack_1", "status": "resumed"})
	}))
	defer server.Close()

	result, err := ackCenterGoalPush(server.URL, "tenant-a", CenterGoalPushAckRequest{EventID: "gpush_1", ColleagueID: "worker-a", Status: "resumed", Note: "executor resumed"}, 1)
	if err != nil {
		t.Fatalf("ackCenterGoalPush returned error: %v", err)
	}
	if result.AckEventID != "gack_1" || result.Status != "resumed" {
		t.Fatalf("unexpected ack result: %+v", result)
	}
}

func TestAckCenterGoalPushEscapesEventID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.EscapedPath() != "/client/goalwatch/pushes/gpush%2Fteam%201/ack" {
			t.Fatalf("escaped path = %s", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"event_id": "gpush/team 1", "task_id": "task_1", "ack_event_id": "gack_1", "status": "resumed"})
	}))
	defer server.Close()

	result, err := ackCenterGoalPush(server.URL, "tenant-a", CenterGoalPushAckRequest{EventID: "gpush/team 1", ColleagueID: "worker-a", Status: "resumed"}, 1)
	if err != nil {
		t.Fatalf("ackCenterGoalPush returned error: %v", err)
	}
	if result.EventID != "gpush/team 1" {
		t.Fatalf("event_id = %q, want escaped event id round trip", result.EventID)
	}
}

func TestAckCenterGoalPushRejectsMissingAckResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	_, err := ackCenterGoalPush(server.URL, "tenant-a", CenterGoalPushAckRequest{EventID: "gpush_1", ColleagueID: "worker-a", Status: "resumed"}, 1)
	if err == nil || !strings.Contains(err.Error(), "missing ack result") {
		t.Fatalf("ackCenterGoalPush error = %v, want missing ack result", err)
	}
}

func TestRecoverCenterGoalPush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/client/goalwatch/pushes/gpush_1/recover" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q", got)
		}
		var req struct {
			ColleagueID string `json:"colleague_id"`
			Status      string `json:"status"`
			Note        string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		if req.ColleagueID != "worker-a" || req.Status != "recovered" || req.Note != "human approved resume" {
			t.Fatalf("unexpected recover request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"push":            map[string]any{"event_id": "gpush_1", "task_id": "task_1", "workflow_step_instance_id": "wfsi-1", "recovery_action": "resume_workflow_step", "recovery_method": "POST", "recovery_path": "/runtime/workflows/steps/wfsi-1/resume"},
			"ack":             map[string]any{"event_id": "gpush_1", "task_id": "task_1", "ack_event_id": "gack_1", "status": "recovered"},
			"recovery_action": "resume_workflow_step",
			"recovery_method": "POST",
			"recovery_path":   "/runtime/workflows/steps/wfsi-1/resume",
			"status":          "recovered",
		})
	}))
	defer server.Close()

	result, err := recoverCenterGoalPush(server.URL, "tenant-a", CenterGoalPushAckRequest{EventID: "gpush_1", ColleagueID: "worker-a", Note: "human approved resume"}, 1)
	if err != nil {
		t.Fatalf("recoverCenterGoalPush returned error: %v", err)
	}
	if result.Status != "recovered" || result.Ack.Status != "recovered" || result.Push.WorkflowStepInstanceID != "wfsi-1" || result.RecoveryPath == "" {
		t.Fatalf("unexpected recover result: %+v", result)
	}
}

func TestRecoverCenterGoalPushRejectsMissingRecoveryResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ack":    map[string]any{"event_id": "gpush_1", "task_id": "task_1", "ack_event_id": "gack_1", "status": "recovered"},
			"status": "recovered",
		})
	}))
	defer server.Close()

	_, err := recoverCenterGoalPush(server.URL, "tenant-a", CenterGoalPushAckRequest{EventID: "gpush_1", ColleagueID: "worker-a"}, 1)
	if err == nil || !strings.Contains(err.Error(), "missing recovery result") {
		t.Fatalf("recoverCenterGoalPush error = %v, want missing recovery result", err)
	}
}

func TestPostAgentInstanceHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/runtime/iworker/instances/heartbeat" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q", got)
		}
		var req CenterAgentInstanceHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		if req.WorkerID != "worker-a" || req.Role != "watcher" || req.LocalCacheMode != "cache_only" {
			t.Fatalf("unexpected heartbeat request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"instance": map[string]any{"tenant_id": "tenant-a", "worker_id": "worker-a", "instance_id": "worker-a:watcher", "role": "watcher", "status": "online"}})
	}))
	defer server.Close()

	result, err := postAgentInstanceHeartbeat(server.URL, "tenant-a", CenterAgentInstanceHeartbeatRequest{WorkerID: "worker-a", Role: "watcher", LocalCacheMode: "cache_only"}, 1)
	if err != nil {
		t.Fatalf("postAgentInstanceHeartbeat returned error: %v", err)
	}
	if result.Instance.InstanceID != "worker-a:watcher" || result.Instance.Role != "watcher" {
		t.Fatalf("unexpected heartbeat result: %+v", result)
	}
}

func TestFetchCenterAgentInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/client/iworker/instances" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("worker_id"); got != "worker-a" {
			t.Fatalf("worker_id = %q", got)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"instances": []map[string]any{{"tenant_id": "tenant-a", "worker_id": "worker-a", "instance_id": "worker-a:executor", "role": "executor", "status": "online"}}})
	}))
	defer server.Close()

	instances, err := fetchCenterAgentInstances(server.URL, "tenant-a", "worker-a", 1)
	if err != nil {
		t.Fatalf("fetchCenterAgentInstances returned error: %v", err)
	}
	if len(instances) != 1 || instances[0].Role != "executor" {
		t.Fatalf("unexpected instances: %+v", instances)
	}
}

func TestFetchCenterAgentInstancesRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instances":[],"padding":"` + strings.Repeat("x", centerClientJSONLimit) + `"}`))
	}))
	defer server.Close()

	_, err := fetchCenterAgentInstances(server.URL, "tenant-a", "worker-a", 1)
	if !errors.Is(err, errCenterJSONTooLarge) {
		t.Fatalf("fetchCenterAgentInstances error = %v, want errCenterJSONTooLarge", err)
	}
}

func TestAutoGoalPushAckFor(t *testing.T) {
	status, note, heartbeat := autoGoalPushAckFor(CenterGoalPush{RecommendedAction: "restart_executor"})
	if status != "resumed" || note != "watcher_auto_restart_executor" || !heartbeat {
		t.Fatalf("restart_executor = %q %q %v", status, note, heartbeat)
	}
	status, note, heartbeat = autoGoalPushAckFor(CenterGoalPush{RecommendedAction: "resume_task"})
	if status != "accepted" || note != "watcher_accepted_goal_push_resume_task" || heartbeat {
		t.Fatalf("resume_task = %q %q %v", status, note, heartbeat)
	}
}

func TestShouldAutoHandleGoalPushOnlyRestartsExecutor(t *testing.T) {
	if !shouldAutoHandleGoalPush(CenterGoalPush{RecommendedAction: "restart_executor"}) {
		t.Fatalf("restart_executor should be auto-handled")
	}
	for _, action := range []string{"resume_task", "start_task", "accept_task", ""} {
		if shouldAutoHandleGoalPush(CenterGoalPush{RecommendedAction: action}) {
			t.Fatalf("%q should not be auto-handled", action)
		}
	}
}

func TestStartGoalWatchAutoHandleStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := make(chan struct{}, 4)
	startGoalWatchAutoHandle(ctx, 10*time.Millisecond, 100*time.Millisecond, func(context.Context) { runs <- struct{}{} })

	select {
	case <-runs:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("auto handler did not run")
	}
	cancel()
	countAfterCancel := len(runs)
	time.Sleep(40 * time.Millisecond)
	if len(runs) > countAfterCancel+1 {
		t.Fatalf("auto handler continued after cancel")
	}
}

func TestStartGoalWatchAutoHandleSkipsOverlapBeforeTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unblock := make(chan struct{})
	started := make(chan struct{}, 4)
	var runs atomic.Int32

	startGoalWatchAutoHandle(ctx, 10*time.Millisecond, 200*time.Millisecond, func(context.Context) {
		runs.Add(1)
		started <- struct{}{}
		<-unblock
	})

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("auto handler did not start")
	}

	time.Sleep(60 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs = %d, want 1 before timeout", got)
	}

	close(unblock)
	cancel()
}

func TestStartGoalWatchAutoHandleCancelsStuckRunAfterTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan int32, 4)
	canceled := make(chan int32, 4)
	var runs atomic.Int32

	startGoalWatchAutoHandle(ctx, 10*time.Millisecond, 30*time.Millisecond, func(runCtx context.Context) {
		id := runs.Add(1)
		started <- id
		<-runCtx.Done()
		canceled <- id
	})

	select {
	case id := <-started:
		if id != 1 {
			t.Fatalf("first run id = %d, want 1", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first auto handler run did not start")
	}

	select {
	case id := <-canceled:
		if id != 1 {
			t.Fatalf("canceled run id = %d, want 1", id)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("stuck auto handler run was not canceled after timeout")
	}

	select {
	case id := <-started:
		if id != 2 {
			t.Fatalf("replacement run id = %d, want 2", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("replacement auto handler run did not start")
	}
}

func TestStartGoalWatchAutoHandleCancelsActiveRunOnParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	done := make(chan error, 1)

	startGoalWatchAutoHandle(ctx, time.Second, time.Minute, func(runCtx context.Context) {
		started <- struct{}{}
		<-runCtx.Done()
		done <- runCtx.Err()
	})

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("auto handler did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("run context error = %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("active auto handler run was not canceled")
	}
}

func TestFetchCenterGoalPushesContextCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fetchCenterGoalPushesContext(ctx, server.URL, "tenant-a", "worker-a", 10, 30)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		cancel()
		t.Fatalf("request did not reach test server")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fetch error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("fetch did not return after context cancellation")
	}
}

func TestStartGoalWatchAutoHandleObserverReportsLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unblock := make(chan struct{})
	started := make(chan int64, 4)
	skipped := make(chan struct{}, 4)
	finished := make(chan int64, 4)

	startGoalWatchAutoHandleWithObserver(ctx, 10*time.Millisecond, 200*time.Millisecond, func(context.Context) {
		<-unblock
	}, managedPeriodicWorkerObserver{
		OnStart: func(runID int64, _ time.Time) { started <- runID },
		OnSkip:  func(time.Time) { skipped <- struct{}{} },
		OnFinish: func(runID int64, _ time.Time) {
			finished <- runID
		},
	})

	select {
	case id := <-started:
		if id != 1 {
			t.Fatalf("start id = %d, want 1", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("observer did not see start")
	}

	select {
	case <-skipped:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("observer did not see skipped overlap")
	}

	close(unblock)
	select {
	case id := <-finished:
		if id != 1 {
			t.Fatalf("finish id = %d, want 1", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("observer did not see finish")
	}
}
