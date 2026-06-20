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
		"app_id":               "mis.expense",
		"blueprint_id":         "mis.expense.approval",
		"object_role":          "expense_report",
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
		"result_payload":       map[string]interface{}{"business_record": map[string]interface{}{"id": "exp-1"}},
		"outputs":              []interface{}{map[string]interface{}{"type": "business_record", "title": "Expense record"}},
		"artifacts":            []interface{}{map[string]interface{}{"id": "artifact-1", "name": "approval.pdf"}},
	})
	if !strings.Contains(createOut, `"ok": true`) {
		t.Fatalf("create_record_approval output = %s", createOut)
	}

	listOut := app.executeMISDataTool(map[string]interface{}{
		"action":               "list_record_approvals",
		"dataset_id":           "finance.expenses",
		"app_id":               "mis.expense",
		"blueprint_id":         "mis.expense.approval",
		"object_role":          "expense_report",
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
		"result_payload":       map[string]interface{}{"business_record": map[string]interface{}{"id": "exp-1", "status": "approved"}},
		"outputs":              []interface{}{map[string]interface{}{"type": "text", "title": "Decision", "text": "approved"}},
		"artifacts":            []interface{}{map[string]interface{}{"id": "artifact-1", "name": "approval.pdf"}},
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
		"app_id":               "mis.expense",
		"blueprint_id":         "mis.expense.approval",
		"object_role":          "expense_report",
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
	if payload, ok := create.Body["result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("create body should keep result payload: %#v", create.Body)
	}
	if outputs, ok := create.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("create body should keep outputs: %#v", create.Body)
	}
	if artifacts, ok := create.Body["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("create body should keep artifacts: %#v", create.Body)
	}

	list := captured[1]
	if list.Method != http.MethodGet || list.Path != "/api/v1/data/approvals" {
		t.Fatalf("unexpected list request: %#v", list)
	}
	for _, want := range []string{
		"app_id=mis.expense",
		"blueprint_id=mis.expense.approval",
		"object_role=expense_report",
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
	if review.Body["result_payload"] == nil || review.Body["outputs"] == nil || review.Body["artifacts"] == nil {
		t.Fatalf("review body should keep result package: %#v", review.Body)
	}
}

func TestMISDataApprovalSemanticActions(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Query  string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Role:     "approver",
	}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	for _, args := range []map[string]interface{}{
		{
			"action":                    "mis.approval.start",
			"dataset_id":                "finance.expenses",
			"record_id":                 "exp-2",
			"approval_workflow_id":      "expense_approval",
			"approval_workflow_version": "2.0.0",
			"approval_instance_id":      "appr-2",
			"summary":                   "Expense approval",
			"request":                   map[string]interface{}{"amount": 1280},
		},
		{
			"action":               "mis.approval.list",
			"dataset_id":           "finance.expenses",
			"record_id":            "exp-2",
			"workflow_instance_id": "appr-2",
		},
		{
			"action":      "mis.approval.get",
			"approval_id": "approval-2",
		},
		{
			"action": "mis.approval.my_inbox",
			"status": "pending",
			"limit":  5,
		},
		{
			"action":               "mis.approval.sync_result",
			"approval_id":          "approval-2",
			"result":               "approved",
			"message":              "done",
			"workflow_decision_id": "decision-3",
			"business_status":      "approved",
			"result_status":        "approved",
		},
	} {
		out := app.executeMISDataTool(args)
		if !strings.Contains(out, `"ok": true`) {
			t.Fatalf("%s output = %s", args["action"], out)
		}
	}

	if len(captured) != 5 {
		t.Fatalf("captured %d requests, want 5: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/datasets/finance.expenses/records/exp-2/approvals" {
		t.Fatalf("unexpected semantic start request: %#v", captured[0])
	}
	if got := strings.TrimSpace(asTestString(captured[0].Body["workflow_skill_id"])); got != "expense_approval" {
		t.Fatalf("semantic start workflow_skill_id = %q; body=%#v", got, captured[0].Body)
	}
	if got := strings.TrimSpace(asTestString(captured[0].Body["workflow_version"])); got != "2.0.0" {
		t.Fatalf("semantic start workflow_version = %q; body=%#v", got, captured[0].Body)
	}
	if captured[1].Method != http.MethodGet || captured[1].Path != "/api/v1/data/approvals" || !strings.Contains(captured[1].Query, "record_id=exp-2") || !strings.Contains(captured[1].Query, "workflow_instance_id=appr-2") {
		t.Fatalf("unexpected semantic list request: %#v", captured[1])
	}
	if captured[2].Method != http.MethodGet || captured[2].Path != "/api/v1/data/approvals/approval-2" {
		t.Fatalf("unexpected semantic get request: %#v", captured[2])
	}
	if captured[3].Method != http.MethodGet || captured[3].Path != "/api/v1/data/approvals" || !strings.Contains(captured[3].Query, "assigned_to=alice") || !strings.Contains(captured[3].Query, "status=pending") {
		t.Fatalf("unexpected semantic inbox request: %#v", captured[3])
	}
	if captured[4].Method != http.MethodPost || captured[4].Path != "/api/v1/data/approvals/approval-2/review" {
		t.Fatalf("unexpected semantic sync request: %#v", captured[4])
	}
	if got := strings.TrimSpace(asTestString(captured[4].Body["decision"])); got != "approved" {
		t.Fatalf("semantic sync decision = %q; body=%#v", got, captured[4].Body)
	}
	if got := strings.TrimSpace(asTestString(captured[4].Body["reason"])); got != "done" {
		t.Fatalf("semantic sync reason = %q; body=%#v", got, captured[4].Body)
	}
}
func TestMISDataApprovalDocumentedListAliases(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Query  string
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery})
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
		Role:     "approver",
	}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	listByRecordOut := app.executeMISDataTool(map[string]interface{}{
		"action":     "mis.approval.list_by_record",
		"dataset_id": "finance.expenses",
		"record_id":  "exp-3",
	})
	if !strings.Contains(listByRecordOut, `"ok": true`) {
		t.Fatalf("mis.approval.list_by_record output = %s", listByRecordOut)
	}
	myPendingOut := app.executeMISDataTool(map[string]interface{}{
		"action": "mis.approval.my_pending",
		"limit":  3,
	})
	if !strings.Contains(myPendingOut, `"ok": true`) {
		t.Fatalf("mis.approval.my_pending output = %s", myPendingOut)
	}

	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/approvals" || !strings.Contains(captured[0].Query, "dataset_id=finance.expenses") || !strings.Contains(captured[0].Query, "record_id=exp-3") {
		t.Fatalf("unexpected list_by_record request: %#v", captured[0])
	}
	if captured[1].Method != http.MethodGet || captured[1].Path != "/api/v1/data/approvals" || !strings.Contains(captured[1].Query, "assigned_to=alice") || !strings.Contains(captured[1].Query, "status=pending") || !strings.Contains(captured[1].Query, "limit=3") {
		t.Fatalf("unexpected my_pending request: %#v", captured[1])
	}
}

