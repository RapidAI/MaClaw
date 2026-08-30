package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCreateRenameDeleteRestoreCloudWorkspaceHub(t *testing.T) {
	resetCloudWorkspaceDialogMocks()
	t.Cleanup(resetCloudWorkspaceDialogMocks)

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceCollectionPath:
			if got := r.Header.Get("Authorization"); got != "Bearer machine-token" {
				t.Errorf("Authorization=%q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"cws_new","name":"工作区 1","status":"active","used_bytes":0,"created_at":"2026-08-28T10:00:00Z","updated_at":"2026-08-28T10:00:00Z"}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, cloudWorkspaceCollectionPath+"/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"cws_new","name":"标书项目","status":"active","used_bytes":0,"updated_at":"2026-08-28T11:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == cloudWorkspaceCollectionPath+"/cws_new":
			_, _ = w.Write([]byte(`{"id":"cws_new","name":"标书项目","status":"deleted","used_bytes":0,"updated_at":"2026-08-28T12:00:00Z","deleted_at":"2026-08-28T12:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceCollectionPath+"/cws_new/restore":
			_, _ = w.Write([]byte(`{"id":"cws_new","name":"标书项目","status":"active","used_bytes":0,"updated_at":"2026-08-28T13:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	ws, err := app.CreateCloudWorkspace("")
	if err != nil {
		t.Fatalf("CreateCloudWorkspace: %v", err)
	}
	if ws.ID != "cws_new" || ws.Name != "工作区 1" {
		t.Fatalf("created=%+v", ws)
	}
	created.ID = ws.ID

	renamed, err := app.RenameCloudWorkspace(created.ID, "标书项目")
	if err != nil {
		t.Fatalf("RenameCloudWorkspace: %v", err)
	}
	if renamed.Name != "标书项目" {
		t.Fatalf("renamed=%+v", renamed)
	}

	deleted, err := app.DeleteCloudWorkspace(created.ID)
	if err != nil {
		t.Fatalf("DeleteCloudWorkspace: %v", err)
	}
	if deleted.ID != "cws_new" || deleted.DeletedAt == "" || deleted.PurgeAfter == "" {
		t.Fatalf("deleted=%+v", deleted)
	}

	restored, err := app.RestoreCloudWorkspace(created.ID)
	if err != nil {
		t.Fatalf("RestoreCloudWorkspace: %v", err)
	}
	if restored.ID != "cws_new" || restored.Name != "标书项目" {
		t.Fatalf("restored=%+v", restored)
	}
}

func TestCreateCloudWorkspaceQuotaError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"code":"CLOUD_WORKSPACE_QUOTA","message":"cloud workspace quota exceeded"}`))
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	_, err := app.CreateCloudWorkspace("x")
	if err == nil || !strings.Contains(err.Error(), "配额") {
		t.Fatalf("err=%v", err)
	}
}

func mustCreateCloudWorkspaceTask(t *testing.T, app *App, name, workingDir, mode, workspaceID string) ProjectSearchResult {
	t.Helper()
	created, err := app.CreateTaskWithCloudWorkspace(name, workingDir, mode, workspaceID)
	if err != nil {
		t.Fatalf("CreateTaskWithCloudWorkspace: %v", err)
	}
	if created.ProjectPath == "" {
		t.Fatal("CreateTaskWithCloudWorkspace returned empty task")
	}
	return created
}

func mustResumeCloudWorkspaceTask(t *testing.T, app *App, workspaceID string, projectPath ...string) ProjectSearchResult {
	t.Helper()
	preferred := ""
	if len(projectPath) > 0 {
		preferred = projectPath[0]
	}
	got, err := app.ResumeCloudWorkspaceTask(workspaceID, preferred)
	if err != nil {
		t.Fatalf("ResumeCloudWorkspaceTask: %v", err)
	}
	return got
}

func TestPrepareAndCreateTaskWithCloudWorkspaceMock(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	prepared, err := app.PrepareCloudWorkspace("cws_demo")
	if err != nil {
		t.Fatalf("PrepareCloudWorkspace: %v", err)
	}
	if prepared.WorkspaceID != "cws_demo" {
		t.Fatalf("workspace id=%q", prepared.WorkspaceID)
	}
	info, err := os.Stat(prepared.LocalPath)
	if err != nil || !info.IsDir() {
		t.Fatalf("local path %q: %v", prepared.LocalPath, err)
	}
	again, err := app.PrepareCloudWorkspace("cws_demo")
	if err != nil {
		t.Fatalf("PrepareCloudWorkspace reuse: %v", err)
	}
	if again.LocalPath != prepared.LocalPath {
		t.Fatalf("expected reused dir, got %q vs %q", again.LocalPath, prepared.LocalPath)
	}

	if got := mustResumeCloudWorkspaceTask(t, app, "cws_demo"); got.ProjectPath != "" {
		t.Fatalf("resume before create=%+v", got)
	}

	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_demo")
	if created.WorkingDir != prepared.LocalPath {
		t.Fatalf("working_dir=%q want %q", created.WorkingDir, prepared.LocalPath)
	}
	if !projectRecordHasTagLike(created.Tags, cloudWorkspaceTag("cws_demo")) {
		t.Fatalf("missing cloud_workspace tag: %v", created.Tags)
	}

	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_demo")
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}

	empty, err := app.CreateTaskWithCloudWorkspace("x", "", "", "")
	if err == nil {
		t.Fatal("empty workspace id should error")
	}
	if empty.ProjectPath != "" {
		t.Fatalf("empty workspace id should not create: %+v", empty)
	}
}

func TestHideTaskDropsCloudWorkspaceResumeMap(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_hide")
	if got := mustResumeCloudWorkspaceTask(t, app, "cws_hide"); got.ProjectPath != created.ProjectPath {
		t.Fatalf("resume before hide=%q want %q", got.ProjectPath, created.ProjectPath)
	}

	app.HideTask(created.ProjectPath)
	if got := mustResumeCloudWorkspaceTask(t, app, "cws_hide"); got.ProjectPath != "" {
		t.Fatalf("resume after hide=%q, want empty so a replacement can be created", got.ProjectPath)
	}

	replacement := mustCreateCloudWorkspaceTask(t, app, "云端任务重试", "", "coding_dev", "cws_hide")
	if replacement.ProjectPath == created.ProjectPath {
		t.Fatalf("replacement reused hidden path %q", replacement.ProjectPath)
	}
	if got := mustResumeCloudWorkspaceTask(t, app, "cws_hide"); got.ProjectPath != replacement.ProjectPath {
		t.Fatalf("resume after replacement=%q want %q", got.ProjectPath, replacement.ProjectPath)
	}
}

func TestDeleteCloudWorkspaceTreats404AsGone(t *testing.T) {
	resetCloudWorkspaceDialogMocks()
	t.Cleanup(resetCloudWorkspaceDialogMocks)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	deleted, err := app.DeleteCloudWorkspace("cws_missing")
	if err != nil {
		t.Fatalf("404 should be treated as already gone: %v", err)
	}
	if deleted.ID != "cws_missing" || deleted.DeletedAt == "" {
		t.Fatalf("deleted=%+v", deleted)
	}
}

func TestCloudWorkspaceMutateRejectsMissingID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"x"}`))
	}))
	defer server.Close()
	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	_, err := app.CreateCloudWorkspace("x")
	if err == nil || !strings.Contains(err.Error(), "missing id") {
		t.Fatalf("err=%v", err)
	}
}

func TestCloudWorkspaceAPIErrorChineseCodes(t *testing.T) {
	cases := map[string]string{
		"CLOUD_WORKSPACE_QUOTA":             "配额",
		"CLOUD_WORKSPACE_FORBIDDEN":         "未开通",
		"CLOUD_WORKSPACE_LEASE_REQUIRED":    "租约",
		"CLOUD_WORKSPACE_REVISION_CONFLICT": "版本冲突",
		"CLOUD_WORKSPACE_VOLUME_FULL":       "存储空间",
		"CLOUD_WORKSPACE_SIZE":              "容量",
		"CLOUD_WORKSPACE_TENANT_DISK":       "总容量",
		"CLOUD_WORKSPACE_IN_USE":            "占用中",
	}
	for code, want := range cases {
		err := cloudWorkspaceAPIError(409, []byte(`{"code":"`+code+`","message":"english"}`))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("code=%s err=%v want substring %q", code, err, want)
		}
	}
}

func TestDecodeCloudWorkspaceHubRowJSON(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id": "cws_1", "name": "n", "used_bytes": 12, "updated_at": "t",
	})
	row, err := decodeCloudWorkspaceHubRow(raw)
	if err != nil || row.ID != "cws_1" || row.UsedBytes != 12 {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}
