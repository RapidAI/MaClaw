package guiapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCloudWorkspaceClientUsesHubIssuedInstanceSession(t *testing.T) {
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	var issueCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceInstanceSessionsPath:
			issueCount++
			if got := r.Header.Get("X-Cloud-Workspace-Instance"); got != "" {
				t.Errorf("client must not assert its own instance id, got %q", got)
			}
			if got := r.Header.Get("X-Cloud-Workspace-Protocol"); got != cloudWorkspaceProtocolVersion {
				t.Errorf("protocol=%q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"session_id":"cwses_server","session_token":"cwst_server","client_instance_id":"cwi_server","protocol":"v1-sequential","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceCollectionPath:
			if got := r.Header.Get(cloudWorkspaceInstanceSessionHeader); got != "cwst_server" {
				t.Errorf("instance session=%q", got)
			}
			if got := r.Header.Get("X-Cloud-Workspace-Instance"); got != "" {
				t.Errorf("legacy instance override=%q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"cws_new","name":"A","status":"active"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateCloudWorkspace("A"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateCloudWorkspace("A"); err != nil {
		t.Fatal(err)
	}
	if issueCount != 1 {
		t.Fatalf("session issue count=%d, want one cached process session", issueCount)
	}
}

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

func TestPurgeCloudWorkspaceLocalCachesRemovesWriterAndReadOnlyCopies(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	workspaceID := "cws-purge-copies"
	writer := app.cloudWorkspaceCachePath("tenant_default", workspaceID)
	readonly := filepath.Join(app.GetDataDir(), "cloud-workspaces-readonly", "tenant_other", workspaceID, "instance-1")
	if err := os.MkdirAll(writer, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(readonly, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(writer, "secret.txt"), []byte("writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readonly, "secret.txt"), []byte("readonly"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.purgeCloudWorkspaceLocalCaches(workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(writer); !os.IsNotExist(err) {
		t.Fatalf("writer cache remains: err=%v", err)
	}
	if _, err := os.Stat(readonly); !os.IsNotExist(err) {
		t.Fatalf("read-only cache remains: err=%v", err)
	}
}

func TestPurgeCloudWorkspaceLocalCachesRejectsTenantSymlink(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := filepath.Join(app.GetDataDir(), "cloud-workspaces")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("must remain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "tenant_link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := app.purgeCloudWorkspaceLocalCaches("cws-symlink"); err == nil {
		t.Fatal("purge must fail closed on a tenant symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatalf("purge followed tenant symlink and removed outside data: %v", err)
	}
}

func TestForceDeleteCloudWorkspace404StillPurgesLocalCopies(t *testing.T) {
	var purgeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == cloudWorkspaceCollectionPath+"/cws-gone/purge" {
			purgeCalls++
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	workspaceID := "cws-gone"
	writer := app.cloudWorkspaceCachePath("tenant_default", workspaceID)
	readonly := filepath.Join(app.GetDataDir(), "cloud-workspaces-readonly", "tenant_other", workspaceID, "instance-1")
	for _, dir := range []string{writer, readonly} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("sensitive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := app.ForceDeleteCloudWorkspace(workspaceID); err != nil {
		t.Fatalf("ForceDeleteCloudWorkspace should treat remote 404 as gone: %v", err)
	}
	if purgeCalls != 1 {
		t.Fatalf("purge calls=%d, want 1", purgeCalls)
	}
	for name, path := range map[string]string{"writer": writer, "readonly": readonly} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s cache remains: err=%v", name, err)
		}
	}
}

func TestForceDeleteCloudWorkspaceRefusesWhenLocalReleaseFails(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	var purgeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/purge") {
			purgeCalls++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()

	resetCloudWorkspaceMounts()
	t.Cleanup(resetCloudWorkspaceMounts)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	app := newProjectSearchTestApp(t)
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test", RemoteTenantID: "tenant_acme",
	}); err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceInstanceSession(server.URL, "machine-token", "machine-test")
	created := mustCreateCloudWorkspaceTask(t, app, "不可丢失任务", "", "coding_dev", "cws-release-fail")
	dirty := filepath.Join(created.WorkingDir, "unsynced.txt")
	if err := os.WriteFile(dirty, []byte("must survive failed release"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.failPush = true
	hub.mu.Unlock()

	err := app.ForceDeleteCloudWorkspace("cws-release-fail")
	if err == nil {
		t.Fatal("ForceDeleteCloudWorkspace must fail when final local push fails")
	}
	if purgeCalls != 0 {
		t.Fatalf("remote purge called after local release failure: %d", purgeCalls)
	}
	if _, statErr := os.Stat(dirty); statErr != nil {
		t.Fatalf("unsynced local file was removed: %v", statErr)
	}
	if mount := lookupHeldCloudWorkspace("cws-release-fail"); mount == nil {
		t.Fatal("failed release must keep the mount recoverable")
	}
}

func TestForceDeleteCloudWorkspaceStopsMountedCacheWithoutTaskRow(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	var purgeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/purge") {
			purgeCalls++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()

	resetCloudWorkspaceMounts()
	t.Cleanup(resetCloudWorkspaceMounts)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	app := newProjectSearchTestApp(t)
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test", RemoteTenantID: "tenant_acme",
	}); err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceInstanceSession(server.URL, "machine-token", "machine-test")
	prepared, err := app.PrepareCloudWorkspace("cws-mounted-only")
	if err != nil {
		t.Fatalf("PrepareCloudWorkspace: %v", err)
	}
	if _, ok := lookupCloudWorkspaceTask("cws-mounted-only"); ok {
		t.Fatal("prepare-only flow unexpectedly created a task row")
	}
	if err := app.ForceDeleteCloudWorkspace("cws-mounted-only"); err != nil {
		t.Fatalf("ForceDeleteCloudWorkspace: %v", err)
	}
	if purgeCalls != 1 {
		t.Fatalf("purge calls=%d, want one", purgeCalls)
	}
	if lookupHeldCloudWorkspace("cws-mounted-only") != nil {
		t.Fatal("mounted cache was not released before purge")
	}
	if _, err := os.Stat(prepared.LocalPath); !os.IsNotExist(err) {
		t.Fatalf("mounted cache remains after purge: err=%v", err)
	}
}

func TestDeleteCloudWorkspaceRefusesWhenLocalReleaseFails(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	var deleteCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == cloudWorkspaceCollectionPath+"/cws-delete-release-fail" {
			deleteCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cws-delete-release-fail","name":"任务","status":"deleted","deleted_at":"2026-09-01T00:00:00Z"}`))
			return
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()

	resetCloudWorkspaceMounts()
	t.Cleanup(resetCloudWorkspaceMounts)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	app := newProjectSearchTestApp(t)
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test", RemoteTenantID: "tenant_acme",
	}); err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceInstanceSession(server.URL, "machine-token", "machine-test")
	created := mustCreateCloudWorkspaceTask(t, app, "不可丢失删除任务", "", "coding_dev", "cws-delete-release-fail")
	dirty := filepath.Join(created.WorkingDir, "unsynced.txt")
	if err := os.WriteFile(dirty, []byte("must survive failed release"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.failPush = true
	hub.mu.Unlock()

	if _, err := app.DeleteCloudWorkspace("cws-delete-release-fail"); err == nil {
		t.Fatal("DeleteCloudWorkspace must fail when final local push fails")
	}
	if deleteCalls != 0 {
		t.Fatalf("remote soft delete called after local release failure: %d", deleteCalls)
	}
	if _, err := os.Stat(dirty); err != nil {
		t.Fatalf("unsynced local file was removed: %v", err)
	}
	if lookupHeldCloudWorkspace("cws-delete-release-fail") == nil {
		t.Fatal("failed release must keep the mount recoverable")
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

func TestSyncCloudWorkspaceFilesPullsManifestWhenAlreadyHeld(t *testing.T) {
	body := []byte("from another machine")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_browse")
	if lookupHeldCloudWorkspace("cws_browse") == nil {
		t.Fatal("create should hold the mount")
	}
	notes := filepath.Join(created.WorkingDir, "notes.md")
	if _, err := os.Stat(notes); !os.IsNotExist(err) {
		t.Fatalf("notes.md should be absent before browse sync: %v", err)
	}
	hub.mu.Lock()
	hub.objects = map[string][]byte{sum: body}
	hub.revision = "rev-notes"
	hub.entries = []cloudWorkspaceManifestEntry{{Path: "notes.md", SHA256: sum, Size: int64(len(body))}}
	hub.mu.Unlock()

	hub.mu.Lock()
	leasesBefore := hub.leaseAcquires
	hub.mu.Unlock()
	prepared, err := app.SyncCloudWorkspaceFiles("cws_browse")
	if err != nil {
		t.Fatalf("SyncCloudWorkspaceFiles: %v", err)
	}
	if prepared.LocalPath != created.WorkingDir {
		t.Fatalf("local=%q want %q", prepared.LocalPath, created.WorkingDir)
	}
	got, err := os.ReadFile(notes)
	if err != nil || string(got) != string(body) {
		t.Fatalf("browse sync should pull already-held files: notes.md=%q err=%v", got, err)
	}
	hub.mu.Lock()
	leasesAfter := hub.leaseAcquires
	hub.mu.Unlock()
	if leasesAfter != leasesBefore {
		t.Fatalf("held browse sync should not re-acquire lease, before=%d after=%d", leasesBefore, leasesAfter)
	}

	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_browse", created.ProjectPath)
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}
}

func TestSyncCloudWorkspaceFilesPullsManifestWhenEventsEmpty(t *testing.T) {
	body := []byte("manifest only")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_manifest")
	readme := filepath.Join(created.WorkingDir, "readme.md")
	if _, err := os.Stat(readme); !os.IsNotExist(err) {
		t.Fatalf("readme.md should be absent before browse sync: %v", err)
	}
	hub.mu.Lock()
	hub.revision = "rev-manifest"
	hub.entries = []cloudWorkspaceManifestEntry{{Path: "readme.md", SHA256: sum, Size: int64(len(body))}}
	hub.objects = map[string][]byte{sum: body}
	hub.mu.Unlock()
	if _, err := app.SyncCloudWorkspaceFiles("cws_manifest"); err != nil {
		t.Fatalf("SyncCloudWorkspaceFiles: %v", err)
	}
	got, err := os.ReadFile(readme)
	if err != nil || string(got) != string(body) {
		t.Fatalf("empty event log should still pull the remote tree: readme.md=%q err=%v", got, err)
	}
}

func TestSyncCloudWorkspaceFilesIgnoresLegacyEventsEndpoint(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted, failEvents: true}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_events_fail")
	prepared, err := app.SyncCloudWorkspaceFiles("cws_events_fail")
	if err != nil {
		t.Fatalf("manifest browse sync should not depend on legacy events: %v", err)
	}
	if prepared.LocalPath != created.WorkingDir {
		t.Fatalf("local=%q want %q", prepared.LocalPath, created.WorkingDir)
	}
}

func TestResumeCloudWorkspaceTaskDoesNotPullWhenAlreadyHeld(t *testing.T) {
	body := []byte("should stay remote until browse")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_resume_nopull")
	hub.mu.Lock()
	hub.objects = map[string][]byte{sum: body}
	hub.events = []cloudWorkspaceEvent{{
		Seq:             1,
		OpID:            "op_notes",
		Path:            "notes.md",
		Kind:            "put",
		NewFileRevision: "fr-notes",
		ObjectSHA256:    sum,
	}}
	hub.mu.Unlock()
	_ = mustResumeCloudWorkspaceTask(t, app, "cws_resume_nopull", created.ProjectPath)
	if _, err := os.Stat(filepath.Join(created.WorkingDir, "notes.md")); !os.IsNotExist(err) {
		t.Fatal("tab reopen must not pull remote files; use SyncCloudWorkspaceFiles / Browse")
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

func TestCloudWorkspaceAPIErrorBandwidthCarriesRetryAfter(t *testing.T) {
	err := cloudWorkspaceAPIError(429, []byte(`{"code":"CLOUD_WORKSPACE_BANDWIDTH","message":"limited","retry_after_seconds":123}`))
	if err == nil || !strings.Contains(err.Error(), "带宽") {
		t.Fatalf("err=%v want 带宽 message", err)
	}
	delay, ok := cloudWorkspaceBandwidthRetryDelay(err)
	if !ok || delay != 123*time.Second {
		t.Fatalf("delay=%v ok=%v want 123s", delay, ok)
	}
	wrapped := fmt.Errorf("push: %w", err)
	if _, ok := cloudWorkspaceBandwidthRetryDelay(wrapped); !ok {
		t.Fatalf("wrapped error must still match bandwidth retry")
	}
	// Missing retry_after falls back to a bounded one-minute default.
	fallback := cloudWorkspaceAPIError(429, []byte(`{"code":"CLOUD_WORKSPACE_BANDWIDTH"}`))
	delay, ok = cloudWorkspaceBandwidthRetryDelay(fallback)
	if !ok || delay != time.Minute {
		t.Fatalf("fallback delay=%v ok=%v want 1m", delay, ok)
	}
	// Unrelated errors never enter the cooldown path.
	if _, ok := cloudWorkspaceBandwidthRetryDelay(fmt.Errorf("boom")); ok {
		t.Fatalf("plain error must not match bandwidth retry")
	}
	// The delay is clamped to [1s, 1h] even for out-of-range server values.
	huge := cloudWorkspaceAPIError(429, []byte(`{"code":"CLOUD_WORKSPACE_BANDWIDTH","retry_after_seconds":7200}`))
	if delay, _ := cloudWorkspaceBandwidthRetryDelay(huge); delay != time.Hour {
		t.Fatalf("clamped delay=%v want 1h", delay)
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