func TestMISDataBusinessObjectRoleActionsAndDatasetResolution(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Query  string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/business-objects":
			_, _ = w.Write([]byte(`{"items":[{"object_role":"expense_report","dataset_id":"finance.expenses","initialized":true}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/object-roles/resolve":
			_, _ = w.Write([]byte(`{"object_role":"expense_report","dataset_id":"finance.expenses","initialized":true,"business_object":{"object_role":"expense_report","dataset_id":"finance.expenses","initialized":true}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Token:    "test-token",
		TenantID: "tenant-1",
		UserID:   "alice",
		Role:     "approver",
	}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	listOut := app.executeMISDataTool(map[string]interface{}{
		"action":      "list_business_objects",
		"app_id":      "mis.expense",
		"domain":      "finance",
		"object_role": "expense_report",
		"limit":       7,
		"before_id":   "cursor-1",
	})
	if !strings.Contains(listOut, `"object_role": "expense_report"`) {
		t.Fatalf("list_business_objects output = %s", listOut)
	}

	resolveOut := app.executeMISDataTool(map[string]interface{}{
		"action":              "resolve_object_role",
		"app_id":              "mis.expense",
		"object_role":         "expense_report",
		"require_initialized": true,
	})
	if !strings.Contains(resolveOut, `"dataset_id": "finance.expenses"`) {
		t.Fatalf("resolve_object_role output = %s", resolveOut)
	}

	queryOut := app.executeMISDataTool(map[string]interface{}{
		"action":      "query_records",
		"app_id":      "mis.expense",
		"object_role": "expense_report",
		"filter":      map[string]interface{}{"status": "submitted"},
		"limit":       5,
	})
	if !strings.Contains(queryOut, `"ok": true`) {
		t.Fatalf("query_records output = %s", queryOut)
	}

	approvalOut := app.executeMISDataTool(map[string]interface{}{
		"action":               "mis.approval.start",
		"app_id":               "mis.expense",
		"object_role":          "expense_report",
		"record_id":            "exp-4",
		"approval_workflow_id": "expense_approval",
		"approval_instance_id": "appr-4",
		"summary":              "Expense approval",
		"request":              map[string]interface{}{"amount": 512},
	})
	if !strings.Contains(approvalOut, `"ok": true`) {
		t.Fatalf("mis.approval.start output = %s", approvalOut)
	}

	if len(captured) != 6 {
		t.Fatalf("captured %d requests, want 6: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/business-objects" {
		t.Fatalf("unexpected business object list request: %#v", captured[0])
	}
	for _, want := range []string{"app_id=mis.expense", "domain=finance", "object_role=expense_report", "limit=7", "before_id=cursor-1"} {
		if !strings.Contains(captured[0].Query, want) {
			t.Fatalf("business object query %q missing %q", captured[0].Query, want)
		}
	}
	if captured[1].Method != http.MethodPost || captured[1].Path != "/api/v1/data/object-roles/resolve" {
		t.Fatalf("unexpected direct resolve request: %#v", captured[1])
	}
	if got := strings.TrimSpace(asTestString(captured[1].Body["object_role"])); got != "expense_report" {
		t.Fatalf("direct resolve object_role = %q; body=%#v", got, captured[1].Body)
	}
	if got, ok := captured[1].Body["require_initialized"].(bool); !ok || !got {
		t.Fatalf("direct resolve should require initialized dataset: %#v", captured[1].Body)
	}
	if captured[2].Method != http.MethodPost || captured[2].Path != "/api/v1/data/object-roles/resolve" {
		t.Fatalf("unexpected query resolver request: %#v", captured[2])
	}
	if got, ok := captured[2].Body["require_initialized"].(bool); !ok || !got {
		t.Fatalf("query_records resolver should require initialized dataset: %#v", captured[2].Body)
	}
	if captured[3].Method != http.MethodPost || captured[3].Path != "/api/v1/data/datasets/finance.expenses/records/query" {
		t.Fatalf("unexpected query_records request: %#v", captured[3])
	}
	if captured[4].Method != http.MethodPost || captured[4].Path != "/api/v1/data/object-roles/resolve" {
		t.Fatalf("unexpected approval resolver request: %#v", captured[4])
	}
	if captured[5].Method != http.MethodPost || captured[5].Path != "/api/v1/data/datasets/finance.expenses/records/exp-4/approvals" {
		t.Fatalf("unexpected approval start request: %#v", captured[5])
	}
	if got := strings.TrimSpace(asTestString(captured[5].Body["workflow_skill_id"])); got != "expense_approval" {
		t.Fatalf("approval workflow_skill_id = %q; body=%#v", got, captured[5].Body)
	}
	if got := strings.TrimSpace(asTestString(captured[5].Body["app_id"])); got != "mis.expense" {
		t.Fatalf("approval app_id = %q; body=%#v", got, captured[5].Body)
	}
	if got := strings.TrimSpace(asTestString(captured[5].Body["object_role"])); got != "expense_report" {
		t.Fatalf("approval object_role = %q; body=%#v", got, captured[5].Body)
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
