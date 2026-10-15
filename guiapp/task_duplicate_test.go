package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestDuplicateTaskCopiesConfigWithoutConversation(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.CreateTaskWithMode("调研报告", workingDir, "coding_dev")
	if source.ProjectPath == "" {
		t.Fatal("CreateTaskWithMode returned empty path")
	}
	app.RenameTask(source.ProjectPath, "调研报告")

	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr
	handler.memory.Save(projectSessionOwnerID(source.ProjectPath), []agent.ConversationEntry{
		{Role: "user", Content: "上一轮问题"},
		{Role: "assistant", Content: "上一轮回答"},
	})
	workbench := stickyCodingWorkbenchMemory{
		Kind:                  "local",
		ProjectPath:           workingDir,
		LastUserText:          "上一轮问题",
		LastSummary:           "上一轮回答",
		SessionPlan:           "把报告写完",
		TurnCount:             3,
		SessionPermissionMode: "workspace",
		PlanMode:              "approve",
		RoutePref:             "reasoning",
		WorktreeMode:          "off",
	}
	raw, err := json.Marshal(workbench)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.ProjectPath, stickyCodingMemoryFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if copied.ProjectPath == "" || copied.ProjectPath == source.ProjectPath {
		t.Fatalf("copy path = %q, source = %q", copied.ProjectPath, source.ProjectPath)
	}
	if copied.Name != "（副本）调研报告" {
		t.Fatalf("copy name = %q", copied.Name)
	}
	if copied.WorkingDir != filepath.Clean(workingDir) {
		t.Fatalf("copy working dir = %q, want %q", copied.WorkingDir, filepath.Clean(workingDir))
	}
	if !projectRecordHasTagLike(copied.Tags, taskCodingDevTag) || !projectRecordHasTagLike(copied.Tags, taskCopyTag) {
		t.Fatalf("copy tags = %#v, want coding_dev and task_copy", copied.Tags)
	}
	if projectRecordHasTagLike(copied.Tags, taskForkedTag) {
		t.Fatalf("copy tags = %#v, must not be a fork", copied.Tags)
	}
	if history := app.LoadProjectConversationHistory(copied.ProjectPath); len(history) != 0 {
		t.Fatalf("copy history = %+v, want empty", history)
	}
	if history := app.LoadProjectConversationHistory(source.ProjectPath); len(history) != 2 {
		t.Fatalf("source history len = %d, want 2", len(history))
	}

	copiedRaw, err := os.ReadFile(filepath.Join(copied.ProjectPath, stickyCodingMemoryFileName))
	if err != nil {
		t.Fatal(err)
	}
	var copiedWorkbench stickyCodingWorkbenchMemory
	if err := json.Unmarshal(copiedRaw, &copiedWorkbench); err != nil {
		t.Fatal(err)
	}
	if copiedWorkbench.RoutePref != "reasoning" || copiedWorkbench.PlanMode != "approve" || copiedWorkbench.SessionPermissionMode != "workspace" || copiedWorkbench.WorktreeMode != "off" {
		t.Fatalf("copied workbench settings = %+v", copiedWorkbench)
	}
	if copiedWorkbench.LastUserText != "" || copiedWorkbench.LastSummary != "" || copiedWorkbench.SessionPlan != "" || copiedWorkbench.TurnCount != 0 {
		t.Fatalf("copied workbench kept chat continuity: %+v", copiedWorkbench)
	}

	listed := app.ListTasks(20)
	if !searchResultsContainProjectPath(listed, source.ProjectPath) || !searchResultsContainProjectPath(listed, copied.ProjectPath) {
		t.Fatalf("task list = %+v, want source and copy", listed)
	}
}

