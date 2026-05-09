package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCenterCollaborationsPreservesWorkflowStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/client/collaborations" {
			t.Fatalf("path = %s, want /client/collaborations", r.URL.Path)
		}
		if got := r.URL.Query().Get("colleague_id"); got != "worker a/+ops" {
			t.Fatalf("colleague_id = %q, want worker a/+ops", got)
		}
		if r.URL.RawQuery == "colleague_id=worker a/+ops" {
			t.Fatalf("colleague_id query was not URL encoded: %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{{
				"id":                        "collab-1",
				"title":                     "Review workflow handoff",
				"to_colleague_id":           "worker a/+ops",
				"status":                    "pending",
				"workflow_step_instance_id": "wf-step-1",
				"created_at":                "2026-05-06T00:00:00Z",
				"updated_at":                "2026-05-06T00:00:00Z",
			}},
		})
	}))
	defer server.Close()

	tasks, err := fetchCenterCollaborationsContext(context.Background(), server.URL, "tenant-a", "worker a/+ops", 1)
	if err != nil {
		t.Fatalf("fetchCenterCollaborationsContext returned error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v, want one task", tasks)
	}
	if tasks[0].WorkflowStepID != "wf-step-1" {
		t.Fatalf("WorkflowStepID = %q, want wf-step-1", tasks[0].WorkflowStepID)
	}
}

func TestFetchCenterCollaborationsRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tasks":[{"id":"collab-1","title":"Review"}]} {"tasks":[]}`))
	}))
	defer server.Close()

	_, err := fetchCenterCollaborationsContext(context.Background(), server.URL, "tenant-a", "worker-a", 1)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("fetchCenterCollaborationsContext error = %v, want errCenterJSONTrailing", err)
	}
}

func TestTransitionCenterCollaborationTaskPostsTenantScopedAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.EscapedPath() != "/runtime/collaboration/collab%2Fa%201/start" {
			t.Fatalf("path = %s, want escaped collaboration transition path", r.URL.EscapedPath())
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		var req struct {
			ActorID string `json:"actor_id"`
			Result  string `json:"result"`
			Note    string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		if req.ActorID != "worker-a" || req.Result != "" || req.Note != "start from iWorker" {
			t.Fatalf("unexpected transition request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"task":   CenterCollabTask{ID: "collab/a 1", Title: "Escaped task", Status: "in_progress"},
		})
	}))
	defer server.Close()

	task, err := transitionCenterCollaborationTaskContext(context.Background(), server.URL, "tenant-a", "collab/a 1", "/start/", " worker-a ", "", " start from iWorker ", 1)
	if err != nil {
		t.Fatalf("transitionCenterCollaborationTaskContext returned error: %v", err)
	}
	if task.ID != "collab/a 1" || task.Status != "in_progress" {
		t.Fatalf("transition task = %+v", task)
	}
}

func TestTransitionCenterCollaborationTaskReturnsCenterErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"cannot transition from completed"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := transitionCenterCollaborationTaskContext(context.Background(), server.URL, "tenant-a", "collab-1", "start", "worker-a", "", "", 1)
	if err == nil {
		t.Fatalf("expected transition error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "cannot transition from completed") {
		t.Fatalf("error = %q, want center body included", got)
	}
}

func TestFetchCenterCapabilitiesEscapesColleagueID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/client/capabilities" {
			t.Fatalf("path = %s, want /client/capabilities", r.URL.Path)
		}
		if got := r.URL.Query().Get("colleague_id"); got != "worker a/+ops" {
			t.Fatalf("colleague_id = %q, want worker a/+ops", got)
		}
		if r.URL.RawQuery == "colleague_id=worker a/+ops" {
			t.Fatalf("colleague_id query was not URL encoded: %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"capabilities": []map[string]any{{
				"id":   "cap-1",
				"name": "Review Skill",
			}},
		})
	}))
	defer server.Close()

	capabilities := fetchCenterCapabilities(server.URL, "tenant-a", "worker a/+ops", 1)
	if len(capabilities) != 1 || capabilities[0].ID != "cap-1" {
		t.Fatalf("capabilities = %+v, want cap-1", capabilities)
	}
}
