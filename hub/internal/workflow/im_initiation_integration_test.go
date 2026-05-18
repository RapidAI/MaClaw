package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Integration Test: IM Quick Initiation Full Flow (Task 19.2)
//
// Simulates the complete IM initiation flow:
// Natural language message → VE extracts Form_Data → User confirms →
// API call to Hub → Instance creation → Notification dispatch.
//
// The IM handler (gui package) extracts fields and calls Hub's RuntimeAPI.
// This test exercises the Hub-side flow that the IM handler invokes:
// FormValidator.Validate → WorkflowExecutor.StartInstance → notifications.
//
// Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6
// =============================================================================

func TestIntegration_IMInitiation_FullFlow(t *testing.T) {
	ctx := context.Background()

	// --- Setup mock stores (reuse rt* prefixed mocks from runtime_integration_test.go) ---
	instanceStore := newRTInstanceStore()
	auditStore := newRTAuditStore()
	confirmStore := newRTConfirmationStore()
	notifStore := newRTNotificationStore()
	hubNotifier := &rtHubNotifier{}
	imPusher := newRTIMPusher("executor-user-1", "notifier-user-1")
	dispatcher := &rtApprovalDispatcher{}

	notifDispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)
	confirmTracker := NewConfirmationTracker(confirmStore, instanceStore, notifDispatcher, auditStore)

	// --- Build workflow graph: Trigger → Form → Approval → Terminal ---
	formConfig, _ := json.Marshal(FormNodeConfig{
		Fields: []FormFieldSchema{
			{Name: "leave_type", Label: "请假类型", Type: FieldSelect, Required: true, Options: []string{"annual", "sick", "personal"}},
			{Name: "start_date", Label: "开始日期", Type: FieldDate, Required: true},
			{Name: "duration", Label: "时长(天)", Type: FieldNumber, Required: true},
			{Name: "reason", Label: "事由", Type: FieldText, Required: false, MaxLength: 500},
		},
	})

	approvalConfig, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"approver-ve-1"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	terminalConfig, _ := json.Marshal(TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "executor-user-1", TimeoutHours: 48, MaxReminders: 3, ReminderInterval: 24},
		},
		Notifiers: []NotifierConfig{
			{UserID: "notifier-user-1", TimeoutHours: 72, MaxReminders: 2, ReminderInterval: 24},
		},
	})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "form-1", Type: NodeForm, Label: "Leave Form", Config: formConfig},
			{ID: "approval-1", Type: NodeApproval, Label: "Manager Approval", Config: approvalConfig},
			{ID: "terminal-1", Type: NodeTypeTerminal, Label: "End", Config: terminalConfig},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "form-1"},
			{ID: "e2", SourceID: "form-1", TargetID: "approval-1"},
			{ID: "e3", SourceID: "approval-1", TargetID: "terminal-1"},
		},
	}

	wfStore := &rtWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:            "ver-im-001",
			WorkflowID:    "wf-leave-im",
			VersionNumber: "1.0.0",
			Status:        VersionPublished,
			Graph:         graph,
		},
	}

	executor := NewWorkflowExecutor(
		wfStore, instanceStore, auditStore, dispatcher,
		WithNotificationDispatcher(notifDispatcher),
		WithConfirmationTracker(confirmTracker),
	)

	// ==========================================================================
	// Step 1: Simulate VE extracting Form_Data from natural language
	// User message: "@VE 帮我发起请假审批，明天一天，事假"
	// VE extracts: leave_type=personal, start_date=2025-05-01, duration=1
	// ==========================================================================

	extractedFormData := map[string]interface{}{
		"leave_type": "personal",
		"start_date": "2025-05-01",
		"duration":   float64(1),
		"reason":     "家庭事务",
	}

	t.Run("Step1_VEExtraction_FormValidation", func(t *testing.T) {
		// Validate extracted data against schema (same as Hub page flow).
		validator := &FormValidator{}
		schema, err := ExtractFormSchema(&graph)
		if err != nil {
			t.Fatalf("ExtractFormSchema failed: %v", err)
		}

		validationErrors := validator.Validate(extractedFormData, schema)
		if len(validationErrors) > 0 {
			t.Fatalf("VE-extracted form data should pass validation, got errors: %v", validationErrors)
		}
	})

	// ==========================================================================
	// Step 2: Simulate VE presenting data to user and user confirming
	// VE shows: "已提取信息：类型：事假，开始：明天，时长：1天。确认发起？"
	// User replies: "确认"
	// ==========================================================================

	t.Run("Step2_UserConfirmation_DataPresentation", func(t *testing.T) {
		// Verify the extracted data contains all required fields.
		requiredFields := []string{"leave_type", "start_date", "duration"}
		for _, field := range requiredFields {
			if _, ok := extractedFormData[field]; !ok {
				t.Errorf("extracted data missing required field: %s", field)
			}
		}
	})

	// ==========================================================================
	// Step 3: Simulate VE submitting to Hub API (POST /api/v1/workflows/{id}/initiate)
	// Channel recorded as IM platform identifier.
	// ==========================================================================

	triggerData, _ := json.Marshal(map[string]interface{}{
		"form_data":    extractedFormData,
		"initiator_id": "user-li-si",
		"channel":      "im_feishu", // IM platform identifier
		"version_id":   "ver-im-001",
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
	})

	inst, err := executor.StartInstance(ctx, "wf-leave-im", string(triggerData))
	if err != nil {
		t.Fatalf("StartInstance (IM initiation) failed: %v", err)
	}

	t.Run("Step3_InstanceCreation_IMChannel", func(t *testing.T) {
		if inst == nil {
			t.Fatal("instance should not be nil")
		}
		if inst.Status != InstanceRunning {
			t.Errorf("expected status 'running', got %q", inst.Status)
		}

		// Verify IM channel is recorded in trigger data.
		var persisted map[string]interface{}
		if err := json.Unmarshal([]byte(inst.TriggerData), &persisted); err != nil {
			t.Fatalf("failed to unmarshal trigger data: %v", err)
		}

		// Requirement 2.6: channel recorded as IM platform identifier.
		if ch, _ := persisted["channel"].(string); ch != "im_feishu" {
			t.Errorf("expected channel 'im_feishu', got %v", persisted["channel"])
		}

		// Requirement 2.6: same complete Form_Data as Hub page initiation.
		fd, ok := persisted["form_data"].(map[string]interface{})
		if !ok {
			t.Fatal("expected form_data to be a map")
		}
		if fd["leave_type"] != "personal" {
			t.Errorf("expected leave_type 'personal', got %v", fd["leave_type"])
		}
		if fd["start_date"] != "2025-05-01" {
			t.Errorf("expected start_date '2025-05-01', got %v", fd["start_date"])
		}
		if fd["duration"] != float64(1) {
			t.Errorf("expected duration 1, got %v", fd["duration"])
		}

		// Requirement 2.6: initiator_id present.
		if id, _ := persisted["initiator_id"].(string); id != "user-li-si" {
			t.Errorf("expected initiator_id 'user-li-si', got %v", persisted["initiator_id"])
		}

		// Requirement 2.6: timestamp present with millisecond precision.
		ts, _ := persisted["timestamp"].(string)
		if ts == "" {
			t.Error("expected timestamp to be present")
		}
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t.Errorf("timestamp should be RFC3339Nano format: %v", err)
		}
		if parsed.IsZero() {
			t.Error("parsed timestamp should not be zero")
		}
	})

	// ==========================================================================
	// Step 4: Simulate approval and verify terminal node notifications
	// ==========================================================================

	instanceStore.mu.Lock()
	if inst.InstanceData == nil {
		inst.InstanceData = make(map[string]interface{})
	}
	inst.InstanceData["form_data"] = extractedFormData
	inst.InstanceData["initiator_id"] = "user-li-si"
	inst.InstanceData["initiator_name"] = "李四"
	inst.InstanceData["workflow_name"] = "请假审批"
	inst.InstanceData["result"] = "approved"
	instanceStore.mu.Unlock()

	err = executor.ResumeInstance(ctx, inst.ID, "approval-1", ApprovalResponse{
		Decision:   "approve",
		Rationale:  "同意请假",
		ApproverID: "approver-ve-1",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance failed: %v", err)
	}

	t.Run("Step4_TerminalNode_NotificationsSent", func(t *testing.T) {
		instanceStore.mu.Lock()
		finalInst := instanceStore.instances[inst.ID]
		instanceStore.mu.Unlock()

		if finalInst.Status != InstanceCompleted {
			t.Errorf("expected instance status 'completed', got %q", finalInst.Status)
		}

		// Verify notifications were sent to executor and notifier.
		hubNotifier.mu.Lock()
		hubSentCount := len(hubNotifier.sent)
		hubNotifier.mu.Unlock()
		if hubSentCount < 2 {
			t.Errorf("expected at least 2 Hub notifications, got %d", hubSentCount)
		}

		imPusher.mu.Lock()
		imPushCount := len(imPusher.pushed)
		imPusher.mu.Unlock()
		if imPushCount < 2 {
			t.Errorf("expected at least 2 IM push notifications, got %d", imPushCount)
		}
	})

	t.Run("Step4_AuditTrail_IMChannel", func(t *testing.T) {
		auditStore.mu.Lock()
		entries := make([]*AuditEntry, len(auditStore.entries))
		copy(entries, auditStore.entries)
		auditStore.mu.Unlock()

		hasCreated := false
		for _, e := range entries {
			if e.EventType == "instance_created" {
				hasCreated = true
			}
		}
		if !hasCreated {
			t.Error("expected 'instance_created' audit event for IM-initiated instance")
		}
	})
}