func TestDuplicateTaskKeepsDefaultWorkspaceDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.createTaskRecordWithWorkingDir("未指定目录", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
	}, "", true)
	if source.ProjectPath == "" {
		t.Fatal("source task was not created")
	}
	sandbox := filepath.Join(source.ProjectPath, "workspace")
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("同一目录"), 0o644); err != nil {
		t.Fatal(err)
	}
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeProjectSessionPath(copied.WorkingDir); got != normalizeProjectSessionPath(sandbox) {
		t.Fatalf("copy working dir = %q, want %q", got, sandbox)
	}
	if !app.anotherVisibleTaskUsesWorkingDir(copied.ProjectPath, sandbox) {
		t.Fatal("closing the copy would cancel the source sandbox")
	}
	if err := app.DeleteTask(source.ProjectPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sandbox, "notes.txt"))
	if err != nil || string(data) != "同一目录" {
		t.Fatalf("shared workspace after source delete: %q err=%v", data, err)
	}
	if got := app.recentTaskWorkingDir(copied.ProjectPath); normalizeProjectSessionPath(got) != normalizeProjectSessionPath(sandbox) {
		t.Fatalf("copy working dir after source delete = %q", got)
	}
	if err := app.DeleteTask(copied.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sandbox); !os.IsNotExist(err) {
		t.Fatalf("shared workspace remains after the last task is deleted: %v", err)
	}
	if _, err := os.Stat(source.ProjectPath); !os.IsNotExist(err) {
		t.Fatalf("source task dir remains after the last task is deleted: %v", err)
	}
}

func TestDeleteDefaultWorkspaceCopyKeepsSourceSandbox(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.createTaskRecordWithWorkingDir("未指定目录", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
	}, "", true)
	sandbox := filepath.Join(source.ProjectPath, "workspace")
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("同一目录"), 0o644); err != nil {
		t.Fatal(err)
	}
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("用户目录"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit := app.createTaskRecordWithWorkingDir("指定目录", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
	}, outside, true)
	if err := app.DeleteTask(copied.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "notes.txt")); err != nil {
		t.Fatalf("deleting the copy removed the source sandbox: %v", err)
	}
	if err := app.DeleteTask(explicit.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("deleting a task removed its explicit working directory: %v", err)
	}
	inside := filepath.Join(app.GetDataDir(), "tasks", "hand-made-2024")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "keep.txt"), []byte("任务目录下的用户文件夹"), 0o644); err != nil {
		t.Fatal(err)
	}
	pinned := app.createTaskRecordWithWorkingDir("指向任务目录", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
	}, inside, true)
	if err := app.DeleteTask(pinned.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inside, "keep.txt")); err != nil {
		t.Fatalf("deleting a task removed a chosen folder inside the tasks directory: %v", err)
	}
}

func TestDuplicateTaskKeepsRemoteMetadata(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.createTaskRecordWithWorkingDir("远程巡检", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		taskRemoteCodingDevTag,
		taskRemoteHostTagPrefix + "10.0.0.8",
		taskRemoteUserTagPrefix + "ubuntu",
		taskRemotePortTagPrefix + "22",
		taskRemoteWorkDirTagPrefix + "/app",
		taskSourceRemoteOpsDiagnosisTag,
	}, "", true)
	if source.ProjectPath == "" {
		t.Fatal("source path empty")
	}

	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Name != "（副本）远程巡检" {
		t.Fatalf("name = %q", copied.Name)
	}
	for _, want := range []string{
		taskRemoteCodingDevTag,
		taskRemoteHostTagPrefix + "10.0.0.8",
		taskRemoteUserTagPrefix + "ubuntu",
		taskRemotePortTagPrefix + "22",
		taskRemoteWorkDirTagPrefix + "/app",
		taskSourceRemoteOpsDiagnosisTag,
		taskCopyTag,
	} {
		if !projectRecordHasTagLike(copied.Tags, want) {
			t.Fatalf("copy tags = %#v, missing %q", copied.Tags, want)
		}
	}
}

func TestDuplicateTaskCloudCopyStaysBesideSource(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy01"),
	}, workingDir, true)
	if source.ProjectPath == "" {
		t.Fatal("source path empty")
	}

	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !projectRecordHasTagLike(copied.Tags, cloudWorkspaceTag("cws_copy01")) {
		t.Fatalf("copy tags = %#v, want cloud workspace", copied.Tags)
	}
	listed := app.ListTasks(20)
	if !searchResultsContainProjectPath(listed, source.ProjectPath) || !searchResultsContainProjectPath(listed, copied.ProjectPath) {
		t.Fatalf("task list = %+v, want both cloud rows", listed)
	}

	app.hideOtherLocalCloudWorkspaceTasks("cws_copy01", source.ProjectPath)
	if !cloudTaskVisible(t, app, copied.ProjectPath) || !cloudTaskVisible(t, app, source.ProjectPath) {
		t.Fatal("hiding leftovers must keep the source and its copy")
	}
	app.hideOtherLocalCloudWorkspaceTasks("cws_copy01", copied.ProjectPath)
	if !cloudTaskVisible(t, app, source.ProjectPath) || !cloudTaskVisible(t, app, copied.ProjectPath) {
		t.Fatal("opening the copy must not hide the source")
	}
	if got := app.findVisibleCloudWorkspaceTask("cws_copy01").ProjectPath; got != source.ProjectPath {
		t.Fatalf("canonical cloud task = %q, want source %q", got, source.ProjectPath)
	}
	if got := app.cloudWorkspaceTaskProjectPath("cws_copy01"); got != source.ProjectPath {
		t.Fatalf("sidecar owner = %q, want source %q", got, source.ProjectPath)
	}
}

