package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func newRestoreCloudWorkspaceTestHub(workspaceID, taskName, taskMode string, includeEntitlementTask bool) *fakeCloudWorkspaceHub {
	taskRaw, _ := json.Marshal(cloudWorkspaceTaskSidecar{
		Name: taskName,
		Mode: taskMode,
		Tag:  cloudWorkspaceTag(workspaceID),
	})
	ws := CloudWorkspaceEntitlementWorkspace{
		ID:   workspaceID,
		Name: "工作区 1",
	}
	if includeEntitlementTask {
		ws.TaskName = taskName
		ws.TaskMode = taskMode
	}
	return &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		sidecars: map[string][]byte{cloudWorkspaceSidecarTask: taskRaw},
		entitlement: &CloudWorkspaceEntitlement{
			Enabled:    true,
			Quota:      5,
			Used:       1,
			Workspaces: []CloudWorkspaceEntitlementWorkspace{ws},
			Deleted:    []CloudWorkspaceDeletedWorkspace{},
		},
	}
}

func TestRestoreCloudWorkspaceTasksCreatesLocalTaskWithoutLease(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_newpc", "跨设备任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)

	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("new machine list=%+v", got)
	}

	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 {
		t.Fatalf("restored=%+v", restored)
	}
	if restored[0].Name != "跨设备任务" {
		t.Fatalf("name=%q", restored[0].Name)
	}
	if !projectRecordHasTagLike(restored[0].Tags, cloudWorkspaceTag("cws_newpc")) {
		t.Fatalf("missing workspace tag: %v", restored[0].Tags)
	}
	if !projectRecordHasTagLike(restored[0].Tags, taskCodingDevTag) {
		t.Fatalf("missing mode tag: %v", restored[0].Tags)
	}

	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ProjectPath != restored[0].ProjectPath {
		t.Fatalf("list=%+v", listed)
	}
	hub.mu.Lock()
	acquires := hub.leaseAcquires
	hub.mu.Unlock()
	if acquires != 0 {
		t.Fatalf("restore must not acquire a lease, acquires=%d", acquires)
	}

	again := app.RestoreCloudWorkspaceTasks()
	if len(again) != 0 {
		t.Fatalf("second restore=%+v", again)
	}
	if got := app.ListTasks(10); len(got) != 1 {
		t.Fatalf("list after second restore=%+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksFallsBackToSidecarAndWorkspaceName(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_sidecar", "从 sidecar 恢复", taskCodingDevTag, false)
	app := newCloudWorkspaceMountTestApp(t, hub)

	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 || restored[0].Name != "从 sidecar 恢复" {
		t.Fatalf("sidecar restore=%+v", restored)
	}

	resetCloudWorkspaceEntitlementCache()
	hub2 := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		entitlement: &CloudWorkspaceEntitlement{
			Enabled: true,
			Quota:   5,
			Used:    1,
			Workspaces: []CloudWorkspaceEntitlementWorkspace{{
				ID:   "cws_named",
				Name: "标书项目",
			}},
			Deleted: []CloudWorkspaceDeletedWorkspace{},
		},
	}
	app2 := newCloudWorkspaceMountTestApp(t, hub2)
	named := app2.RestoreCloudWorkspaceTasks()
	if len(named) != 1 || named[0].Name != "标书项目" {
		t.Fatalf("workspace name fallback=%+v", named)
	}
}

func TestRestoreCloudWorkspaceTasksSkipsHiddenAndDeleted(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_hide", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_hide")

	app.HideTask(created.ProjectPath)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("hidden restore=%+v", got)
	}

	resetCloudWorkspaceEntitlementCache()
	hub2 := newRestoreCloudWorkspaceTestHub("cws_del", "云端任务", taskCodingDevTag, true)
	app2 := newCloudWorkspaceMountTestApp(t, hub2)
	created2 := mustCreateCloudWorkspaceTask(t, app2, "云端任务", "", "coding_dev", "cws_del")
	if err := app2.DeleteTask(created2.ProjectPath); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if got := app2.ListTasks(10); len(got) != 0 {
		t.Fatalf("list after delete=%+v", got)
	}
	if got := app2.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("deleted restore=%+v", got)
	}
}

