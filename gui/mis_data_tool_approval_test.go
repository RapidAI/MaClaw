package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMISDataApprovalActionsCarryWorkflowLinkFields(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Query  string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Token:    "test-token",
		TenantID: "tenant-1",
		UserID:   "alice",
		Role:     "data_admin",
	}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	createOut := app.executeMISDataTool(map[string]interface{}{
		"action":               "create_record_approval",
		"dataset_id":           "finance.expenses",
		"record_id":            "exp-1",
		"kind":                 "approval",
		"summary":              "Expense approval",
		"workflow_skill_id":    "expense-approval-workflow",
		"workflow_version":     "1.2.0",
		"workflow_instance_id": "wf-inst-1",
		"workflow_node_id":     "manager_review",
		"workflow_decision_id": "decision-1",
		"business_status":      "approval_pending",
		"result_status":        "pending",
		"request":              map[string]interface{}{"amount": 88},
	})
	if !strings.Contains(createOut, `"ok": true`) {
		t.Fatalf("create_record_approval output = %s", createOut)
	}

	listOut := app.executeMISDataTool(map[string]interface{}{
		"action":               "list_record_approvals",
		"dataset_id":           "finance.expenses",
		"workflow_skill_id":    "expense-approval-workflow",
		"approval_instance_id": "wf-inst-1",
		"business_status":      "approval_pending",
		"result_status":        "pending",
	})
	if !strings.Contains(listOut, `"ok": true`) {
		t.Fatalf("list_record_approvals output = %s", listOut)
	}

	reviewOut := app.executeMISDataTool(map[string]interface{}{
		"action":               "review_record_approval",
		"approval_id":          "approval-1",
		"decision":             "approved",
		"reason":               "Policy matched",
		"workflow_node_id":     "finance_review",
		"workflow_decision_id": "decision-2",
		"business_status":      "approved",
		"result_status":        "approved",
	})
	if !strings.Contains(reviewOut, `"ok": true`) {
		t.Fatalf("review_record_approval output = %s", reviewOut)
	}

	if len(captured) != 3 {
		t.Fatalf("captured %d requests, want 3: %#v", len(captured), captured)
	}
	create := captured[0]
	if create.Method != http.MethodPost || create.Path != "/api/v1/data/datasets/finance.expenses/records/exp-1/approvals" {
		t.Fatalf("unexpected create request: %#v", create)
	}
	for key, want := range map[string]string{
		"workflow_skill_id":    "expense-approval-workflow",
		"workflow_version":     "1.2.0",
		"workflow_instance_id": "wf-inst-1",
		"workflow_node_id":     "manager_review",
		"workflow_decision_id": "decision-1",
		"business_status":      "approval_pending",
		"result_status":        "pending",
	} {
		if got := strings.TrimSpace(asTestString(create.Body[key])); got != want {
			t.Fatalf("create body[%s] = %q, want %q; body=%#v", key, got, want, create.Body)
		}
	}
	if request, ok := create.Body["request"].(map[string]interface{}); !ok || request["amount"] == nil {
		t.Fatalf("create body should keep request payload: %#v", create.Body)
	}

	list := captured[1]
	if list.Method != http.MethodGet || list.Path != "/api/v1/data/approvals" {
		t.Fatalf("unexpected list request: %#v", list)
	}
	for _, want := range []string{
		"workflow_skill_id=expense-approval-workflow",
		"workflow_instance_id=wf-inst-1",
		"business_status=approval_pending",
		"result_status=pending",
	} {
		if !strings.Contains(list.Query, want) {
			t.Fatalf("list query %q missing %q", list.Query, want)
		}
	}

	review := captured[2]
	if review.Method != http.MethodPost || review.Path != "/api/v1/data/approvals/approval-1/review" {
		t.Fatalf("unexpected review request: %#v", review)
	}
	for key, want := range map[string]string{
		"decision":             "approved",
		"workflow_node_id":     "finance_review",
		"workflow_decision_id": "decision-2",
		"business_status":      "approved",
		"result_status":        "approved",
	} {
		if got := strings.TrimSpace(asTestString(review.Body[key])); got != want {
			t.Fatalf("review body[%s] = %q, want %q; body=%#v", key, got, want, review.Body)
		}
	}
}

func asTestString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	data, _ := json.Marshal(value)
	return string(data)
}
