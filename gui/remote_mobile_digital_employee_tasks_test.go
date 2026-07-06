package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestMobileDigitalEmployeeCandidateIDsIncludesVEAlias(t *testing.T) {
	got := mobileDigitalEmployeeCandidateIDs("machine-1", "client-1")
	want := []string{"machine-1", "ve_machine-1", "client-1", "ve_client-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate IDs = %#v, want %#v", got, want)
	}
}

func TestRemoteHubClientClaimMobileDocumentUploadTaskUsesMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/mobile/documents/upload/claim" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("kind") != "document" {
			t.Fatalf("kind = %s, want document", r.URL.Query().Get("kind"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "claimed",
			"task": map[string]any{
				"task_id":             "upload-1",
				"filename":            "incident.txt",
				"content_type":        "text/plain",
				"status":              "in_progress",
				"source_download_url": "/api/mobile/documents/upload/upload-1/source",
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	claim, err := client.claimMobileDocumentUploadTask()
	if err != nil {
		t.Fatalf("claimMobileDocumentUploadTask: %v", err)
	}
	if claim == nil || claim.Status != "claimed" || claim.Task == nil || claim.Task.TaskID != "upload-1" {
		t.Fatalf("claim response = %#v", claim)
	}
}

func TestRemoteHubClientDownloadMobileDocumentUploadSourceUsesMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/mobile/documents/upload/upload-1/source" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		_, _ = io.WriteString(w, "incident body")
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	raw, err := client.downloadMobileDocumentUploadSource(mobileDocumentUploadTask{
		TaskID:            "upload-1",
		SourceDownloadURL: "/api/mobile/documents/upload/upload-1/source",
	})
	if err != nil {
		t.Fatalf("downloadMobileDocumentUploadSource: %v", err)
	}
	if string(raw) != "incident body" {
		t.Fatalf("raw = %q, want incident body", string(raw))
	}
}

func TestRemoteHubClientUpdateMobileDocumentUploadTaskSendsResultPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/mobile/documents/upload/upload-1/result" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["status"] != "ready" || payload["markdown"] != "# Incident\n\nDone" || payload["message"] != "parsed" || payload["error"] != "" {
			t.Fatalf("payload = %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": "upload-1",
			"status":  "ready",
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	task, err := client.updateMobileDocumentUploadTask("upload-1", "ready", "# Incident\n\nDone", "parsed", "")
	if err != nil {
		t.Fatalf("updateMobileDocumentUploadTask: %v", err)
	}
	if task == nil || task.TaskID != "upload-1" || task.Status != "ready" {
		t.Fatalf("task = %#v", task)
	}
}

func TestMobileDocumentSourceMarkdownParsesTextLikeSource(t *testing.T) {
	markdown, ok := mobileDocumentSourceMarkdown("incident.txt", "text/plain", []byte("service recovered"))
	if !ok {
		t.Fatal("mobileDocumentSourceMarkdown returned ok=false")
	}
	if !strings.Contains(markdown, "# incident") || !strings.Contains(markdown, "service recovered") {
		t.Fatalf("markdown = %q, want title and body", markdown)
	}

	if _, ok := mobileDocumentSourceMarkdown("photo.png", "image/png", []byte("not text")); ok {
		t.Fatal("image source should not be parsed by document worker")
	}
}

func TestRemoteHubClientClaimMobileDigitalEmployeeTaskUsesMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/mobile/digital-employees/ve_machine-1/tasks/claim" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "claimed",
			"task": map[string]any{
				"task_id":     "task-1",
				"employee_id": "ve_machine-1",
				"prompt":      "check server",
				"task_type":   "server_maintenance",
				"context": map[string]string{
					"source": "maclaw_mobile",
				},
				"status": "in_progress",
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	claim, err := client.claimMobileDigitalEmployeeTask("ve_machine-1")
	if err != nil {
		t.Fatalf("claimMobileDigitalEmployeeTask: %v", err)
	}
	if claim == nil || claim.Status != "claimed" || claim.Task == nil || claim.Task.TaskID != "task-1" || claim.Task.Prompt != "check server" {
		t.Fatalf("claim response = %#v", claim)
	}
	if claim.Task.TaskType != "server_maintenance" || claim.Task.Context["source"] != "maclaw_mobile" {
		t.Fatalf("claim task context = %#v", claim.Task)
	}
}

func TestBuildMobileDigitalEmployeeExecutionPromptIncludesTypeAndContext(t *testing.T) {
	prompt := buildMobileDigitalEmployeeExecutionPrompt(mobileDigitalEmployeeTask{
		TaskID:   "task-1",
		Prompt:   "check disk",
		TaskType: "server_maintenance",
		Context: map[string]string{
			"source":      "maclaw_mobile",
			"machine_id":  "desktop-1",
			" tenant_id ": "tenant-a",
			"empty_value": " ",
			"   ":         "ignored",
		},
	})
	for _, want := range []string{
		"Task type: server_maintenance",
		"source: maclaw_mobile",
		"machine_id: desktop-1",
		"check disk",
		"manual confirmation",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	for _, unwanted := range []string{"empty_value", "ignored"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("prompt included blank context %q: %s", unwanted, prompt)
		}
	}
	wantOrder := []string{
		"- machine_id: desktop-1",
		"- source: maclaw_mobile",
		"- tenant_id: tenant-a",
	}
	last := -1
	for _, want := range wantOrder {
		index := strings.Index(prompt, want)
		if index < 0 {
			t.Fatalf("prompt missing ordered context %q: %s", want, prompt)
		}
		if index <= last {
			t.Fatalf("prompt context order is unstable; %q appeared after index %d: %s", want, last, prompt)
		}
		last = index
	}
}

func TestRemoteHubClientUpdateMobileDigitalEmployeeTaskSendsStatusResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/mobile/digital-employees/tasks/task-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["status"] != "done" || payload["result"] != "fixed" {
			t.Fatalf("payload = %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": "task-1",
			"status":  "done",
			"result":  "fixed",
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	task, err := client.updateMobileDigitalEmployeeTask("task-1", "done", "fixed")
	if err != nil {
		t.Fatalf("updateMobileDigitalEmployeeTask: %v", err)
	}
	if task == nil || task.TaskID != "task-1" || task.Status != "done" || task.Result != "fixed" {
		t.Fatalf("task = %#v", task)
	}
}

func TestRemoteHubClientClaimMobileBackendSSHSessionUsesMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/mobile/ssh/sessions/claim" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "claimed",
			"session": map[string]any{
				"session_id":        "mobssh_1",
				"server_profile_id": "prod",
				"status":            "input_queued",
				"pending_input":     []string{"uptime"},
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	claim, err := client.claimMobileBackendSSHSession()
	if err != nil {
		t.Fatalf("claimMobileBackendSSHSession: %v", err)
	}
	if claim == nil || claim.Status != "claimed" || claim.Session == nil || claim.Session.SessionID != "mobssh_1" {
		t.Fatalf("claim response = %#v", claim)
	}
	if claim.Session.ServerProfileID != "prod" || len(claim.Session.PendingInput) != 1 || claim.Session.PendingInput[0] != "uptime" {
		t.Fatalf("claim session = %#v", claim.Session)
	}
}

func TestRemoteHubClientUpdateMobileBackendSSHSessionSendsWorkerPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/mobile/ssh/sessions/mobssh_1/worker" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["status"] != "connected" || payload["state"] != "running" || payload["backend_session_id"] != "ssh-prod" || payload["clear_pending_input"] != true {
			t.Fatalf("payload = %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":         "mobssh_1",
			"backend_session_id": "ssh-prod",
			"status":             "connected",
			"state":              "running",
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	session, err := client.updateMobileBackendSSHSession("mobssh_1", map[string]any{
		"status":              "connected",
		"state":               "running",
		"backend_session_id":  "ssh-prod",
		"clear_pending_input": true,
	})
	if err != nil {
		t.Fatalf("updateMobileBackendSSHSession: %v", err)
	}
	if session == nil || session.SessionID != "mobssh_1" || session.BackendSessionID != "ssh-prod" || session.Status != "connected" {
		t.Fatalf("session = %#v", session)
	}
}

func TestRemoteHubClientMobileBackendSSHTaskClaimAndUpdateUseMachineAuth(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		switch r.URL.Path {
		case "/api/mobile/ssh/tasks/claim":
			if r.Method != http.MethodPost {
				t.Fatalf("claim method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "claimed",
				"task": map[string]any{
					"task_id":            "task-1",
					"session_id":         "mobssh_1",
					"backend_session_id": "mobile-ssh:mobssh_1",
					"command":            "journalctl -u app",
					"status":             "agent_claimed",
				},
			})
		case "/api/mobile/ssh/tasks/task-1/worker":
			if r.Method != http.MethodPatch {
				t.Fatalf("update method = %s", r.Method)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload["status"] != "completed" || payload["log_tail"] != "done" || payload["exit_code"] != float64(0) {
				t.Fatalf("payload = %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"task_id":    "task-1",
					"session_id": "mobssh_1",
					"status":     "completed",
					"log_tail":   "done",
					"exit_code":  0,
				},
			})
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	claim, err := client.claimMobileBackendSSHTask()
	if err != nil {
		t.Fatalf("claimMobileBackendSSHTask: %v", err)
	}
	if claim == nil || claim.Status != "claimed" || claim.Task == nil || claim.Task.TaskID != "task-1" || claim.Task.Command != "journalctl -u app" {
		t.Fatalf("claim = %#v", claim)
	}
	updated, err := client.updateMobileBackendSSHTask("task-1", map[string]any{
		"status":    "completed",
		"log_tail":  "done",
		"exit_code": 0,
	})
	if err != nil {
		t.Fatalf("updateMobileBackendSSHTask: %v", err)
	}
	if updated == nil || updated.TaskID != "task-1" || updated.Status != "completed" || updated.ExitCode == nil || *updated.ExitCode != 0 {
		t.Fatalf("updated task = %#v", updated)
	}
	if len(seen) != 2 {
		t.Fatalf("seen requests = %#v, want claim and update", seen)
	}
}

func TestRemoteHubClientMobileBackendSSHFileOperationClaimAndUpdateUseMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		switch r.URL.Path {
		case "/api/mobile/ssh/files/claim":
			if r.Method != http.MethodPost {
				t.Fatalf("claim method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "claimed",
				"operation": map[string]any{
					"operation_id":       "file-op-1",
					"session_id":         "mobssh_1",
					"backend_session_id": "mobile-ssh:mobssh_1",
					"action":             "download",
					"remote_path":        "/var/log/app.log",
					"status":             "agent_claimed",
				},
			})
		case "/api/mobile/ssh/files/file-op-1/worker":
			if r.Method != http.MethodPatch {
				t.Fatalf("update method = %s", r.Method)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload["status"] != "completed" || payload["bytes_transferred"] != float64(42) || payload["download_url"] == "" {
				t.Fatalf("payload = %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"operation": map[string]any{
					"operation_id":      "file-op-1",
					"session_id":        "mobssh_1",
					"status":            "completed",
					"bytes_transferred": 42,
					"download_url":      "/api/mobile/ssh/files/file-op-1/download",
				},
			})
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	claim, err := client.claimMobileBackendSSHFileOperation()
	if err != nil {
		t.Fatalf("claimMobileBackendSSHFileOperation: %v", err)
	}
	if claim == nil || claim.Status != "claimed" || claim.Operation == nil || claim.Operation.OperationID != "file-op-1" || claim.Operation.RemotePath != "/var/log/app.log" {
		t.Fatalf("claim = %#v", claim)
	}
	updated, err := client.updateMobileBackendSSHFileOperation("file-op-1", map[string]any{
		"status":            "completed",
		"bytes_transferred": 42,
		"download_url":      "/api/mobile/ssh/files/file-op-1/download",
	})
	if err != nil {
		t.Fatalf("updateMobileBackendSSHFileOperation: %v", err)
	}
	if updated == nil || updated.OperationID != "file-op-1" || updated.Status != "completed" || updated.BytesTransferred != 42 || updated.DownloadURL == "" {
		t.Fatalf("updated operation = %#v", updated)
	}
}

func TestRemoteHubClientMobileBackendSSHOutputChunkTracksDelta(t *testing.T) {
	client := &RemoteHubClient{}

	first := client.mobileBackendSSHOutputChunk("mobssh_1", "connected")
	if first != "connected" {
		t.Fatalf("first chunk = %q, want full preview", first)
	}
	second := client.mobileBackendSSHOutputChunk("mobssh_1", "connected\n$ uptime\n1 day")
	if second != "\n$ uptime\n1 day" {
		t.Fatalf("second chunk = %q, want appended output", second)
	}
	third := client.mobileBackendSSHOutputChunk("mobssh_1", "connected\n$ uptime\n1 day")
	if third != "" {
		t.Fatalf("third chunk = %q, want no duplicate output", third)
	}
	replaced := client.mobileBackendSSHOutputChunk("mobssh_1", "new shell banner")
	if replaced != "new shell banner" {
		t.Fatalf("replaced chunk = %q, want full preview after tail reset", replaced)
	}
}

func TestMobileBackendSSHHelpersMapMobileControlRecordsToDesktopRuntime(t *testing.T) {
	client := &RemoteHubClient{}
	client.mobileBackendSSHTasks.Store("mobile-task-1", "bg_123")

	if got := mobileBackendLocalSessionID("mobssh_1", ""); got != "mobile-ssh:mobssh_1" {
		t.Fatalf("mobileBackendLocalSessionID fallback = %q", got)
	}
	if got := mobileBackendLocalSessionID("mobssh_1", "ssh-prod"); got != "ssh-prod" {
		t.Fatalf("mobileBackendLocalSessionID backend = %q", got)
	}
	if got := mobileBackendSSHTaskOwner("mobssh_1"); got != "mobile:mobssh_1" {
		t.Fatalf("mobileBackendSSHTaskOwner = %q", got)
	}
	if got, ok := client.mobileBackendSSHCoreTaskID("mobile-task-1"); !ok || got != "bg_123" {
		t.Fatalf("mobileBackendSSHCoreTaskID = %q,%v", got, ok)
	}
	if _, ok := client.mobileBackendSSHCoreTaskID("missing"); ok {
		t.Fatal("missing mobile task should not resolve")
	}
}

func TestMobileBackendSSHTaskStatusPayloadIncludesExitCodeOnlyWhenKnown(t *testing.T) {
	payload := mobileBackendSSHTaskStatusPayload("mobile-ssh:mobssh_1", &remote.BackgroundTaskStatus{
		Status:        remote.SSHBackgroundTaskStatusCompleted,
		LogTail:       "done",
		ExitCode:      0,
		ExitCodeKnown: true,
	}, "finished")
	if payload["status"] != "completed" || payload["backend_session_id"] != "mobile-ssh:mobssh_1" || payload["log_tail"] != "done" || payload["exit_code"] != 0 {
		t.Fatalf("payload = %#v", payload)
	}

	payload = mobileBackendSSHTaskStatusPayload("ssh-prod", &remote.BackgroundTaskStatus{
		Status:  remote.SSHBackgroundTaskStatusRunning,
		LogTail: "still running",
	}, "")
	if payload["status"] != "running" || payload["message"] == "" {
		t.Fatalf("running payload = %#v", payload)
	}
	if _, ok := payload["exit_code"]; ok {
		t.Fatalf("running payload should not include unknown exit_code: %#v", payload)
	}
}

func TestMobileBackendSSHWaitTimeoutDefaultsAndBounds(t *testing.T) {
	cases := map[int]int{
		0:    60,
		-10:  60,
		1:    5,
		30:   30,
		1200: 600,
	}
	for input, want := range cases {
		if got := mobileBackendSSHTimeoutSeconds(input); got != want {
			t.Fatalf("mobileBackendSSHTimeoutSeconds(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestProcessMobileBackendSSHTaskWaitsForDesktopManagedSession(t *testing.T) {
	updates := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/mobile/ssh/tasks/task-1/worker" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		updates <- payload
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"task_id":            "task-1",
				"session_id":         "mobssh_1",
				"backend_session_id": "mobile-ssh:mobssh_1",
				"status":             payload["status"],
				"message":            payload["message"],
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{
		app: app,
		configureIMHandler: func(h *IMMessageHandler) {
			h.sshMgr = remote.NewSSHSessionManager(nil)
			h.bgTaskMgr = remote.NewSSHBackgroundTaskManager(h.sshMgr)
		},
	}

	client.processMobileBackendSSHTask(mobileBackendSSHTask{
		TaskID:    "task-1",
		SessionID: "mobssh_1",
		Command:   "journalctl -u app",
		Status:    "agent_claimed",
	})

	payload := <-updates
	if payload["status"] != "agent_claimed" || payload["backend_session_id"] != "mobile-ssh:mobssh_1" {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(payload["message"].(string), "Waiting for MaClaw desktop to attach") {
		t.Fatalf("message = %#v", payload["message"])
	}
}

func TestProcessMobileBackendSSHFileOperationWaitsForDesktopManagedSession(t *testing.T) {
	updates := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/mobile/ssh/files/file-op-1/worker" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		updates <- payload
		_ = json.NewEncoder(w).Encode(map[string]any{
			"operation": map[string]any{
				"operation_id":       "file-op-1",
				"session_id":         "mobssh_1",
				"backend_session_id": "mobile-ssh:mobssh_1",
				"status":             payload["status"],
				"message":            payload["message"],
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{
		app: app,
		configureIMHandler: func(h *IMMessageHandler) {
			h.sshMgr = remote.NewSSHSessionManager(nil)
		},
	}

	client.processMobileBackendSSHFileOperation(mobileBackendSSHFileOperation{
		OperationID: "file-op-1",
		SessionID:   "mobssh_1",
		Action:      "list",
		RemotePath:  "/var/log",
		Status:      "agent_claimed",
	})

	payload := <-updates
	if payload["status"] != "agent_claimed" || payload["backend_session_id"] != "mobile-ssh:mobssh_1" {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(payload["message"].(string), "Waiting for MaClaw desktop to attach") {
		t.Fatalf("message = %#v", payload["message"])
	}
}

func TestMobileBackendShellQuoteEscapesRemotePaths(t *testing.T) {
	cases := map[string]string{
		"":                   "''",
		"/var/log/app.log":   "'/var/log/app.log'",
		"/tmp/it's fine.log": "'/tmp/it'\\''s fine.log'",
		"  /tmp/space.log  ": "'/tmp/space.log'",
	}
	for input, want := range cases {
		if got := mobileBackendShellQuote(input); got != want {
			t.Fatalf("mobileBackendShellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRemoteHubClientPublishMobileServerProfilesUsesMachineAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/mobile/server-profiles" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q", got)
		}
		var payload struct {
			Profiles []mobileServerProfilePayload `json:"profiles"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload.Profiles) != 1 {
			t.Fatalf("profiles = %#v, want one", payload.Profiles)
		}
		profile := payload.Profiles[0]
		if profile.ID != "prod" || profile.Name != "prod" || profile.Host != "10.0.0.10" || profile.Port != 2222 || profile.Username != "deploy" || profile.AuthMode != "private_key" {
			t.Fatalf("profile = %#v, want sanitized desktop SSH profile", profile)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "count": 1})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		SSHHosts: []corelib.SSHHostEntry{
			{Label: "prod", Host: "10.0.0.10", Port: 2222, User: "deploy", AuthMethod: "key", KeyPath: "~/.ssh/prod"},
			{Label: "invalid", Host: "", User: "root"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := &RemoteHubClient{app: app}

	client.publishMobileServerProfilesOnce()
}

func TestResolveMobileBackendSSHHostMatchesConfiguredProfiles(t *testing.T) {
	hosts := []corelib.SSHHostEntry{
		{Label: "prod", Host: "10.0.0.10", User: "deploy", Port: 2222, AuthMethod: "key", KeyPath: "~/.ssh/prod"},
		{Label: "bad", Host: "", User: "root"},
	}

	for _, profileID := range []string{"prod", "PROD", "deploy@10.0.0.10:2222", "10.0.0.10"} {
		host, ok := resolveMobileBackendSSHHost(hosts, profileID)
		if !ok || host.Label != "prod" {
			t.Fatalf("resolveMobileBackendSSHHost(%q) = %#v,%v; want prod", profileID, host, ok)
		}
	}
	if _, ok := resolveMobileBackendSSHHost(hosts, "bad"); ok {
		t.Fatal("invalid profile with missing host should not resolve")
	}
	if _, ok := resolveMobileBackendSSHHost(hosts, "missing"); ok {
		t.Fatal("missing profile should not resolve")
	}
	cfg := mobileBackendSSHHostConfig(hosts[0])
	if cfg.Port != 2222 || cfg.AuthMethod != "key" || cfg.KeyPath != "~/.ssh/prod" || cfg.SSHHostID() != "deploy@10.0.0.10:2222" {
		t.Fatalf("host config = %#v", cfg)
	}
}

func TestMobileServerProfilesFromSSHHostsSanitizesConfiguredHosts(t *testing.T) {
	profiles := mobileServerProfilesFromSSHHosts([]corelib.SSHHostEntry{
		{Label: "prod", Host: "10.0.0.10", User: "deploy", Port: 2222, AuthMethod: "key", KeyPath: "~/.ssh/prod"},
		{Host: "10.0.0.11", User: "root", AuthMethod: "agent"},
		{Label: "bad", Host: "", User: "root"},
	})

	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v, want two valid profiles", profiles)
	}
	if profiles[0].ID != "prod" || profiles[0].AuthMode != "private_key" || profiles[0].Tag != "desktop" {
		t.Fatalf("first profile = %#v", profiles[0])
	}
	if profiles[1].ID != "root@10.0.0.11:22" || profiles[1].AuthMode != "agent" {
		t.Fatalf("second profile = %#v", profiles[1])
	}
}
