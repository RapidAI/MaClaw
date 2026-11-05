package guiapp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOverlayRemoteCloudWorkspaceRunStatus(t *testing.T) {
	resetCloudWorkspaceRunPublishState()
	t.Cleanup(resetCloudWorkspaceRunPublishState)
	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_remote",
			TaskRunStatus: "running",
			TaskRunPhase:  "execute",
			LeaseInUse:    true,
		}},
	})

	localLive := &ProjectWorkflowState{Status: "running", Phase: "local"}
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_remote", localLive); got == nil || got.Phase != "execute" {
		t.Fatalf("stale local running must yield to the leased remote run, got=%+v", got)
	}
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_remote", &ProjectWorkflowState{Status: "completed"}); got == nil || got.Status != "running" {
		t.Fatalf("another machine's live run must replace a finished local snapshot, got=%+v", got)
	}

	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_remote",
			TaskRunStatus: "running",
		}},
	})
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_remote", nil); got != nil {
		t.Fatalf("running without a lease must not stick, got=%+v", got)
	}

	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_remote",
			TaskRunStatus: "completed",
		}},
	})
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_remote", nil); got == nil || got.Status != "completed" {
		t.Fatalf("terminal remote status=%+v", got)
	}
	local := &ProjectWorkflowState{Status: "failed", ProjectPath: "local-task"}
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_remote", local); got == nil || got.Status != "completed" || got.ProjectPath != "local-task" {
		t.Fatalf("remote terminal status=%+v", got)
	}
}

func TestClipCloudWorkspaceRunFieldKeepsRunes(t *testing.T) {
	got := clipCloudWorkspaceRunField(strings.Repeat("a", 63) + "中")
	if got != strings.Repeat("a", 63) {
		t.Fatalf("clip=%q", got)
	}
}

func TestWriterCloudWorkspaceIgnoresRemoteRunStatus(t *testing.T) {
	resetCloudWorkspaceRunPublishState()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_writer")
		resetCloudWorkspaceRunPublishState()
	})
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_writer",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_writer",
			TaskRunStatus: "running",
			LeaseInUse:    true,
		}},
	})
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_writer", nil); got != nil {
		t.Fatalf("writer must keep its own empty snapshot, got=%+v", got)
	}
	local := &ProjectWorkflowState{Status: "failed"}
	if got := overlayRemoteCloudWorkspaceRunStatus("cws_writer", local); got == nil || got.Status != "failed" {
		t.Fatalf("writer status=%+v", got)
	}
}

func TestSyncCloudWorkspaceTasksSeesWorkspaceCreatedElsewhere(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_late", "后来的任务", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	storeCloudWorkspaceEntitlementCache(CloudWorkspaceEntitlement{
		Enabled:    true,
		Workspaces: []CloudWorkspaceEntitlementWorkspace{},
		Deleted:    []CloudWorkspaceDeletedWorkspace{},
		Shared:     []CloudWorkspaceSharedWorkspace{},
	})
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("stale entitlement must not materialize a task that is not in the cache, got=%+v", got)
	}
	app.syncCloudWorkspaceTasksFromHub()
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].Name != "后来的任务" {
		t.Fatalf("list=%+v", listed)
	}
}

func TestListTasksProjectsRemoteCloudRunStatus(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_running", "正在跑的任务", "", true)
	hub.entitlement.Workspaces[0].TaskRunStatus = "running"
	hub.entitlement.Workspaces[0].TaskRunPhase = "execute"
	hub.entitlement.Workspaces[0].LeaseInUse = true
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ActiveWorkflow == nil || listed[0].ActiveWorkflow.Status != "running" {
		t.Fatalf("list=%+v", listed)
	}

	hub.mu.Lock()
	hub.entitlement.Workspaces[0].LeaseInUse = false
	hub.mu.Unlock()
	_ = app.CloudWorkspaceEntitlement()
	listed = app.ListTasks(10)
	if len(listed) != 1 || (listed[0].ActiveWorkflow != nil && listed[0].ActiveWorkflow.Status == "running") {
		t.Fatalf("stale running stayed visible: %+v", listed)
	}

	hub.mu.Lock()
	hub.entitlement.Workspaces[0].TaskRunStatus = "completed"
	hub.mu.Unlock()
	_ = app.CloudWorkspaceEntitlement()
	listed = app.ListTasks(10)
	if len(listed) != 1 || listed[0].ActiveWorkflow == nil || listed[0].ActiveWorkflow.Status != "completed" {
		t.Fatalf("completed list=%+v", listed)
	}
}