func TestCloudCopyDoesNotOwnSidecarAfterSourceHidden(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy02"),
	}, workingDir, true)
	if _, err := app.DuplicateTask(source.ProjectPath); err != nil {
		t.Fatal(err)
	}
	app.memoryStore.ProjectIndex().SetHidden(source.ProjectPath, true)
	if got := app.cloudWorkspaceTaskProjectPath("cws_copy02"); got != "" {
		t.Fatalf("hidden source must not leave the copy as sidecar owner, got %q", got)
	}
}

func TestDuplicateExpertTaskDoesNotStealResumeTarget(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.insertExpertTaskRecord("paper-review", "审稿专家", "")
	if source.ProjectPath == "" {
		t.Fatal("expert task path empty")
	}
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	pi := app.memoryStore.ProjectIndex()
	got, ok := app.newestVisibleExpertTask(pi, taskSourceExpertPrefix+"paper-review", false)
	if !ok || got.ProjectPath != source.ProjectPath {
		t.Fatalf("resume target = %q, want source %q (copy %q)", got.ProjectPath, source.ProjectPath, copied.ProjectPath)
	}
	if !projectRecordHasTagLike(copied.Tags, taskSourceExpertPrefix+"paper-review") {
		t.Fatalf("copy tags = %#v, want expert configuration", copied.Tags)
	}
}

func TestDeleteTaskCopyKeepsExpertTranscript(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.insertExpertTaskRecord("paper-review", "审稿专家", "")
	if source.ProjectPath == "" {
		t.Fatal("expert task path empty")
	}
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	app.imHandler = &IMMessageHandler{memory: mem}
	app.ensureRemoteInfra()
	hub := NewRemoteHubClient(app, app.remoteSessions)
	hub.imHandler = app.imHandler
	app.remoteSessions.SetHubClient(hub)

	expertOwner := expertSessionUserID("paper-review")
	mem.Save(expertOwner, []agent.ConversationEntry{{Role: "user", Content: "原稿意见"}})
	if err := app.ensureProjectTabSessionPersist().SaveSession(&TabSessionData{
		TabID:        expertTabSessionID("paper-review"),
		Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "原稿意见"}},
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteTask(copied.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if entries := mem.Load(expertOwner); len(entries) != 1 || entries[0].Content != "原稿意见" {
		t.Fatalf("expert transcript after deleting copy = %+v", entries)
	}
	if session, err := app.ensureProjectTabSessionPersist().LoadSession(expertTabSessionID("paper-review")); err != nil || session == nil {
		t.Fatalf("expert tab session = %v err=%v", session, err)
	}
	if app.memoryStore.ProjectIndex().Get(source.ProjectPath) == nil {
		t.Fatal("deleting the copy removed the source task")
	}
}

func TestDeleteCloudCopyKeepsSharedWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy03"),
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteTask(copied.ProjectPath); err != nil {
		t.Fatal(err)
	}
	if !cloudTaskVisible(t, app, source.ProjectPath) {
		t.Fatal("deleting the copy hid the source")
	}
	if _, dismissed := app.loadDismissedCloudWorkspaceTasks()["cws_copy03"]; dismissed {
		t.Fatal("deleting the copy dismissed the shared workspace")
	}
	if err := app.flushCloudWorkspaceSession(context.Background(), "cws_copy03", copied.ProjectPath); err != nil {
		t.Fatalf("copy session flush should stay local, err=%v", err)
	}
}

