package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudWorkspaceSidecarFlushOnRelease(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "标书项目", "", "coding_dev", "cws_sidecar")
	sticky := []byte(`{"kind":"local","goal":"finish bid"}`)
	checkpoint := []byte(`{"tasks":[{"title":"impl"}]}`)
	if err := os.WriteFile(filepath.Join(created.ProjectPath, stickyCodingMemoryFileName), sticky, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.ProjectPath, codingExecCheckpointFileName), checkpoint, 0o644); err != nil {
		t.Fatal(err)
	}
	decoy := []byte(`{"kind":"decoy"}`)
	if err := os.WriteFile(filepath.Join(created.WorkingDir, stickyCodingMemoryFileName), decoy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorkingDir, "src.txt"), []byte("code"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(created.WorkingDir, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorkingDir, cloudWorkspaceCacheStateDir, "draft.json"), []byte(`{"local":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ensureProjectTabSessionPersist().SaveSession(&TabSessionData{
		TabID:        "tab-sidecar",
		ProjectPath:  created.ProjectPath,
		Conversation: []interface{}{map[string]any{"role": "user", "content": "continue the bid"}},
		InputText:    "next step",
	}); err != nil {
		t.Fatal(err)
	}
	index, err := app.ensureProjectTabSessionPersist().LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	index.Tabs = append(index.Tabs, TabIndexEntry{ID: "tab-sidecar", Type: "project", Title: created.Name, ProjectPath: created.ProjectPath, LastActiveAt: 1})
	if err := app.ensureProjectTabSessionPersist().SaveIndex(index); err != nil {
		t.Fatal(err)
	}

	if err := app.ReleaseCloudWorkspace("cws_sidecar"); err != nil {
		t.Fatalf("release: %v", err)
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if string(hub.sidecars[cloudWorkspaceSidecarWorkbench]) != string(sticky) {
		t.Fatalf("sticky sidecar=%q (must come from projectPath, not workingDir)", hub.sidecars[cloudWorkspaceSidecarWorkbench])
	}
	if string(hub.sidecars[cloudWorkspaceSidecarCheckpoint]) != string(checkpoint) {
		t.Fatalf("checkpoint sidecar=%q", hub.sidecars[cloudWorkspaceSidecarCheckpoint])
	}
	assertCloudWorkspaceSessionSidecar(t, hub.sidecars[cloudWorkspaceSidecarSession], "continue the bid")
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatal(err)
	}
	if task.Name != "标书项目" || task.Mode != taskCodingDevTag || task.Tag != cloudWorkspaceTag("cws_sidecar") {
		t.Fatalf("task sidecar=%+v", task)
	}
	for _, e := range hub.entries {
		if e.Path == cloudWorkspaceSidecarSession || e.Path == cloudWorkspaceSidecarTask || e.Path == cloudWorkspaceSidecarWorkbench || e.Path == cloudWorkspaceSidecarCheckpoint {
			t.Fatalf("sidecar channel leaked into file manifest: %+v", hub.entries)
		}
		if strings.HasPrefix(e.Path, cloudWorkspaceCacheStateDir+"/") {
			t.Fatalf("maclaw-cloud draft in manifest: %+v", hub.entries)
		}
	}
}

func TestMergeCloudWorkspaceSessionHistoryKeepsBothMachines(t *testing.T) {
	local := &TabSessionData{Conversation: []interface{}{
		map[string]any{"id": "a", "role": "user", "timestamp": 20.0},
		map[string]any{"id": "dup", "role": "assistant", "timestamp": 30.0},
	}, InputText: "local draft"}
	remote := &TabSessionData{Conversation: []interface{}{
		map[string]any{"id": "dup", "role": "assistant", "timestamp": 30.0},
		map[string]any{"id": "b", "role": "user", "timestamp": 10.0},
	}, InputText: ""}
	merged := mergeCloudWorkspaceSessionHistory(local, remote)
	if len(merged.Conversation) != 3 || merged.InputText != "local draft" {
		t.Fatalf("merged=%+v", merged)
	}
	first := merged.Conversation[0].(map[string]any)
	if first["id"] != "b" {
		t.Fatalf("timestamp ordering lost: %+v", merged.Conversation)
	}
}

func TestCloudWorkspaceSidecarRestoreOnCreateTask(t *testing.T) {
	sticky := []byte(`{"kind":"local","goal":"restored sticky"}`)
	checkpoint := []byte(`{"tasks":[{"title":"resume"}]}`)
	sessionRaw, _ := json.Marshal(cloudWorkspaceSessionSidecar{
		Conversation: []interface{}{map[string]any{"role": "assistant", "content": "restored chat"}},
		InputText:    "carry on",
	})
	taskRaw, _ := json.Marshal(cloudWorkspaceTaskSidecar{
		Name: "跨设备任务",
		Mode: taskCodingDevTag,
		Tag:  cloudWorkspaceTag("cws_restore"),
	})
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		sidecars: map[string][]byte{
			cloudWorkspaceSidecarWorkbench:  sticky,
			cloudWorkspaceSidecarCheckpoint: checkpoint,
			cloudWorkspaceSidecarSession:    sessionRaw,
			cloudWorkspaceSidecarTask:       taskRaw,
		},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "dialog-name", "", "", "cws_restore")
	if created.Name != "跨设备任务" {
		t.Fatalf("name=%q", created.Name)
	}
	if !projectRecordHasTagLike(created.Tags, taskCodingDevTag) {
		t.Fatalf("mode missing: %v", created.Tags)
	}
	gotSticky, err := os.ReadFile(filepath.Join(created.ProjectPath, stickyCodingMemoryFileName))
	if err != nil || string(gotSticky) != string(sticky) {
		t.Fatalf("sticky at projectPath=%q err=%v", gotSticky, err)
	}
	gotCP, err := os.ReadFile(filepath.Join(created.ProjectPath, codingExecCheckpointFileName))
	if err != nil || string(gotCP) != string(checkpoint) {
		t.Fatalf("checkpoint at projectPath=%q err=%v", gotCP, err)
	}
	if _, err := os.Stat(filepath.Join(created.WorkingDir, stickyCodingMemoryFileName)); !os.IsNotExist(err) {
		t.Fatal("sticky must not be restored onto workingDir root")
	}
	if _, err := os.Stat(filepath.Join(created.WorkingDir, codingExecCheckpointFileName)); !os.IsNotExist(err) {
		t.Fatal("checkpoint must not be restored onto workingDir root")
	}

	notice := app.CreateProjectTabSession("tab-restore", created.ProjectPath)
	if notice == "" {
		t.Fatal("expected tab session")
	}
	session, err := app.ensureProjectTabSessionPersist().LoadSession("tab-restore")
	if err != nil || session == nil {
		t.Fatalf("load session err=%v session=%v", err, session)
	}
	raw, _ := json.Marshal(session.Conversation)
	if !strings.Contains(string(raw), "restored chat") {
		t.Fatalf("conversation=%s", raw)
	}
	if session.InputText != "carry on" {
		t.Fatalf("input=%q", session.InputText)
	}
	if session.TabID != "tab-restore" {
		t.Fatalf("tab_id=%q", session.TabID)
	}
	if session.ProjectPath != created.ProjectPath {
		t.Fatalf("session project=%q want %q", session.ProjectPath, created.ProjectPath)
	}
}

func TestCloudWorkspaceSidecarFlushOnTabClose(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "标书项目", "", "coding_dev", "cws_tabclose")
	if notice := app.CreateProjectTabSession("tab-close", created.ProjectPath); notice == "" {
		t.Fatal("expected tab session")
	}
	if err := app.ensureProjectTabSessionPersist().SaveSession(&TabSessionData{
		TabID:        "tab-close",
		ProjectPath:  created.ProjectPath,
		Conversation: []interface{}{map[string]any{"role": "user", "content": "keep going"}},
		InputText:    "draft",
	}); err != nil {
		t.Fatal(err)
	}

	app.CloseProjectTabSession("tab-close")
	if lookupHeldCloudWorkspace("cws_tabclose") != nil {
		t.Fatal("tab close should release the lease")
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	assertCloudWorkspaceSessionSidecar(t, hub.sidecars[cloudWorkspaceSidecarSession], "keep going")
	if !hub.deleted {
		t.Fatal("tab close should DELETE lease")
	}
}

func TestCloudWorkspaceSidecarSkipsOversizedCheckpoint(t *testing.T) {
	prev := cloudWorkspaceSidecarMaxBytes
	cloudWorkspaceSidecarMaxBytes = 8
	t.Cleanup(func() { cloudWorkspaceSidecarMaxBytes = prev })

	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "标书项目", "", "coding_dev", "cws_bigcp")
	if err := os.WriteFile(filepath.Join(created.ProjectPath, codingExecCheckpointFileName), []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorkingDir, "src.txt"), []byte("code"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ReleaseCloudWorkspace("cws_bigcp"); err != nil {
		t.Fatalf("release: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if _, ok := hub.sidecars[cloudWorkspaceSidecarCheckpoint]; ok {
		t.Fatalf("oversized checkpoint must be skipped: %q", hub.sidecars[cloudWorkspaceSidecarCheckpoint])
	}
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(hub.sidecars[cloudWorkspaceSidecarTask], &task); err != nil {
		t.Fatal(err)
	}
	if task.Name != "标书项目" {
		t.Fatalf("task sidecar=%+v", task)
	}
	if !hub.deleted {
		t.Fatal("oversized checkpoint must not keep the lease")
	}
}

func assertCloudWorkspaceSessionSidecar(t *testing.T, raw []byte, wantContent string) {
	t.Helper()
	if !strings.Contains(string(raw), wantContent) {
		t.Fatalf("session sidecar=%q", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("session sidecar json: %v", err)
	}
	for _, key := range []string{"tab_id", "project_path", "working_dir"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("%s leaked into session sidecar: %s", key, raw)
		}
	}
}
