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

func TestPrepareAndCreateTaskWithCloudWorkspaceMock(t *testing.T) {
	resetCloudWorkspaceDialogMocks()
	t.Cleanup(resetCloudWorkspaceDialogMocks)

	app := newProjectSearchTestApp(t)
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

	if got := app.ResumeCloudWorkspaceTask("cws_demo"); got.ProjectPath != "" {
		t.Fatalf("resume before create=%+v", got)
	}

	created := app.CreateTaskWithCloudWorkspace("云端任务", "", "coding_dev", "cws_demo")
	if created.ProjectPath == "" {
		t.Fatalf("CreateTaskWithCloudWorkspace returned empty task")
	}
	if created.WorkingDir != prepared.LocalPath {
		t.Fatalf("working_dir=%q want %q", created.WorkingDir, prepared.LocalPath)
	}

	resumed := app.ResumeCloudWorkspaceTask("cws_demo")
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}

	empty := app.CreateTaskWithCloudWorkspace("x", "", "", "")
	if empty.ProjectPath != "" {
		t.Fatalf("empty workspace id should not create: %+v", empty)
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

func TestDecodeCloudWorkspaceHubRowJSON(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id": "cws_1", "name": "n", "used_bytes": 12, "updated_at": "t",
	})
	row, err := decodeCloudWorkspaceHubRow(raw)
	if err != nil || row.ID != "cws_1" || row.UsedBytes != 12 {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}
