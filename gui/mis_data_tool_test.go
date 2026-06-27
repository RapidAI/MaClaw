package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var capturedPath string
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.RequestURI()
		capturedAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/data/app-installations" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"app_id": "expense.blocked", "metadata": map[string]any{"has_blocking_dependency": true}},
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.MISData = corelib.MISDataConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Token:    "data-token",
		TenantID: "tenant_1",
		UserID:   "ops_1",
		Role:     "data_admin",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	out := app.executeMISDataTool(map[string]interface{}{
		"action":                  "list_app_installations",
		"app_id":                  "datasrv-installed-expense.blocked",
		"kind":                    "enterprise_approval_app",
		"source":                  "hub",
		"workflow_skill_id":       "skill.expense.approval",
		"workflow_node":           "finance.review",
		"approval_result_status":  "approved",
		"decision":                "approved",
		"created_by":              "employee_1",
		"current_assignee":        "manager_1",
		"record_approval_id":      "approval-expense-1",
		"approval_instance_id":    "workflow-expense-1",
		"dataset":                 "finance.expenses",
		"object":                  "expense_report",
		"business_record_id":      "expense-1",
		"output_type":             "document",
		"app_definition_hash":     "hash-expense-current",
		"has_blocking_dependency": true,
		"has_missing_required":    false,
		"limit":                   12,
	})
	if capturedAuth != "Bearer data-token" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedPath == "" {
		t.Fatalf("expected DataSrv request, got output: %s", out)
	}
	query := mustParseTestURLQuery(t, capturedPath)
	expected := map[string]string{
		"app_id":                          "expense.blocked",
		"kind":                            "enterprise_approval_app",
		"source":                          "hub",
		"workflow_skill_id":               "skill.expense.approval",
		"workflow_node":                   "finance.review",
		"approval_status":                 "approved",
		"approval_decision":               "approved",
		"applicant_id":                    "employee_1",
		"approver_id":                     "manager_1",
		"approval_id":                     "approval-expense-1",
		"workflow_instance_id":            "workflow-expense-1",
		"dataset_id":                      "finance.expenses",
		"object_role":                     "expense_report",
		"record_id":                       "expense-1",
		"result_type":                     "document",
		"definition_fingerprint":          "hash-expense-current",
		"has_blocking_dependency":         "true",
		"has_missing_required_dependency": "false",
		"limit":                           "12",
	}
	for key, want := range expected {
		if got := query.Get(key); got != want {
			t.Fatalf("query %s = %q, want %q in %s", key, got, want, capturedPath)
		}
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("expected JSON tool response, got: %s", out)
	}
}

func TestExecuteMISDataToolGetAppInstallation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var capturedPath string
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.RequestURI()
		capturedAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/data/app-installations/expense.approval" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"app_id": "expense.approval",
			"metadata": map[string]any{
				"test_evidence_approval_id": "approval-expense-1",
			},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.MISData = corelib.MISDataConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Token:    "data-token",
		TenantID: "tenant_1",
		UserID:   "ops_1",
		Role:     "data_admin",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	out := app.executeMISDataTool(map[string]interface{}{
		"action": "get_app_installation",
		"app_id": "datasrv-installed-expense.approval",
	})
	if capturedAuth != "Bearer data-token" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedPath != "/api/v1/data/app-installations/expense.approval" {
		t.Fatalf("request path = %q, output: %s", capturedPath, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("expected JSON tool response, got: %s", out)
	}
}

func mustParseTestURLQuery(t *testing.T, requestURI string) url.Values {
	t.Helper()
	parsed, err := url.Parse(requestURI)
	if err != nil {
		t.Fatalf("parse request URI %q: %v", requestURI, err)
	}
	return parsed.Query()
}