func TestPublishCloudWorkspaceRunStatusOncePerChange(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_publish")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_publish", "要同步状态", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_publish",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	if ids := writableCloudWorkspaceIDs(); len(ids) != 1 || ids[0] != "cws_publish" {
		t.Fatalf("writable=%v", ids)
	}
	rememberCloudWorkspaceLocalRun("cws_publish", &ProjectWorkflowState{Status: "running", Phase: "execute"})
	if !app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_publish"}) {
		t.Fatal("expected a status publish")
	}
	hub.mu.Lock()
	raw := append([]byte(nil), hub.sidecars[cloudWorkspaceSidecarTask]...)
	gets := hub.sidecarGets
	hub.mu.Unlock()
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.Name != "要同步状态" || task.RunStatus != "running" || task.RunPhase != "execute" {
		t.Fatalf("sidecar=%+v", task)
	}
	if app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_publish"}) {
		t.Fatal("unchanged status must not be published again")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarGets != gets {
		t.Fatalf("second publish contacted hub, gets %d -> %d", gets, hub.sidecarGets)
	}
}

func TestLeaseLossPublishesSameStatusAgain(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_publish")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_publish", "要同步状态", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_publish",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberCloudWorkspaceLocalRun("cws_publish", &ProjectWorkflowState{Status: "running", Phase: "execute"})
	if !app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_publish"}) {
		t.Fatal("expected a status publish")
	}
	hub.mu.Lock()
	puts := hub.sidecarPuts
	stored := append([]byte(nil), hub.sidecars[cloudWorkspaceSidecarTask]...)
	// Another machine wrote task.json while this process did not hold the lease.
	hub.sidecars[cloudWorkspaceSidecarTask] = []byte(`{"name":"要同步状态","task_run_status":"failed"}`)
	hub.mu.Unlock()
	forgetCloudWorkspaceRunPublish("cws_publish")
	// A PUT that finished after the lease moved must not restore the cache.
	rememberCloudWorkspaceSentSidecar("cws_publish", cloudWorkspaceSidecarTask, stored, 0)
	if !app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_publish"}) {
		t.Fatal("status must publish again after the lease moves")
	}
	if app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_publish"}) {
		t.Fatal("republish must happen once")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarPuts != puts+1 {
		t.Fatalf("puts=%d want %d", hub.sidecarPuts, puts+1)
	}
	var again cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &again); err != nil {
		t.Fatal(err)
	}
	if again.RunStatus != "running" {
		t.Fatalf("sidecar=%+v", again)
	}
}

func TestStatusPublishSkipsWorkspaceWhileFileSyncHoldsLock(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_busy")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_busy", "推送中", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_busy",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberCloudWorkspaceLocalRun("cws_busy", &ProjectWorkflowState{Status: "running", Phase: "execute"})
	mount := lookupHeldCloudWorkspace("cws_busy")
	if mount == nil {
		t.Fatal("missing mount")
	}
	mount.syncMu.Lock()
	wrote := make(chan bool, 1)
	go func() {
		wrote <- app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_busy"})
	}()
	select {
	case got := <-wrote:
		if got {
			mount.syncMu.Unlock()
			t.Fatal("status publish ran while a file sync held the lock")
		}
	case <-time.After(time.Second):
		mount.syncMu.Unlock()
		t.Fatal("status publish blocked behind the file sync")
	}
	hub.mu.Lock()
	puts := hub.sidecarPuts
	hub.mu.Unlock()
	mount.syncMu.Unlock()
	if !app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_busy"}) {
		t.Fatal("status must publish after the file sync releases the lock")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarPuts != puts+1 {
		t.Fatalf("puts=%d want %d", hub.sidecarPuts, puts+1)
	}
}

func TestUnchangedSidecarPutSkipsHub(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_side", "工作区", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	body := []byte(`{"sticky":"same"}`)
	if err := app.putCloudWorkspaceSidecar(t.Context(), "cws_side", cloudWorkspaceSidecarWorkbench, body); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	gets := hub.sidecarGets
	puts := hub.sidecarPuts
	hub.mu.Unlock()
	if err := app.putCloudWorkspaceSidecar(t.Context(), "cws_side", cloudWorkspaceSidecarWorkbench, body); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarGets != gets || hub.sidecarPuts != puts {
		t.Fatalf("unchanged sidecar contacted hub, gets %d -> %d puts %d -> %d", gets, hub.sidecarGets, puts, hub.sidecarPuts)
	}
	if !bytes.Equal(hub.sidecars[cloudWorkspaceSidecarWorkbench], body) {
		t.Fatalf("sidecar=%s", hub.sidecars[cloudWorkspaceSidecarWorkbench])
	}
}

