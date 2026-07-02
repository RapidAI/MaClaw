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
			"source":     "maclaw_mobile",
			"machine_id": "desktop-1",
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