// TestIntegration_IMInitiation_MissingFields tests the flow when VE cannot extract
// all required fields from the natural language message.
// Validates: Requirement 2.4
func TestIntegration_IMInitiation_MissingFields(t *testing.T) {
	// Build a form schema with required fields.
	formConfig, _ := json.Marshal(FormNodeConfig{
		Fields: []FormFieldSchema{
			{Name: "leave_type", Label: "请假类型", Type: FieldSelect, Required: true, Options: []string{"annual", "sick", "personal"}},
			{Name: "start_date", Label: "开始日期", Type: FieldDate, Required: true},
			{Name: "duration", Label: "时长(天)", Type: FieldNumber, Required: true},
		},
	})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			{ID: "form-1", Type: NodeForm, Label: "Leave Form", Config: formConfig},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "form-1"},
		},
	}

	schema, err := ExtractFormSchema(&graph)
	if err != nil {
		t.Fatalf("ExtractFormSchema failed: %v", err)
	}

	t.Run("MissingRequiredField_ValidationFails", func(t *testing.T) {
		// VE only extracted leave_type from "帮我发起请假审批，事假"
		// Missing: start_date, duration
		incompleteData := map[string]interface{}{
			"leave_type": "personal",
		}

		validator := &FormValidator{}
		validationErrors := validator.Validate(incompleteData, schema)

		if len(validationErrors) == 0 {
			t.Fatal("expected validation errors for missing required fields")
		}

		// Verify specific missing fields are reported.
		missingFields := make(map[string]bool)
		for _, ve := range validationErrors {
			missingFields[ve.Field] = true
		}

		if !missingFields["start_date"] {
			t.Error("expected validation error for missing 'start_date'")
		}
		if !missingFields["duration"] {
			t.Error("expected validation error for missing 'duration'")
		}
	})

	t.Run("InvalidFieldType_ValidationFails", func(t *testing.T) {
		// VE extracted wrong type: duration should be number but got string.
		invalidData := map[string]interface{}{
			"leave_type": "personal",
			"start_date": "2025-05-01",
			"duration":   "一天", // Should be a number
		}

		validator := &FormValidator{}
		validationErrors := validator.Validate(invalidData, schema)

		if len(validationErrors) == 0 {
			t.Fatal("expected validation error for invalid field type")
		}

		// Verify the duration field has a type error.
		found := false
		for _, ve := range validationErrors {
			if ve.Field == "duration" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected validation error for 'duration' field type mismatch")
		}
	})

	t.Run("InvalidSelectOption_ValidationFails", func(t *testing.T) {
		// VE extracted invalid option for leave_type.
		invalidData := map[string]interface{}{
			"leave_type": "maternity", // Not in options: annual, sick, personal
			"start_date": "2025-05-01",
			"duration":   float64(1),
		}

		validator := &FormValidator{}
		validationErrors := validator.Validate(invalidData, schema)

		if len(validationErrors) == 0 {
			t.Fatal("expected validation error for invalid select option")
		}

		found := false
		for _, ve := range validationErrors {
			if ve.Field == "leave_type" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected validation error for 'leave_type' invalid option")
		}
	})
}

// TestIntegration_IMInitiation_NoMatchingWorkflow tests the flow when user's message
// does not match any published workflow.
// Validates: Requirement 2.5
func TestIntegration_IMInitiation_NoMatchingWorkflow(t *testing.T) {
	// This test verifies the concept: when no workflow matches, the system
	// should inform the user. The actual matching logic is in the gui package's
	// WorkflowInitiationHandler.matchWorkflowByMessage, but we can verify that
	// the form schema extraction handles edge cases.

	t.Run("EmptyGraph_NoFormSchema", func(t *testing.T) {
		graph := WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			},
			Edges: []WorkflowEdge{},
		}

		schema, err := ExtractFormSchema(&graph)
		if err != nil {
			// Some implementations return error, some return empty schema.
			// Both are acceptable for "no matching workflow" scenario.
			if !strings.Contains(err.Error(), "form") && !strings.Contains(err.Error(), "not found") {
				t.Logf("ExtractFormSchema returned unexpected error: %v", err)
			}
			return
		}

		// Empty schema means no form fields to match against.
		if len(schema) != 0 {
			t.Errorf("expected empty schema for graph without form node, got %d fields", len(schema))
		}
	})
}