func TestRestoreCloudWorkspaceRecreatesCanonicalBehindCopy(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy06"),
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	app.ensureMemoryStore()
	if pi := app.memoryStore.ProjectIndex(); pi != nil {
		pi.SetHidden(source.ProjectPath, true)
	}
	found := app.findCanonicalCloudWorkspaceTask("cws_copy06", true)
	if found.ProjectPath != source.ProjectPath {
		t.Fatalf("hidden canonical = %q, want %q", found.ProjectPath, source.ProjectPath)
	}
	if err := app.DeleteTask(source.ProjectPath); err != nil {
		t.Fatal(err)
	}
	restored := app.restoreCloudWorkspaceTaskRecord(CloudWorkspaceEntitlementWorkspace{
		ID: "cws_copy06", Name: "经营材料",
	}, "经营材料", "")
	if restored.ProjectPath == "" || restored.ProjectPath == copied.ProjectPath || projectResultIsTaskCopy(restored) {
		t.Fatalf("restored = %+v, want a new canonical row", restored)
	}
	if !cloudTaskVisible(t, app, copied.ProjectPath) {
		t.Fatal("restoring the canonical hid the copy")
	}
}

func TestRenameCloudCopyKeepsCanonicalWorkspaceTitle(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy07"),
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	rememberCloudWorkspaceTask("cws_copy07", source)
	if got := app.RenameTask(copied.ProjectPath, "草稿"); got != "草稿" {
		t.Fatalf("copy rename = %q", got)
	}
	cached, ok := lookupCloudWorkspaceTask("cws_copy07")
	if !ok || normalizeProjectSessionPath(cached.ProjectPath) != normalizeProjectSessionPath(source.ProjectPath) || cached.Name != source.Name {
		t.Fatalf("cached workspace task = %+v, want source name %q", cached, source.Name)
	}
	payload := app.cloudWorkspaceTaskSidecarPayload("cws_copy07")
	if payload.Name != source.Name {
		t.Fatalf("sidecar name = %q, want %q", payload.Name, source.Name)
	}
	if err := app.DeleteTask(source.ProjectPath); err != nil {
		t.Fatal(err)
	}
	payload = app.cloudWorkspaceTaskSidecarPayload("cws_copy07")
	if payload.Name != "" || payload.Mode != "" {
		t.Fatalf("copy-only sidecar = %+v", payload)
	}
	if pi := app.memoryStore.ProjectIndex(); pi == nil || pi.GetDisplayName(copied.ProjectPath) != "草稿" {
		t.Fatal("copy display name was not renamed")
	}
}

