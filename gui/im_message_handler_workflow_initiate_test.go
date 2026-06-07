package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// mockAppForInitiation creates a minimal App with mock Hub credentials pointing to the given server URL.
func mockAppForInitiation(hubURL string) *App {
	return &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       hubURL,
			RemoteMachineToken: "test-token-123",
		},
	}
}

// mockHubServer creates a test HTTP server that simulates Hub API endpoints.
func mockHubServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workflows/published":
			resp := map[string]interface{}{
				"workflows": []map[string]interface{}{
					{
						"id":   "wf-leave-001",
						"name": "请假审批",
						"schema": []map[string]interface{}{
							{"name": "leave_type", "label": "请假类型", "type": "select", "required": true},
							{"name": "start_date", "label": "开始日期", "type": "date", "required": true},
							{"name": "duration", "label": "时长", "type": "text", "required": true},
							{"name": "reason", "label": "事由", "type": "text", "required": false},
						},
					},
					{
						"id":   "wf-expense-002",
						"name": "报销审批",
						"schema": []map[string]interface{}{
							{"name": "amount", "label": "金额", "type": "number", "required": true},
							{"name": "category", "label": "类别", "type": "select", "required": true},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/initiate"):
			var reqBody struct {
				FormData map[string]interface{} `json:"form_data"`
				Channel  string                 `json:"channel"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instance_id": "inst-20250501-042",
				"status":      "running",
				"created_at":  time.Now().UTC().Format(time.RFC3339Nano),
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// =============================================================================
// Test 1: Workflow Matching
// Validates: Requirements 2.1, 2.5
// =============================================================================

func TestWorkflowInitiation_MatchWorkflow(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()

	t.Run("message_matches_leave_workflow", func(t *testing.T) {
		resp, err := handler.HandleInitiationIntent(ctx, "user-match-1", "帮我发起请假审批")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !strings.Contains(resp.Text, "请假") {
			t.Errorf("response should reference matched workflow, got: %s", resp.Text)
		}
		handler.deleteSession("user-match-1")
	})

	t.Run("message_no_match_suggests_available", func(t *testing.T) {
		resp, err := handler.HandleInitiationIntent(ctx, "user-match-2", "random unrelated text about weather")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !strings.Contains(resp.Text, "未找到匹配") {
			t.Errorf("response should indicate no match, got: %s", resp.Text)
		}
		if !strings.Contains(resp.Text, "请假审批") || !strings.Contains(resp.Text, "报销审批") {
			t.Errorf("response should suggest available workflows, got: %s", resp.Text)
		}
	})
}

// =============================================================================
// Test 2: Field Extraction
// Validates: Requirements 2.1, 2.2, 2.4
// =============================================================================

func TestWorkflowInitiation_FieldExtraction(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()

	t.Run("extracts_date_and_type_fields", func(t *testing.T) {
		resp, err := handler.HandleInitiationIntent(ctx, "user-ext-1", "请假审批，明天一天，事假")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}

		session := handler.getSession("user-ext-1")
		if session == nil {
			t.Fatal("expected session to be created")
		}

		// "事假" should be extracted as leave_type
		if session.ExtractedData["leave_type"] != "事假" {
			t.Errorf("expected leave_type='事假', got: %v", session.ExtractedData["leave_type"])
		}

		// "明天" should be extracted as start_date
		tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		if session.ExtractedData["start_date"] != tomorrow {
			t.Errorf("expected start_date='%s', got: %v", tomorrow, session.ExtractedData["start_date"])
		}

		handler.deleteSession("user-ext-1")
	})

	t.Run("missing_required_fields_asks_user", func(t *testing.T) {
		resp, err := handler.HandleInitiationIntent(ctx, "user-ext-2", "帮我发起请假审批")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}

		session := handler.getSession("user-ext-2")
		if session == nil {
			t.Fatal("expected session to be created")
		}
		if len(session.MissingFields) == 0 {
			t.Error("expected missing fields since no field values were provided")
		}
		if !strings.Contains(resp.Text, "信息") {
			t.Errorf("response should ask for missing info, got: %s", resp.Text)
		}
		handler.deleteSession("user-ext-2")
	})
}

// =============================================================================
// Test 3: Confirmation Flow
// Validates: Requirements 2.3, 2.6
// =============================================================================

func TestWorkflowInitiation_ConfirmationFlow(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()

	setupConfirmableSession := func(userID string) {
		session := &initiationSession{
			UserID:       userID,
			WorkflowID:   "wf-leave-001",
			WorkflowName: "请假审批",
			Schema: []initiationFormField{
				{Name: "leave_type", Label: "请假类型", Type: "select", Required: true},
				{Name: "start_date", Label: "开始日期", Type: "date", Required: true},
				{Name: "duration", Label: "时长", Type: "text", Required: true},
				{Name: "reason", Label: "事由", Type: "text", Required: false},
			},
			ExtractedData: map[string]interface{}{
				"leave_type": "事假",
				"start_date": time.Now().AddDate(0, 0, 1).Format("2006-01-02"),
				"duration":   "1天",
			},
			MissingFields: nil,
			CreatedAt:     time.Now(),
		}
		handler.setSession(userID, session)
	}

	t.Run("user_confirms_submits_to_hub", func(t *testing.T) {
		setupConfirmableSession("user-confirm-1")

		resp, err := handler.HandleInitiationIntent(ctx, "user-confirm-1", "确认")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !strings.Contains(resp.Text, "✅") {
			t.Errorf("response should contain ✅, got: %s", resp.Text)
		}
		if !strings.Contains(resp.Text, "审批已发起") {
			t.Errorf("response should contain '审批已发起', got: %s", resp.Text)
		}
		if !strings.Contains(resp.Text, "WF-") {
			t.Errorf("response should contain 'WF-', got: %s", resp.Text)
		}
		if handler.getSession("user-confirm-1") != nil {
			t.Error("session should be cleared after submission")
		}
	})

	t.Run("user_cancels_clears_session", func(t *testing.T) {
		setupConfirmableSession("user-cancel-1")

		resp, err := handler.HandleInitiationIntent(ctx, "user-cancel-1", "取消")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(resp.Text, "已取消") {
			t.Errorf("response should confirm cancellation, got: %s", resp.Text)
		}
		if handler.getSession("user-cancel-1") != nil {
			t.Error("session should be cleared after cancellation")
		}
	})

	t.Run("user_modifies_field_re_presents", func(t *testing.T) {
		setupConfirmableSession("user-modify-1")

		resp, err := handler.HandleInitiationIntent(ctx, "user-modify-1", "类型改为年假")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(resp.Text, "已更新") {
			t.Errorf("response should show update, got: %s", resp.Text)
		}
		session := handler.getSession("user-modify-1")
		if session == nil {
			t.Fatal("session should still exist")
		}
		if session.ExtractedData["leave_type"] != "年假" {
			t.Errorf("leave_type should be '年假', got: %v", session.ExtractedData["leave_type"])
		}
		handler.deleteSession("user-modify-1")
	})
}

func TestWorkflowInitiation_UserPromptsUseConfiguredLanguage(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	app.CurrentLanguage = "en"
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()

	noMatch, err := handler.HandleInitiationIntent(ctx, "user-lang-no-match", "random unrelated text about weather")
	if err != nil {
		t.Fatalf("no match: %v", err)
	}
	if noMatch == nil || !strings.Contains(noMatch.Text, "No matching approval workflow") {
		t.Fatalf("expected English no-match prompt, got %#v", noMatch)
	}
	if strings.Contains(noMatch.Text, "未找到") || strings.Contains(noMatch.Text, "请指定") {
		t.Fatalf("no-match prompt leaked Chinese text: %q", noMatch.Text)
	}

	session := &initiationSession{
		UserID:       "user-lang-session",
		WorkflowID:   "wf-leave-001",
		WorkflowName: "Leave Approval",
		Schema: []initiationFormField{
			{Name: "leave_type", Label: "Leave type", Type: "select", Required: true},
			{Name: "reason", Label: "Reason", Type: "text", Required: false},
		},
		ExtractedData: map[string]interface{}{},
		MissingFields: []string{"leave_type"},
		CreatedAt:     time.Now(),
	}
	missing := handler.buildMissingFieldsPrompt(session)
	if missing == nil || !strings.Contains(missing.Text, "The following information is still required") {
		t.Fatalf("expected English missing-fields prompt, got %#v", missing)
	}
	if strings.Contains(missing.Text, "还需要") || strings.Contains(missing.Text, "请补充") {
		t.Fatalf("missing-fields prompt leaked Chinese text: %q", missing.Text)
	}
	if data := handler.presentExtractedData(session); !strings.Contains(data, "(not filled)") {
		t.Fatalf("expected English unset placeholder, got %q", data)
	}

	session.MissingFields = nil
	session.ExtractedData["leave_type"] = "Annual leave"
	handler.setSession(session.UserID, session)
	cancelled, err := handler.HandleInitiationIntent(ctx, session.UserID, "cancel")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled == nil || !strings.Contains(cancelled.Text, "Cancelled the initiation flow") {
		t.Fatalf("expected English cancellation prompt, got %#v", cancelled)
	}
	if strings.Contains(cancelled.Text, "已取消") || strings.Contains(cancelled.Text, "发起流程") {
		t.Fatalf("cancel prompt leaked Chinese text: %q", cancelled.Text)
	}
}

// =============================================================================
// Test 4: Multi-Turn Conversation
// Validates: Requirements 2.1, 2.2, 2.3, 2.4
// =============================================================================

func TestWorkflowInitiation_MultiTurnConversation(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()
	userID := "user-multi-1"

	// Turn 1: partial data → session created with missing fields
	resp, err := handler.HandleInitiationIntent(ctx, userID, "帮我发起请假审批，明天一天")
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if resp == nil {
		t.Fatal("turn 1: expected non-nil response")
	}

	session := handler.getSession(userID)
	if session == nil {
		t.Fatal("turn 1: session should be created")
	}
	if session.WorkflowID != "wf-leave-001" {
		t.Errorf("turn 1: expected workflow 'wf-leave-001', got: %s", session.WorkflowID)
	}

	// Turn 2: provide missing field
	if len(session.MissingFields) > 0 {
		resp, err = handler.HandleInitiationIntent(ctx, userID, "事假")
		if err != nil {
			t.Fatalf("turn 2 error: %v", err)
		}
		if resp == nil {
			t.Fatal("turn 2: expected non-nil response")
		}
	}

	// Ensure all fields are filled for confirmation
	session = handler.getSession(userID)
	if session == nil {
		t.Fatal("session should still exist")
	}
	if session.ExtractedData["leave_type"] == nil {
		session.ExtractedData["leave_type"] = "事假"
	}
	if session.ExtractedData["start_date"] == nil {
		session.ExtractedData["start_date"] = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	if session.ExtractedData["duration"] == nil {
		session.ExtractedData["duration"] = "一天"
	}
	session.MissingFields = nil
	handler.setSession(userID, session)

	// Turn 3: confirm → submits
	resp, err = handler.HandleInitiationIntent(ctx, userID, "确认")
	if err != nil {
		t.Fatalf("turn 3 error: %v", err)
	}
	if !strings.Contains(resp.Text, "✅") || !strings.Contains(resp.Text, "审批已发起") {
		t.Errorf("turn 3: expected success, got: %s", resp.Text)
	}
	if handler.getSession(userID) != nil {
		t.Error("turn 3: session should be cleared")
	}
}

// =============================================================================
// Test 5: Success Response Format
// Validates: Requirements 2.6
// =============================================================================

func TestWorkflowInitiation_SuccessResponseFormat(t *testing.T) {
	server := mockHubServer(t)
	defer server.Close()

	app := mockAppForInitiation(server.URL)
	handler := NewWorkflowInitiationHandler(app)
	ctx := context.Background()

	userID := "user-format-1"
	session := &initiationSession{
		UserID:       userID,
		WorkflowID:   "wf-leave-001",
		WorkflowName: "请假审批",
		Schema: []initiationFormField{
			{Name: "leave_type", Label: "请假类型", Type: "select", Required: true},
			{Name: "start_date", Label: "开始日期", Type: "date", Required: true},
			{Name: "duration", Label: "时长", Type: "text", Required: true},
		},
		ExtractedData: map[string]interface{}{
			"leave_type": "年假",
			"start_date": "2025-05-01",
			"duration":   "2天",
		},
		MissingFields: nil,
		CreatedAt:     time.Now(),
	}
	handler.setSession(userID, session)

	resp, err := handler.HandleInitiationIntent(ctx, userID, "确认")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: "✅ 审批已发起，单号：WF-{date}-{seq}"
	if !strings.Contains(resp.Text, "✅") {
		t.Errorf("missing ✅ in response: %s", resp.Text)
	}
	if !strings.Contains(resp.Text, "审批已发起") {
		t.Errorf("missing '审批已发起': %s", resp.Text)
	}
	if !strings.Contains(resp.Text, "单号：WF-") {
		t.Errorf("missing '单号：WF-': %s", resp.Text)
	}

	dateStr := time.Now().Format("20060102")
	expectedPrefix := fmt.Sprintf("WF-%s-", dateStr)
	if !strings.Contains(resp.Text, expectedPrefix) {
		t.Errorf("missing date component '%s': %s", expectedPrefix, resp.Text)
	}

	// Mock server returns instance_id "inst-20250501-042", seq should be "042"
	if !strings.Contains(resp.Text, "042") {
		t.Errorf("missing sequence '042': %s", resp.Text)
	}
}