func TestCreateTaskWithCloudWorkspaceFlushesTaskSidecarImmediately(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	mustCreateCloudWorkspaceTask(t, app, "标书项目", "", "coding_dev", "cws_flush")

	hub.mu.Lock()
	defer hub.mu.Unlock()
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatalf("task sidecar missing after create: %v raw=%q", err, hub.sidecars[cloudWorkspaceSidecarTask])
	}
	if task.Name != "标书项目" || task.Mode != taskCodingDevTag || task.Tag != cloudWorkspaceTag("cws_flush") {
		t.Fatalf("task sidecar=%+v", task)
	}
}

func TestRestoreCloudWorkspaceTasksSkipsSidecarGetWhenNamePresent(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_named", "跨设备任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 1 {
		t.Fatalf("restored=%+v", got)
	}
	hub.mu.Lock()
	gets := hub.sidecarGets
	hub.mu.Unlock()
	if gets != 0 {
		t.Fatalf("entitlement task_name should skip sidecar GET, gets=%d", gets)
	}
}

func TestHideTaskWinsOverInFlightUnhide(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_hide_race", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_hide_race")
	app.hideLocalCloudWorkspaceTask("cws_hide_race", false)
	app.HideTask(created.ProjectPath)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("hide tombstone must win: %+v", got)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("hidden task still listed: %+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksSkipsArchived(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_arch", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_arch")
	app.ensureMemoryStore()
	if pi := app.memoryStore.ProjectIndex(); pi != nil {
		pi.SetArchived(created.ProjectPath, true)
	}
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("archived restore=%+v", got)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("archived must stay off the list: %+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksUnhidesUndismissedHidden(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_unhide", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_unhide")
	app.hideLocalCloudWorkspaceTask("cws_unhide", false)
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("hidden list=%+v", got)
	}
	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 || restored[0].ProjectPath != created.ProjectPath {
		t.Fatalf("unhide=%+v want %q", restored, created.ProjectPath)
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ProjectPath != created.ProjectPath {
		t.Fatalf("list after unhide=%+v", listed)
	}
}

func TestRestoreCloudWorkspaceClearsTombstone(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_del", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_del")
	if err := app.DeleteTask(created.ProjectPath); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("tombstone restore=%+v", got)
	}
	// DeleteTask also soft-deletes the Hub workspace. Clearing the local
	// tombstone is not enough until the workspace itself is restored.
	app.clearDismissedCloudWorkspaceTask("cws_del")
	resetCloudWorkspaceEntitlementCache()
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("deleted workspace must stay gone: %+v", got)
	}
	if _, err := app.RestoreCloudWorkspace("cws_del"); err != nil {
		t.Fatalf("RestoreCloudWorkspace: %v", err)
	}
	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 || restored[0].Name != "云端任务" {
		t.Fatalf("after workspace restore=%+v", restored)
	}
	if restored[0].ProjectPath == created.ProjectPath {
		t.Fatalf("deleted record must not be reused: %q", restored[0].ProjectPath)
	}
}

func TestDeleteTaskSoftDeletesCloudWorkspace(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_release", "人工智能数学基础书编写", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "人工智能数学基础书编写", "", "coding_dev", "cws_release")
	if err := app.DeleteTask(created.ProjectPath); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("list after delete=%+v", got)
	}
	resetCloudWorkspaceEntitlementCache()
	ent := app.CloudWorkspaceEntitlement()
	if ent.Used != 0 || len(ent.Workspaces) != 0 {
		t.Fatalf("workspace should be released: used=%d workspaces=%+v", ent.Used, ent.Workspaces)
	}
	if len(ent.Deleted) != 1 || ent.Deleted[0].ID != "cws_release" {
		t.Fatalf("deleted=%+v", ent.Deleted)
	}
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("released workspace must not restore a task: %+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksContinuesAfterDeletingSibling(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_keep", "保留任务", taskCodingDevTag, true)
	hub.entitlement.Workspaces = append(hub.entitlement.Workspaces, CloudWorkspaceEntitlementWorkspace{
		ID: "cws_drop", Name: "删除任务", TaskName: "删除任务", TaskMode: taskCodingDevTag,
	})
	hub.entitlement.Used = 2
	app := newCloudWorkspaceMountTestApp(t, hub)
	dropped := mustCreateCloudWorkspaceTask(t, app, "删除任务", "", "coding_dev", "cws_drop")
	if err := app.DeleteTask(dropped.ProjectPath); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 || restored[0].Name != "保留任务" {
		t.Fatalf("other workspace should still restore: %+v", restored)
	}
}