func TestKeepCloudWorkspacePendingSidecarsAcrossCopyPrepare(t *testing.T) {
	const workspaceID = "cws_copy08"
	t.Cleanup(func() { takeCloudWorkspacePendingSidecars(workspaceID) })
	original := &TabSessionData{InputText: "canonical draft"}
	storeCloudWorkspacePendingSidecars(workspaceID, cloudWorkspaceSidecarBundle{Session: original})
	err := keepCloudWorkspacePendingSidecars(workspaceID, true, func() error {
		storeCloudWorkspacePendingSidecars(workspaceID, cloudWorkspaceSidecarBundle{Session: &TabSessionData{InputText: "copy fetch"}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := peekCloudWorkspacePendingSidecars(workspaceID)
	if !ok || got.Session == nil || got.Session.InputText != "canonical draft" {
		t.Fatalf("pending session = %+v", got.Session)
	}
}

func TestCloseCopyTabKeepsSharedCloudWorkspaceLease(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy04"),
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	app.tabProjectPaths.Store("tab-source", source.ProjectPath)
	app.tabProjectPaths.Store("tab-copy", copied.ProjectPath)
	app.tabProjectPaths.Delete("tab-copy")
	if !app.anotherOpenTabUsesCloudWorkspace("cws_copy04", copied.ProjectPath) {
		t.Fatal("source tab should keep the shared workspace mounted")
	}
	if !app.anotherVisibleTaskUsesWorkingDir(copied.ProjectPath, workingDir) {
		t.Fatal("copy should not cancel the source working directory")
	}
	if !app.anotherVisibleTaskUsesWorkingDir(source.ProjectPath, workingDir) {
		t.Fatal("source should not cancel the copy working directory")
	}
	app.tabProjectPaths.Delete("tab-source")
	if app.anotherOpenTabUsesCloudWorkspace("cws_copy04", copied.ProjectPath) {
		t.Fatal("workspace should release once the last tab closes")
	}
}

func TestRefreshCloudSidecarsLeavesCanonicalBundleOnCopy(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-cache")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("经营材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_copy05"),
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	keep := &TabSessionData{Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "原稿"}}}
	storeCloudWorkspacePendingSidecars("cws_copy05", cloudWorkspaceSidecarBundle{Session: keep})
	t.Cleanup(func() { takeCloudWorkspacePendingSidecars("cws_copy05") })
	app.refreshCloudWorkspaceSidecars("cws_copy05", copied.ProjectPath)
	app.refreshCloudWorkspaceSessionSidecar("cws_copy05", copied.ProjectPath)
	bundle, ok := peekCloudWorkspacePendingSidecars("cws_copy05")
	if !ok || bundle.Session == nil || len(bundle.Session.Conversation) != 1 {
		t.Fatalf("pending sidecar after copy refresh = ok:%v session:%v", ok, bundle.Session)
	}
}

func TestDuplicateTaskRejectsMissingSource(t *testing.T) {
	app := newProjectSearchTestApp(t)
	if _, err := app.DuplicateTask(""); err == nil {
		t.Fatal("empty path should fail")
	}
	if _, err := app.DuplicateTask(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing task should fail")
	}
}

func TestShouldCopyTaskConfigTagDropsChatLineage(t *testing.T) {
	if shouldCopyTaskConfigTag(taskForkedTag) || shouldCopyTaskConfigTag(taskSavedConversationTag) {
		t.Fatal("fork and saved conversation tags must not be copied")
	}
	if shouldCopyTaskConfigTag(taskSourceTagPrefix + `D:\tasks\source`) {
		t.Fatal("source path lineage must not be copied")
	}
	if shouldCopyTaskConfigTag(taskSourceAssistantTabPrefix + "acp:abc") {
		t.Fatal("assistant tab identity must not be copied")
	}
	if !shouldCopyTaskConfigTag(taskSourceExpertPrefix+"paper-review") || !shouldCopyTaskConfigTag(latexEntryTagPrefix+"main.tex") {
		t.Fatal("expert and latex entry are configuration")
	}
	if !shouldCopyTaskConfigTag(cloudWorkspaceShareReadTagPrefix) || !shouldCopyTaskConfigTag(cloudWorkspaceSharedFromPrefix+"owner@example.com") {
		t.Fatal("cloud share permission is configuration")
	}
	if shouldCopyTaskConfigTag(cloudWorkspaceSharedFromPrefix) {
		t.Fatal("empty share owner must not be copied")
	}
}

func TestDuplicateTaskKeepsReadOnlyCloudShare(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "cloud-read")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := app.createTaskRecordWithWorkingDir("分享材料", "", []string{
		taskManagementTag,
		taskUserCreatedTag,
		cloudWorkspaceTag("cws_share01"),
		cloudWorkspaceShareReadTagPrefix,
		cloudWorkspaceSharedFromPrefix + "owner@example.com",
	}, workingDir, true)
	copied, err := app.DuplicateTask(source.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cloudWorkspaceTaskIsReadShare(copied) {
		t.Fatalf("copy tags = %#v, want read-only share", copied.Tags)
	}
	if !projectRecordHasTagLike(copied.Tags, cloudWorkspaceSharedFromPrefix+"owner@example.com") {
		t.Fatalf("copy tags = %#v, want share owner", copied.Tags)
	}
}

func cloudTaskVisible(t *testing.T, app *App, projectPath string) bool {
	t.Helper()
	pi := app.memoryStore.ProjectIndex()
	if pi == nil {
		t.Fatal("project index missing")
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return false
	}
	return !pi.IsHidden(projectPath) && !pi.IsArchived(projectPath)
}

func TestCollapseKeepsExplicitCloudCopies(t *testing.T) {
	source := memory.ProjectRecord{
		ProjectPath: "D:/tasks/source",
		Name:        "经营材料",
		Tags:        []string{taskManagementTag, cloudWorkspaceTag("cws_copy01")},
	}
	copy := memory.ProjectRecord{
		ProjectPath: "D:/tasks/copy",
		Name:        "（副本）经营材料",
		Tags:        []string{taskManagementTag, cloudWorkspaceTag("cws_copy01"), taskCopyTag},
	}
	got := collapseDuplicateCloudWorkspaceRecords([]memory.ProjectRecord{source, copy})
	if len(got) != 2 {
		t.Fatalf("collapsed = %+v, want source and copy", got)
	}
}