func TestUnchangedTaskSidecarFlushSkipsHub(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_same", "相同状态", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	rememberCloudWorkspaceLocalRun("cws_same", &ProjectWorkflowState{Status: "running", Phase: "execute"})
	if err := app.flushCloudWorkspaceTaskSidecar(t.Context(), "cws_same"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	gets := hub.sidecarGets
	puts := hub.sidecarPuts
	hub.mu.Unlock()
	if err := app.flushCloudWorkspaceTaskSidecar(t.Context(), "cws_same"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarGets != gets || hub.sidecarPuts != puts {
		t.Fatalf("unchanged task sidecar contacted hub, gets %d -> %d puts %d -> %d", gets, hub.sidecarGets, puts, hub.sidecarPuts)
	}
}

func TestOpenCloudWorkspaceDoesNotClearFinishedStatus(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_done")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_done", "已完成的任务", "", true)
	finished, err := json.Marshal(cloudWorkspaceTaskSidecar{Name: "已完成的任务", RunStatus: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	hub.sidecars[cloudWorkspaceSidecarTask] = finished
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_done",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberCloudWorkspaceLocalRun("cws_done", nil)
	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_done",
			TaskRunStatus: "completed",
		}},
	})
	if app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_done"}) {
		t.Fatal("opening a finished task must not publish")
	}
	if err := app.flushCloudWorkspaceTaskSidecar(t.Context(), "cws_done"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatal(err)
	}
	if task.RunStatus != "completed" {
		t.Fatalf("status cleared: %+v", task)
	}
}

func TestWriterClearsStaleRunningStatus(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_stale")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_stale", "中断的任务", "", true)
	running, err := json.Marshal(cloudWorkspaceTaskSidecar{Name: "中断的任务", RunStatus: "running"})
	if err != nil {
		t.Fatal(err)
	}
	hub.sidecars[cloudWorkspaceSidecarTask] = running
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_stale",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberCloudWorkspaceLocalRun("cws_stale", nil)
	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_stale",
			TaskRunStatus: "running",
			LeaseInUse:    true,
		}},
	})
	if !app.flushChangedCloudWorkspaceRunStatuses([]string{"cws_stale"}) {
		t.Fatal("stale running must be cleared")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatal(err)
	}
	if task.Name != "中断的任务" || task.RunStatus != "" {
		t.Fatalf("sidecar=%+v", task)
	}
}

func TestRepeatedTaskSidecarFlushSkipsIdenticalPut(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_same", "相同状态", "", true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	snap := cloudWorkspaceRunSnapshot{Status: "running", Phase: "execute"}
	if err := app.flushCloudWorkspaceTaskRun(t.Context(), "cws_same", &snap); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	puts := hub.sidecarPuts
	hub.mu.Unlock()
	if err := app.flushCloudWorkspaceTaskRun(t.Context(), "cws_same", &snap); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.sidecarPuts != puts {
		t.Fatalf("identical task sidecar was written again, puts %d -> %d", puts, hub.sidecarPuts)
	}
}

func TestReleaseFlushClearsStaleRunning(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_release")
		resetCloudWorkspaceEntitlementCache()
	})
	hub := newRestoreCloudWorkspaceTestHub("cws_release", "已停下", "", true)
	running, err := json.Marshal(cloudWorkspaceTaskSidecar{Name: "已停下", RunStatus: "running"})
	if err != nil {
		t.Fatal(err)
	}
	hub.sidecars[cloudWorkspaceSidecarTask] = running
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_release",
		LocalPath:    t.TempDir(),
		FencingToken: 1,
	})
	rememberRemoteCloudWorkspaceRunStatuses(CloudWorkspaceEntitlement{
		Workspaces: []CloudWorkspaceEntitlementWorkspace{{
			ID:            "cws_release",
			TaskRunStatus: "running",
		}},
	})
	if err := app.publishCloudWorkspaceRunForRelease(t.Context(), "cws_release"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatal(err)
	}
	if task.RunStatus != "" {
		t.Fatalf("release left stale running: %+v", task)
	}
}

func TestReadOnlyCloudWorkspaceDoesNotPublishRunStatus(t *testing.T) {
	resetCloudWorkspaceRunPublishState()
	t.Cleanup(func() {
		takeCloudWorkspaceMount("cws_readonly")
		resetCloudWorkspaceRunPublishState()
	})
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID:  "cws_readonly",
		LocalPath:    t.TempDir(),
		ReadOnly:     true,
		FencingToken: 1,
	})
	if ids := writableCloudWorkspaceIDs(); len(ids) != 0 {
		t.Fatalf("read-only mount must not publish, ids=%v", ids)
	}
}