func TestDeleteTaskHidesReplacementRowForSameWorkspace(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_dup", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_dup")
	dup := app.createTaskRecordWithWorkingDir("重复行", "", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_dup")}, created.WorkingDir, true)
	if strings.TrimSpace(dup.ProjectPath) == "" || dup.ProjectPath == created.ProjectPath {
		t.Fatalf("dup=%+v created=%q", dup, created.ProjectPath)
	}
	if err := app.DeleteTask(created.ProjectPath); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("replacement row still listed: %+v", got)
	}
}

func TestLookupCloudWorkspaceIDForProjectUsesWorkingDir(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_lookup", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	cache := app.cloudWorkspaceCachePath(app.cloudWorkspaceTenantID(), "cws_lookup")
	created := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag}, cache, true)
	if created.ProjectPath == "" {
		t.Fatal("legacy cloud task was not created")
	}
	if got := app.lookupCloudWorkspaceIDForProject(created.ProjectPath); got != "cws_lookup" {
		t.Fatalf("lookup=%q want cws_lookup", got)
	}
	app.HideTask(created.ProjectPath)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("working-dir hide must tombstone restore: %+v", got)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("hidden working-dir task still listed: %+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksReusesWorkingDirIdentity(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_legacy", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	cache := app.cloudWorkspaceCachePath(app.cloudWorkspaceTenantID(), "cws_legacy")
	created := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag}, cache, true)
	if created.ProjectPath == "" {
		t.Fatal("legacy cloud task was not created")
	}
	if projectRecordHasTagLike(created.Tags, cloudWorkspaceTag("cws_legacy")) {
		t.Fatalf("setup must omit the workspace tag: %v", created.Tags)
	}

	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 0 {
		t.Fatalf("restore should reuse the visible row, got %+v", restored)
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ProjectPath != created.ProjectPath {
		t.Fatalf("list after restore=%+v want %q", listed, created.ProjectPath)
	}
	if countVisibleCloudWorkspaceTasks(t, app, "cws_legacy") != 1 {
		t.Fatalf("visible rows for workspace=%d", countVisibleCloudWorkspaceTasks(t, app, "cws_legacy"))
	}
}

func TestRestoreCloudWorkspaceTaskRecordReusesVisibleRow(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_reuse", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_reuse")
	again := app.restoreCloudWorkspaceTaskRecord(CloudWorkspaceEntitlementWorkspace{
		ID: "cws_reuse", Name: "云端任务",
	}, "重复行", taskCodingDevTag)
	if again.ProjectPath != created.ProjectPath {
		t.Fatalf("restore record=%q want existing %q", again.ProjectPath, created.ProjectPath)
	}
	if countVisibleCloudWorkspaceTasks(t, app, "cws_reuse") != 1 {
		t.Fatalf("visible rows=%d list=%+v", countVisibleCloudWorkspaceTasks(t, app, "cws_reuse"), app.ListTasks(10))
	}
}

func TestRestorePrefersVisibleRowOverHiddenProcessMap(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_stale", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	hidden := mustCreateCloudWorkspaceTask(t, app, "长江学者申请", "", "coding_dev", "cws_stale")
	visible := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_stale")}, hidden.WorkingDir, true)
	if visible.ProjectPath == "" || visible.ProjectPath == hidden.ProjectPath {
		t.Fatalf("visible=%+v hidden=%q", visible, hidden.ProjectPath)
	}
	app.ensureMemoryStore()
	if pi := app.memoryStore.ProjectIndex(); pi != nil {
		pi.SetHidden(hidden.ProjectPath, true)
	}
	rememberCloudWorkspaceTask("cws_stale", hidden)

	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("restore should keep the visible row, got %+v", got)
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ProjectPath != visible.ProjectPath {
		t.Fatalf("list=%+v want visible %q", listed, visible.ProjectPath)
	}
	if pi := app.memoryStore.ProjectIndex(); pi == nil || !pi.IsHidden(hidden.ProjectPath) {
		t.Fatalf("hidden leftover was unhidden: %q", hidden.ProjectPath)
	}
}

func TestResumeCloudWorkspaceTaskHidesDuplicateRows(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_click", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "长江学者申请", "", "coding_dev", "cws_click")
	dup := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_click")}, created.WorkingDir, true)
	if dup.ProjectPath == "" || dup.ProjectPath == created.ProjectPath {
		t.Fatalf("dup=%+v created=%q", dup, created.ProjectPath)
	}

	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_click")
	if resumed.ProjectPath == "" {
		t.Fatal("resume returned empty")
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 {
		t.Fatalf("list after resume=%+v", listed)
	}
	if countVisibleCloudWorkspaceTasks(t, app, "cws_click") != 1 {
		t.Fatalf("visible rows after resume=%d", countVisibleCloudWorkspaceTasks(t, app, "cws_click"))
	}
}

func TestResumeCloudWorkspaceTaskKeepsClickedRow(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_prefer", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	clicked := mustCreateCloudWorkspaceTask(t, app, "长江学者申请", "", "coding_dev", "cws_prefer")
	newer := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_prefer")}, clicked.WorkingDir, true)
	if newer.ProjectPath == "" || newer.ProjectPath == clicked.ProjectPath {
		t.Fatalf("newer=%+v clicked=%q", newer, clicked.ProjectPath)
	}
	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_prefer", clicked.ProjectPath)
	if resumed.ProjectPath != clicked.ProjectPath {
		t.Fatalf("resume=%q want clicked %q", resumed.ProjectPath, clicked.ProjectPath)
	}
	if countVisibleCloudWorkspaceTasks(t, app, "cws_prefer") != 1 {
		t.Fatalf("visible rows after resume=%d", countVisibleCloudWorkspaceTasks(t, app, "cws_prefer"))
	}
	listed := app.ListTasks(10)
	if len(listed) != 1 || listed[0].ProjectPath != clicked.ProjectPath {
		t.Fatalf("list=%+v want clicked %q", listed, clicked.ProjectPath)
	}
}

func TestRestoreCloudWorkspaceTaskRecordRespectsDismissed(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_tombstone", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_tombstone")
	app.HideTask(created.ProjectPath)
	again := app.restoreCloudWorkspaceTaskRecord(CloudWorkspaceEntitlementWorkspace{
		ID: "cws_tombstone", Name: "云端任务",
	}, "重复行", taskCodingDevTag)
	if strings.TrimSpace(again.ProjectPath) != "" {
		t.Fatalf("dismissed workspace created/unhid %+v", again)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("list after dismissed restore=%+v", got)
	}
}

func TestDeleteCloudWorkspaceHidesDuplicateLocalRows(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_deldup", "长江学者申请", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "长江学者申请", "", "coding_dev", "cws_deldup")
	dup := app.createTaskRecordWithWorkingDir("长江学者申请", "", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_deldup")}, created.WorkingDir, true)
	if dup.ProjectPath == "" || dup.ProjectPath == created.ProjectPath {
		t.Fatalf("dup=%+v created=%q", dup, created.ProjectPath)
	}
	if _, err := app.DeleteCloudWorkspace("cws_deldup"); err != nil {
		t.Fatalf("DeleteCloudWorkspace: %v", err)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("duplicate rows survived workspace delete: %+v", got)
	}
	if countVisibleCloudWorkspaceTasks(t, app, "cws_deldup") != 0 {
		t.Fatalf("visible rows after delete=%d", countVisibleCloudWorkspaceTasks(t, app, "cws_deldup"))
	}
}

func TestHideOtherLocalCloudWorkspaceTasksRequiresKeepPath(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_keep", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_keep")
	app.hideOtherLocalCloudWorkspaceTasks("cws_keep", "")
	if got := app.ListTasks(10); len(got) != 1 || got[0].ProjectPath != created.ProjectPath {
		t.Fatalf("empty keep path must not hide the row: %+v", got)
	}
}

func TestCloudWorkspaceIdentityFromResultPrefersWorkingDir(t *testing.T) {
	got := cloudWorkspaceIdentityFromResult(ProjectSearchResult{
		ProjectPath: "legacy",
		WorkingDir:  "C:/data/cloud-workspaces/tenant_default/cws_b",
		Tags:        []string{taskManagementTag, cloudWorkspaceTag("cws_a"), cloudWorkspaceTag("cws_b")},
	})
	if got != "cws_b" {
		t.Fatalf("identity=%q want cws_b from working_dir", got)
	}
}

func TestRecordIsLocalCloudWorkspaceTaskUsesWorkingDirOverAccumulatedTags(t *testing.T) {
	rec := memory.ProjectRecord{
		ProjectPath: "legacy",
		Tags: []string{
			taskManagementTag,
			cloudWorkspaceTag("cws_a"),
			cloudWorkspaceTag("cws_b"),
			recentTaskWorkingDirTag("C:/data/cloud-workspaces/tenant_default/cws_b"),
		},
	}
	if recordIsLocalCloudWorkspaceTask(rec, "cws_a") {
		t.Fatal("working_dir identity is cws_b; cws_a tag must not claim the row")
	}
	if !recordIsLocalCloudWorkspaceTask(rec, "cws_b") {
		t.Fatal("working_dir identity should match cws_b")
	}
}

func countVisibleCloudWorkspaceTasks(t *testing.T, app *App, workspaceID string) int {
	t.Helper()
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store missing")
	}
	pi := app.memoryStore.ProjectIndex()
	if pi == nil {
		t.Fatal("project index missing")
	}
	count := 0
	for _, rec := range pi.ListAllMatching(func(candidate memory.ProjectRecord) bool {
		return recordIsLocalCloudWorkspaceTask(candidate, workspaceID)
	}) {
		if pi.IsArchived(rec.ProjectPath) || pi.IsHidden(rec.ProjectPath) {
			continue
		}
		count++
	}
	return count
}

func TestHideTaskKeepsCloudWorkspaceForRetry(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_retry", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_retry")
	app.HideTask(created.ProjectPath)
	resetCloudWorkspaceEntitlementCache()
	ent := app.CloudWorkspaceEntitlement()
	if ent.Used != 1 || len(ent.Workspaces) != 1 || ent.Workspaces[0].ID != "cws_retry" {
		t.Fatalf("HideTask must keep the workspace: %+v", ent)
	}
	replacement := mustCreateCloudWorkspaceTask(t, app, "云端任务重试", "", "coding_dev", "cws_retry")
	if replacement.ProjectPath == created.ProjectPath {
		t.Fatalf("replacement reused hidden path %q", replacement.ProjectPath)
	}
}

func TestDeleteAndRestoreCloudWorkspaceSyncsTaskList(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := newRestoreCloudWorkspaceTestHub("cws_cycle", "云端任务", taskCodingDevTag, true)
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_cycle")

	if _, err := app.DeleteCloudWorkspace("cws_cycle"); err != nil {
		t.Fatalf("DeleteCloudWorkspace: %v", err)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("list after workspace delete=%+v", got)
	}
	resetCloudWorkspaceEntitlementCache()
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("deleted workspace must not restore a task: %+v", got)
	}

	if _, err := app.RestoreCloudWorkspace("cws_cycle"); err != nil {
		t.Fatalf("RestoreCloudWorkspace: %v", err)
	}
	restored := app.RestoreCloudWorkspaceTasks()
	if len(restored) != 1 || restored[0].ProjectPath != created.ProjectPath {
		t.Fatalf("workspace restore should unhide %q, got %+v", created.ProjectPath, restored)
	}
	if got := app.ListTasks(10); len(got) != 1 || got[0].ProjectPath != created.ProjectPath {
		t.Fatalf("list after workspace restore=%+v", got)
	}
}

func TestRestoreCloudWorkspaceTasksDisabledDoesNothing(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	hub := &fakeCloudWorkspaceHub{
		entitlement: &CloudWorkspaceEntitlement{
			Enabled:    false,
			Workspaces: []CloudWorkspaceEntitlementWorkspace{{ID: "cws_off", Name: "隐藏"}},
			Deleted:    []CloudWorkspaceDeletedWorkspace{},
		},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	if got := app.RestoreCloudWorkspaceTasks(); len(got) != 0 {
		t.Fatalf("disabled restore=%+v", got)
	}
	if got := app.ListTasks(10); len(got) != 0 {
		t.Fatalf("disabled should not create tasks: %+v", got)
	}
}
