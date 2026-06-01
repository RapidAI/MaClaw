package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestBuildRegisteredToolAgentViewIncludesHiddenArgs(t *testing.T) {
	tool := RegisteredTool{
		Name:        "demo_tool",
		Description: "demo tool",
		InputSchema: map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "search query"},
			"limit": map[string]interface{}{"type": "integer"},
		},
		Required: []string{"query"},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"limit": float64(3)}, []string{"query"})
	if view == nil {
		t.Fatal("expected tool agent view")
	}
	if view["id"] != "tool:run:demo_tool" || view["type"] != "form" {
		t.Fatalf("unexpected view identity: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	if len(fields) != 4 {
		t.Fatalf("expected two visible fields plus hidden args/schema version, got %#v", fields)
	}
	var queryField map[string]interface{}
	var argsField map[string]interface{}
	var schemaVersionField map[string]interface{}
	for _, field := range fields {
		switch field["name"] {
		case "query":
			queryField = field
		case registeredToolAgentViewArgsField:
			argsField = field
		case agentViewSchemaVersionField:
			schemaVersionField = field
		}
	}
	if argsField == nil || argsField["type"] != "hidden" {
		t.Fatalf("expected hidden original args field, got %#v", argsField)
	}
	if schemaVersionField == nil || schemaVersionField["type"] != "hidden" || schemaVersionField["value"] == "" {
		t.Fatalf("expected hidden schema version field, got %#v", schemaVersionField)
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta["schemaVersion"] == "" || meta["schemaSource"] != "tool.adapter" {
		t.Fatalf("expected schema version metadata, got %#v", meta)
	}
	if queryField == nil || queryField["required"] != true || queryField["error"] == "" {
		t.Fatalf("expected required query field with error, got %#v", queryField)
	}
}

func TestRegisteredToolAgentViewReadsJSONSchemaRequired(t *testing.T) {
	tool := RegisteredTool{
		Name: "mcp_demo",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"path"},
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
		},
	}
	missing := registeredToolMissingRequired(&tool, map[string]interface{}{})
	if len(missing) != 1 || missing[0] != "path" {
		t.Fatalf("expected path missing from schema required, got %#v", missing)
	}
}

func TestRegisteredToolAgentViewInfersDirectoryField(t *testing.T) {
	tool := RegisteredTool{
		Name: "workspace_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"working_dir": map[string]interface{}{"type": "string", "description": "Working directory for command execution"},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, nil)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	if fields[0]["name"] != "working_dir" || fields[0]["type"] != "directory" {
		t.Fatalf("expected directory field, got %#v", fields[0])
	}
}

func TestRegisteredToolAgentViewDoesNotTreatRedirectAsDirectory(t *testing.T) {
	tool := RegisteredTool{
		Name: "oauth_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"redirect_url": map[string]interface{}{"type": "string", "description": "Callback redirect URL"},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, nil)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	if fields[0]["name"] != "redirect_url" || fields[0]["type"] != "text" {
		t.Fatalf("expected text redirect field, got %#v", fields[0])
	}
}

func TestRegisteredToolAgentViewDoesNotTreatPathologyAsFile(t *testing.T) {
	tool := RegisteredTool{
		Name: "medical_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pathology_note": map[string]interface{}{"type": "string", "description": "Clinical pathology note"},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, nil)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	if fields[0]["name"] != "pathology_note" || fields[0]["type"] != "text" {
		t.Fatalf("expected text pathology field, got %#v", fields[0])
	}
}

func TestRegisteredToolAgentViewStillInfersPathTokenAsFile(t *testing.T) {
	tool := RegisteredTool{
		Name: "file_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input_path": map[string]interface{}{"type": "string", "description": "Input path"},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, nil)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	if fields[0]["name"] != "input_path" || fields[0]["type"] != "file" {
		t.Fatalf("expected file path field, got %#v", fields[0])
	}
}

func TestRegisteredToolAgentViewInfersDirectoryObjectColumn(t *testing.T) {
	tool := RegisteredTool{
		Name: "workspace_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"settings": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"working_dir": map[string]interface{}{"type": "string", "description": "Working directory for command execution"},
					},
				},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, nil)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	columns, ok := fields[0]["columns"].([]map[string]interface{})
	if fields[0]["type"] != "object_form" || !ok || len(columns) == 0 {
		t.Fatalf("expected object_form with columns, got %#v", fields[0])
	}
	if columns[0]["name"] != "working_dir" || columns[0]["type"] != "directory" {
		t.Fatalf("expected directory object column, got %#v", columns[0])
	}
}

func TestRegisteredToolMissingRequiredRecognizesTypedEmptySlices(t *testing.T) {
	tool := RegisteredTool{Name: "typed", Required: []string{"tags", "items"}}
	missing := registeredToolMissingRequired(&tool, map[string]interface{}{
		"tags":  []string{},
		"items": []map[string]interface{}{},
	})
	if len(missing) != 2 || missing[0] != "items" || missing[1] != "tags" {
		t.Fatalf("expected typed empty slices to be missing, got %#v", missing)
	}
}

func TestHandleRegisteredToolAgentViewSubmitMergesAndRuns(t *testing.T) {
	var called map[string]interface{}
	h := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{
		Name: "demo_tool",
		InputSchema: map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
			"body":  map[string]interface{}{"type": "object"},
		},
		Required: []string{"query"},
		Handler: func(args map[string]interface{}) string {
			called = args
			return "ok"
		},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("demo_tool", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{"existing": "kept"},
		"query":                          "hello",
		"body":                           `{"amount":86}`,
	})
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "completed") {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if called["existing"] != "kept" || called["query"] != "hello" {
		t.Fatalf("expected merged args, got %#v", called)
	}
	body, ok := called["body"].(map[string]interface{})
	if !ok || body["amount"] != float64(86) {
		t.Fatalf("expected object textarea to be parsed as JSON, got %#v", called["body"])
	}
}

func TestHandleRegisteredToolAgentViewSubmitHonorsWorkflowPolicy(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.registry = NewToolRegistry()
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name:     "write_file",
		Required: []string{"path", "content"},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "wrote"
		},
	}); err != nil {
		t.Fatal(err)
	}
	userID := "agent-view-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("write_file", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{registeredToolPolicyOwnerIDField: userID},
		"path":                           "out.txt",
		"content":                        "x",
	})
	if called {
		t.Fatal("task panel submit must not run workflow-blocked registered tool")
	}
	if resp == nil || !strings.Contains(resp.Text, "not allowed by the current workflow tool policy") {
		t.Fatalf("expected workflow policy rejection, got %#v", resp)
	}
}

func TestHandleRegisteredToolAgentViewSubmitStripsRuntimePolicyOwner(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	var seenOwner bool
	if err := h.registry.Register(RegisteredTool{
		Name:     "capture_args",
		Required: []string{"value"},
		Handler: func(args map[string]interface{}) string {
			_, seenOwner = args[registeredToolPolicyOwnerIDField]
			return "captured"
		},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("capture_args", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{registeredToolPolicyOwnerIDField: "remote:mobile"},
		"value":                          "x",
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if seenOwner {
		t.Fatal("registered tool handler must not receive hidden runtime policy owner")
	}
}

func TestHandleRegisteredToolAgentViewSubmitKeepsOwnerForOwnerAwareTool(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	var seenOwner string
	if err := h.registry.Register(RegisteredTool{
		Name:     "memory",
		Required: []string{"value"},
		Handler: func(args map[string]interface{}) string {
			seenOwner = consumeRuntimePolicyOwnerIDFromToolArgs(args)
			return "captured"
		},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("memory", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{registeredToolPolicyOwnerIDField: "remote:mobile"},
		"value":                          "x",
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if seenOwner != "remote:mobile" {
		t.Fatalf("owner-aware registered tool owner = %q, want remote:mobile", seenOwner)
	}
}

func TestHandleRegisteredToolAgentViewSubmitEmptyOwnerAwareRuntimeOwnerFailsClosed(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name:     "memory",
		Required: []string{"value"},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "captured"
		},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("memory", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{registeredToolPolicyOwnerIDField: ""},
		"value":                          "x",
	})
	if called {
		t.Fatal("owner-aware registered tool should not run with empty runtime owner")
	}
	if resp == nil || !strings.Contains(resp.Error, "runtime owner is missing") {
		t.Fatalf("expected runtime owner rejection, got %#v", resp)
	}
}

func TestHandleRegisteredToolAgentViewSubmitWithoutOwnerDoesNotUseLegacyFallback(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.registry = NewToolRegistry()
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name:     "write_file",
		Required: []string{"path", "content"},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "wrote"
		},
	}); err != nil {
		t.Fatal(err)
	}
	userID := "agent-view-legacy-fallback-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("write_file", map[string]interface{}{
		"path":    "out.txt",
		"content": "x",
	})
	if !called {
		t.Fatal("task panel submit without explicit owner should not inherit unrelated workflow policy")
	}
	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleRegisteredToolAgentViewSubmitWithoutOwnerDoesNotUseLastUserID(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.registry = NewToolRegistry()
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name:     "write_file",
		Required: []string{"path", "content"},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "wrote"
		},
	}); err != nil {
		t.Fatal(err)
	}
	userID := "agent-view-last-user-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	h.lastUserID = userID

	resp := h.handleRegisteredToolAgentViewSubmit("write_file", map[string]interface{}{
		"path":    "out.txt",
		"content": "x",
	})
	if !called {
		t.Fatal("task panel submit without explicit owner should not inherit lastUserID workflow policy")
	}
	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleRegisteredToolAgentViewSubmitWithoutOwnerDoesNotUseCurrentRuntime(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.registry = NewToolRegistry()
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name:     "write_file",
		Required: []string{"path", "content"},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "wrote"
		},
	}); err != nil {
		t.Fatal(err)
	}
	userID := "agent-view-current-runtime-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	h.currentLoopCtx = &LoopContext{Runtime: RuntimeContext{RequestID: "req-current", PolicyOwnerID: userID}}

	resp := h.handleRegisteredToolAgentViewSubmit("write_file", map[string]interface{}{
		"path":    "out.txt",
		"content": "x",
	})
	if !called {
		t.Fatal("task panel submit without hidden owner should not inherit current runtime workflow policy")
	}
	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestRegisteredToolAgentViewCarriesRuntimePolicyOwner(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-1", PolicyOwnerID: "owner-1"}}}
	tool := RegisteredTool{Name: "write_file", Required: []string{"path"}}
	view := buildRegisteredToolAgentView(tool, h.attachRegisteredToolPolicyOwner(map[string]interface{}{"content": "x"}), []string{"path"})
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("fields = %#v", view["fields"])
	}
	for _, field := range fields {
		if field["name"] != registeredToolAgentViewArgsField {
			continue
		}
		args, _ := field["value"].(map[string]interface{})
		if args[registeredToolPolicyOwnerIDField] != "owner-1" {
			t.Fatalf("hidden policy owner = %#v", args[registeredToolPolicyOwnerIDField])
		}
		return
	}
	t.Fatal("hidden args field missing")
}

func TestHandleRegisteredToolAgentViewSubmitRejectsSchemaConstraints(t *testing.T) {
	called := false
	h := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{
		Name: "validated_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"email": map[string]interface{}{"type": "string", "format": "email"},
				"limit": map[string]interface{}{"type": "integer", "minimum": float64(1), "maximum": float64(10)},
			},
		},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ok"
		},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.handleRegisteredToolAgentViewSubmit("validated_tool", map[string]interface{}{
		"email": "bad-email",
		"limit": float64(99),
	})
	if resp == nil || resp.Error == "" || !strings.Contains(resp.Error, "valid email") || !strings.Contains(resp.Error, "at most 10") {
		t.Fatalf("expected schema validation errors, got %#v", resp)
	}
	if called {
		t.Fatal("handler should not be called with invalid task panel data")
	}
}

func TestExecuteToolOpensAgentViewForInvalidRegisteredToolArgs(t *testing.T) {
	registry := NewToolRegistry()
	called := false
	if err := registry.Register(RegisteredTool{
		Name: "validated_tool",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"mode"},
			"properties": map[string]interface{}{
				"mode": map[string]interface{}{"type": "string", "enum": []interface{}{"safe", "dry_run"}},
			},
		},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ran"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	handler := &IMMessageHandler{registry: registry}

	result := handler.executeTool("validated_tool", `{"mode":"delete_all"}`, nil)

	if called {
		t.Fatal("handler should not run when registered tool args fail schema validation")
	}
	if !strings.Contains(result, "Tool parameters need correction") {
		t.Fatalf("expected correction message, got %q", result)
	}
}

func TestHandleRegisteredToolApprovalSubmitRunsApprovedTool(t *testing.T) {
	registry := NewToolRegistry()
	called := false
	if err := registry.Register(RegisteredTool{
		Name: "bash",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"command", "session_id"},
			"properties": map[string]interface{}{
				"command":    map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
		},
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ran " + fmt.Sprint(args["command"])
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	firewall := NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngine(), nil)
	handler := &IMMessageHandler{registry: registry, firewall: firewall}
	approval := storeRegisteredToolPendingApproval("bash", map[string]interface{}{
		"command":    "curl -X POST https://example.com",
		"session_id": "sess-1",
	}, "sess-1", "", firewall.analyzer.Assess("bash", map[string]interface{}{"command": "curl -X POST https://example.com"}, &SecurityCallContext{SessionID: "sess-1"}))

	resp := handler.handleRegisteredToolApprovalAgentViewSubmit(map[string]interface{}{
		"approved": true,
		"parameters": map[string]interface{}{
			registeredToolApprovalIDField: approval.ID,
		},
	})

	if resp == nil || resp.Error != "" {
		t.Fatalf("unexpected approval response: %#v", resp)
	}
	if !called {
		t.Fatal("expected approved tool to run")
	}
	if _, ok := getRegisteredToolPendingApproval(approval.ID); ok {
		t.Fatal("expected approval to be removed after handling")
	}
	if !firewall.isSessionApproved("sess-1", "bash") {
		t.Fatal("expected session approval to be recorded")
	}
}

func TestHandleRegisteredToolApprovalSubmitHonorsWorkflowPolicyBeforeApprovingSession(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.registry = NewToolRegistry()
	called := false
	if err := h.registry.Register(RegisteredTool{
		Name: "bash",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ran"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	firewall := NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngine(), nil)
	h.firewall = firewall
	userID := "agent-view-approval-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	approval := storeRegisteredToolPendingApproval("bash", map[string]interface{}{
		"command":    "echo hi",
		"session_id": "sess-1",
	}, "sess-1", userID, firewall.analyzer.Assess("bash", map[string]interface{}{"command": "echo hi"}, &SecurityCallContext{SessionID: "sess-1"}))

	resp := h.handleRegisteredToolApprovalAgentViewSubmit(map[string]interface{}{
		"approved": true,
		"parameters": map[string]interface{}{
			registeredToolApprovalIDField: approval.ID,
		},
	})
	if called {
		t.Fatal("approval submit must not run workflow-blocked registered tool")
	}
	if resp == nil || !strings.Contains(resp.Text, "not allowed by the current workflow tool policy") {
		t.Fatalf("expected workflow policy rejection, got %#v", resp)
	}
	if firewall.isSessionApproved("sess-1", "bash") {
		t.Fatal("workflow-blocked approval must not approve future session calls")
	}
	if _, ok := getRegisteredToolPendingApproval(approval.ID); ok {
		t.Fatal("expected workflow-blocked approval to be removed after handling")
	}
}

func TestBuildRegisteredToolApprovalAgentViewCarriesApprovalID(t *testing.T) {
	view := buildRegisteredToolApprovalAgentView(registeredToolPendingApproval{
		ID:       "approval-1",
		ToolName: "bash",
		Risk: security.RiskAssessment{
			Level:  security.RiskHigh,
			Reason: "network post",
		},
	})
	if view["type"] != "approval" || view["id"] != "tool:approval" {
		t.Fatalf("unexpected approval view: %#v", view)
	}
	action, ok := view["action"].(map[string]interface{})
	if !ok || action["risk"] != "high" {
		t.Fatalf("expected high-risk action, got %#v", view["action"])
	}
	parameters, ok := action["parameters"].(map[string]interface{})
	if !ok || parameters[registeredToolApprovalIDField] != "approval-1" {
		t.Fatalf("expected approval id in parameters, got %#v", action["parameters"])
	}
}

func TestRegisteredToolValidateArgsChecksNestedArrayRows(t *testing.T) {
	tool := RegisteredTool{
		Name: "batch_expense",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"items": map[string]interface{}{
					"type":     "array",
					"minItems": float64(1),
					"items": map[string]interface{}{
						"type":     "object",
						"required": []interface{}{"amount", "note"},
						"properties": map[string]interface{}{
							"amount": map[string]interface{}{"type": "number", "minimum": float64(1)},
							"note":   map[string]interface{}{"type": "string", "minLength": float64(3)},
						},
					},
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"amount": float64(0), "note": "x"}},
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "amount must be at least 1") || !strings.Contains(joined, "note must be at least 3 characters") {
		t.Fatalf("expected nested row validation errors, got %#v", errors)
	}
}

func TestRegisteredToolValidationIssuesCarryTopLevelPaths(t *testing.T) {
	tool := RegisteredTool{
		Name: "validated_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"amount": map[string]interface{}{"type": "number", "minimum": float64(1)},
						},
					},
				},
				"email": map[string]interface{}{"type": "string", "format": "email"},
			},
		},
	}
	issues := registeredToolValidateArgIssues(tool, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"amount": float64(0)}},
		"email": "bad-email",
	})
	if len(issues) != 2 {
		t.Fatalf("expected two issues, got %#v", issues)
	}
	paths := map[string]bool{}
	for _, issue := range issues {
		paths[registeredToolTopLevelPath(issue.Path)] = true
	}
	if !paths["items"] || !paths["email"] {
		t.Fatalf("expected top-level paths for items and email, got %#v", issues)
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"email": "bad-email"}, nil)
	applyRegisteredToolFieldIssues(view, issues)
	fields := view["fields"].([]map[string]interface{})
	fieldErrors := map[string]string{}
	for _, field := range fields {
		if errText, _ := field["error"].(string); errText != "" {
			fieldErrors[fmt.Sprint(field["name"])] = errText
		}
	}
	if fieldErrors["items"] == "" || fieldErrors["email"] == "" {
		t.Fatalf("expected field-level errors, got %#v", fields)
	}
}

func TestRegisteredToolValidateArgsRejectsEnumValues(t *testing.T) {
	tool := RegisteredTool{
		Name: "enum_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{"type": "string", "enum": []interface{}{"open", "closed"}},
				"scopes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []interface{}{"read", "write"},
					},
				},
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"category": map[string]interface{}{"type": "string", "enum": []interface{}{"transport", "meal"}},
						},
					},
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"status": "deleted",
		"scopes": []interface{}{"read", "admin"},
		"items":  []interface{}{map[string]interface{}{"category": "hotel"}},
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "status must be one of: open, closed") {
		t.Fatalf("expected top-level enum error, got %#v", errors)
	}
	if !strings.Contains(joined, "scopes[2] must be one of: read, write") {
		t.Fatalf("expected array item enum error, got %#v", errors)
	}
	if !strings.Contains(joined, "items[1].category must be one of: transport, meal") {
		t.Fatalf("expected nested row enum error, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsRejectsExclusiveAndMultipleOf(t *testing.T) {
	tool := RegisteredTool{
		Name: "number_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"amount": map[string]interface{}{
					"type":             "number",
					"exclusiveMinimum": float64(0),
					"exclusiveMaximum": float64(10),
					"multipleOf":       float64(0.5),
				},
				"legacy": map[string]interface{}{
					"type":             "number",
					"minimum":          float64(1),
					"exclusiveMinimum": true,
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"amount": float64(10),
		"legacy": float64(1),
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "amount must be less than 10") {
		t.Fatalf("expected exclusive maximum error, got %#v", errors)
	}
	if !strings.Contains(joined, "legacy must be greater than 1") {
		t.Fatalf("expected legacy exclusive minimum error, got %#v", errors)
	}

	errors = registeredToolValidateArgs(tool, map[string]interface{}{"amount": float64(1.25)})
	if !strings.Contains(strings.Join(errors, "; "), "amount must be a multiple of 0.5") {
		t.Fatalf("expected multipleOf error, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsRejectsConstMismatch(t *testing.T) {
	tool := RegisteredTool{
		Name: "const_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":  map[string]interface{}{"type": "string", "const": "expense.submit"},
				"version": map[string]interface{}{"type": "integer", "const": float64(2)},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"action":  "expense.delete",
		"version": float64(1),
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "action must be expense.submit") || !strings.Contains(joined, "version must be 2") {
		t.Fatalf("expected const validation errors, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsRejectsDuplicateUniqueItems(t *testing.T) {
	tool := RegisteredTool{
		Name: "unique_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tags": map[string]interface{}{"type": "array", "uniqueItems": true},
				"items": map[string]interface{}{
					"type":        "array",
					"uniqueItems": true,
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"category": map[string]interface{}{"type": "string"},
							"amount":   map[string]interface{}{"type": "number"},
						},
					},
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"tags": []interface{}{"finance", "finance"},
		"items": []interface{}{
			map[string]interface{}{"category": "meal", "amount": float64(86)},
			map[string]interface{}{"category": "meal", "amount": float64(86)},
		},
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "tags must not contain duplicate items") || !strings.Contains(joined, "items must not contain duplicate items") {
		t.Fatalf("expected uniqueItems validation errors, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsRejectsAdditionalProperties(t *testing.T) {
	tool := RegisteredTool{
		Name: "strict_tool",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
				"filter": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"status": map[string]interface{}{"type": "string"},
					},
				},
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"amount": map[string]interface{}{"type": "number"},
						},
					},
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"query": "hello",
		"extra": "nope",
		"filter": map[string]interface{}{
			"status":  "open",
			"unknown": true,
		},
		"items": []interface{}{map[string]interface{}{"amount": float64(12), "memo": "x"}},
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "Parameters.extra is not allowed") {
		t.Fatalf("expected top-level additional property error, got %#v", errors)
	}
	if !strings.Contains(joined, "filter.unknown is not allowed") {
		t.Fatalf("expected nested additional property error, got %#v", errors)
	}
	if !strings.Contains(joined, "items[1].memo is not allowed") {
		t.Fatalf("expected row additional property error, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsRejectsDependentRequired(t *testing.T) {
	tool := RegisteredTool{
		Name: "dependent_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"invoice_no": map[string]interface{}{"type": "string"},
				"receipt":    map[string]interface{}{"type": "string"},
				"filter": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{"type": "string"},
						"reason": map[string]interface{}{"type": "string"},
					},
					"dependentRequired": map[string]interface{}{
						"status": []interface{}{"reason"},
					},
				},
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"card_no": map[string]interface{}{"type": "string"},
							"bank":    map[string]interface{}{"type": "string"},
						},
						"dependencies": map[string]interface{}{
							"card_no": []interface{}{"bank"},
						},
					},
				},
			},
			"dependentRequired": map[string]interface{}{
				"invoice_no": []interface{}{"receipt"},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"invoice_no": "INV-1",
		"filter":     map[string]interface{}{"status": "rejected"},
		"items":      []interface{}{map[string]interface{}{"card_no": "6222"}},
	})
	joined := strings.Join(errors, "; ")
	if !strings.Contains(joined, "Parameters.receipt is required when invoice_no is provided") {
		t.Fatalf("expected top-level dependentRequired error, got %#v", errors)
	}
	if !strings.Contains(joined, "filter.reason is required when status is provided") {
		t.Fatalf("expected nested dependentRequired error, got %#v", errors)
	}
	if !strings.Contains(joined, "items[1].bank is required when card_no is provided") {
		t.Fatalf("expected row dependencies error, got %#v", errors)
	}
}

func TestBuildRegisteredToolAgentViewIncludesDependentRequired(t *testing.T) {
	tool := RegisteredTool{
		Name: "dependent_tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"invoice_no": map[string]interface{}{"type": "string"},
				"receipt":    map[string]interface{}{"type": "string"},
			},
			"dependentRequired": map[string]interface{}{
				"invoice_no": []interface{}{"receipt"},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"invoice_no": "INV-1"}, nil)
	deps, ok := view["dependentRequired"].(map[string][]string)
	if !ok || len(deps["invoice_no"]) != 1 || deps["invoice_no"][0] != "receipt" {
		t.Fatalf("expected dependentRequired in agent view, got %#v", view["dependentRequired"])
	}
}

func TestBuildRegisteredToolAgentViewIncludesSchemaVariants(t *testing.T) {
	tool := RegisteredTool{
		Name: "expense_submit",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"applicant": map[string]interface{}{"type": "string"},
			},
			"oneOf": []interface{}{
				map[string]interface{}{
					"$id":      "travel",
					"title":    "Travel",
					"required": []interface{}{"destination"},
					"properties": map[string]interface{}{
						"destination": map[string]interface{}{"type": "string"},
					},
				},
				map[string]interface{}{
					"$id":      "meal",
					"title":    "Meal",
					"required": []interface{}{"restaurant"},
					"properties": map[string]interface{}{
						"restaurant": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"applicant": "Alice"}, nil)
	variants, ok := view["variants"].([]map[string]interface{})
	if !ok || len(variants) != 2 {
		t.Fatalf("expected two schema variants, got %#v", view["variants"])
	}
	if variants[0]["id"] != "travel" || variants[0]["label"] != "Travel" {
		t.Fatalf("expected travel variant metadata, got %#v", variants[0])
	}
	fields, ok := variants[0]["fields"].([]map[string]interface{})
	if !ok || len(fields) != 1 || fields[0]["name"] != "destination" || fields[0]["required"] != true {
		t.Fatalf("expected travel variant fields without hidden args, got %#v", variants[0]["fields"])
	}
}

func TestRegisteredToolValidateArgsChecksSelectedSchemaVariant(t *testing.T) {
	tool := RegisteredTool{
		Name: "expense_submit",
		InputSchema: map[string]interface{}{
			"type": "object",
			"oneOf": []interface{}{
				map[string]interface{}{
					"$id":      "travel",
					"title":    "Travel",
					"required": []interface{}{"destination"},
					"properties": map[string]interface{}{
						"destination": map[string]interface{}{"type": "string", "minLength": float64(3)},
					},
				},
				map[string]interface{}{
					"$id":      "meal",
					"title":    "Meal",
					"required": []interface{}{"restaurant"},
					"properties": map[string]interface{}{
						"restaurant": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	errors := registeredToolValidateArgs(tool, map[string]interface{}{"_agent_view_variant": "travel"})
	if !strings.Contains(strings.Join(errors, "; "), "destination") {
		t.Fatalf("expected selected variant required field error, got %#v", errors)
	}
	errors = registeredToolValidateArgs(tool, map[string]interface{}{"_agent_view_variant": "travel", "destination": "NY"})
	if !strings.Contains(strings.Join(errors, "; "), "destination must be at least 3 characters") {
		t.Fatalf("expected selected variant field validation, got %#v", errors)
	}
	errors = registeredToolValidateArgs(tool, map[string]interface{}{"_agent_view_variant": "meal", "restaurant": "Noodle House"})
	if len(errors) != 0 {
		t.Fatalf("expected selected meal variant to pass, got %#v", errors)
	}
}

func TestRegisteredToolValidateArgsAllowsSelectedVariantPropertiesWithStrictBaseSchema(t *testing.T) {
	tool := RegisteredTool{
		Name: "expense_submit",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"applicant": map[string]interface{}{"type": "string"},
			},
			"oneOf": []interface{}{
				map[string]interface{}{
					"$id": "travel",
					"properties": map[string]interface{}{
						"destination": map[string]interface{}{"type": "string"},
					},
				},
				map[string]interface{}{
					"$id": "meal",
					"properties": map[string]interface{}{
						"restaurant": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}

	errors := registeredToolValidateArgs(tool, map[string]interface{}{
		"_agent_view_variant": "meal",
		"applicant":           "Alice",
		"restaurant":          "Noodle House",
	})
	if len(errors) != 0 {
		t.Fatalf("expected selected variant property to pass strict base schema, got %#v", errors)
	}

	errors = registeredToolValidateArgs(tool, map[string]interface{}{
		"_agent_view_variant": "meal",
		"applicant":           "Alice",
		"destination":         "Shanghai",
	})
	if !strings.Contains(strings.Join(errors, "; "), "Parameters.destination is not allowed") {
		t.Fatalf("expected inactive variant property to be rejected, got %#v", errors)
	}
}

func TestBuildRegisteredToolAgentViewArraySchemaUsesTable(t *testing.T) {
	tool := RegisteredTool{
		Name: "batch_expense",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"items"},
			"properties": map[string]interface{}{
				"items": map[string]interface{}{
					"type":        "array",
					"minItems":    float64(1),
					"maxItems":    float64(5),
					"uniqueItems": true,
					"items": map[string]interface{}{
						"type":     "object",
						"required": []interface{}{"amount"},
						"properties": map[string]interface{}{
							"category": map[string]interface{}{"type": "string", "enum": []interface{}{"transport", "meal"}},
							"amount":   map[string]interface{}{"type": "number", "minimum": float64(1), "maximum": float64(5000), "multipleOf": float64(0.01)},
							"note":     map[string]interface{}{"type": "string", "minLength": float64(3), "maxLength": float64(120), "pattern": "^[^<>]+$", "const": "receipt", "readOnly": true},
							"token":    map[string]interface{}{"type": "string", "writeOnly": true, "format": "password"},
						},
						"dependentRequired": map[string]interface{}{
							"category": []interface{}{"note"},
						},
					},
				},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{}, []string{"items"})
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	var itemsField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "items" {
			itemsField = field
			break
		}
	}
	if itemsField == nil || itemsField["type"] != "array_table" {
		t.Fatalf("expected array_table items field, got %#v", itemsField)
	}
	if itemsField["minItems"] != float64(1) || itemsField["maxItems"] != float64(5) {
		t.Fatalf("expected array item constraints, got %#v", itemsField)
	}
	if itemsField["uniqueItems"] != true {
		t.Fatalf("expected uniqueItems constraint, got %#v", itemsField)
	}
	deps, ok := itemsField["dependentRequired"].(map[string][]string)
	if !ok || len(deps["category"]) != 1 || deps["category"][0] != "note" {
		t.Fatalf("expected row dependentRequired constraint, got %#v", itemsField["dependentRequired"])
	}
	columns, ok := itemsField["columns"].([]map[string]interface{})
	if !ok || len(columns) != 4 {
		t.Fatalf("expected inferred columns, got %#v", itemsField["columns"])
	}
	if columns[0]["name"] != "amount" || columns[0]["type"] != "number" {
		t.Fatalf("expected sorted numeric amount column, got %#v", columns)
	}
	if columns[0]["required"] != true {
		t.Fatalf("expected nested required amount column, got %#v", columns)
	}
	if columns[0]["min"] != float64(1) || columns[0]["max"] != float64(5000) {
		t.Fatalf("expected nested numeric constraints, got %#v", columns)
	}
	if columns[0]["step"] != float64(0.01) {
		t.Fatalf("expected nested numeric step, got %#v", columns)
	}
	if columns[1]["name"] != "category" || columns[1]["type"] != "select" {
		t.Fatalf("expected category select column, got %#v", columns)
	}
	if columns[2]["name"] != "note" || columns[2]["minLength"] != float64(3) || columns[2]["maxLength"] != float64(120) || columns[2]["pattern"] != "^[^<>]+$" {
		t.Fatalf("expected nested text constraints, got %#v", columns)
	}
	if columns[2]["constValue"] != "receipt" {
		t.Fatalf("expected nested const constraint, got %#v", columns)
	}
	if columns[2]["readOnly"] != true {
		t.Fatalf("expected nested readOnly annotation, got %#v", columns)
	}
	if columns[3]["name"] != "token" || columns[3]["sensitive"] != true {
		t.Fatalf("expected nested sensitive annotation, got %#v", columns)
	}
}

func TestBuildRegisteredToolAgentViewArrayEnumUsesMultiselect(t *testing.T) {
	tool := RegisteredTool{
		Name: "tag_tool",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"tags"},
			"properties": map[string]interface{}{
				"tags": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []interface{}{"finance", "urgent", "audit"},
					},
				},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"tags": []interface{}{"finance"}}, nil)
	fields := view["fields"].([]map[string]interface{})
	var tagsField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "tags" {
			tagsField = field
			break
		}
	}
	if tagsField == nil || tagsField["type"] != "multiselect" {
		t.Fatalf("expected multiselect tags field, got %#v", tagsField)
	}
	options, ok := tagsField["options"].([]map[string]interface{})
	if !ok || len(options) != 3 {
		t.Fatalf("expected multiselect options, got %#v", tagsField["options"])
	}
}

func TestBuildRegisteredToolAgentViewObjectSchemaUsesObjectForm(t *testing.T) {
	tool := RegisteredTool{
		Name: "filter_tool",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"filter"},
			"properties": map[string]interface{}{
				"filter": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{"type": "string", "enum": []interface{}{"open", "closed"}, "format": "status-code"},
						"limit":  map[string]interface{}{"type": "integer", "minimum": float64(1), "maximum": float64(100)},
					},
					"required": []interface{}{"status"},
					"dependentRequired": map[string]interface{}{
						"status": []interface{}{"limit"},
					},
				},
			},
		},
	}
	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"filter": map[string]interface{}{"status": "open"}}, nil)
	fields := view["fields"].([]map[string]interface{})
	var filterField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "filter" {
			filterField = field
			break
		}
	}
	if filterField == nil || filterField["type"] != "object_form" {
		t.Fatalf("expected object_form filter field, got %#v", filterField)
	}
	deps, ok := filterField["dependentRequired"].(map[string][]string)
	if !ok || len(deps["status"]) != 1 || deps["status"][0] != "limit" {
		t.Fatalf("expected object dependentRequired constraint, got %#v", filterField["dependentRequired"])
	}
	columns, ok := filterField["columns"].([]map[string]interface{})
	if !ok || len(columns) != 2 {
		t.Fatalf("expected object form columns, got %#v", filterField["columns"])
	}
	if columns[1]["name"] != "status" || columns[1]["type"] != "select" {
		t.Fatalf("expected status select column, got %#v", columns)
	}
	if columns[1]["required"] != true {
		t.Fatalf("expected nested required status column, got %#v", columns)
	}
	if columns[1]["format"] != "status-code" {
		t.Fatalf("expected nested format hint, got %#v", columns)
	}
	if columns[0]["name"] != "limit" || columns[0]["min"] != float64(1) || columns[0]["max"] != float64(100) {
		t.Fatalf("expected object numeric constraints, got %#v", columns)
	}
}

func TestBuildRegisteredToolAgentViewUsesResourcePickerAnnotation(t *testing.T) {
	tool := RegisteredTool{
		Name:        "assign_approver",
		Description: "Assign an approver.",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"approver_id"},
			"properties": map[string]interface{}{
				"approver_id": map[string]interface{}{
					"type":            "string",
					"x-agent-view":    "resource_picker",
					"x-resource-type": "employee",
					"resources": []interface{}{
						map[string]interface{}{"id": "u1", "label": "Alice", "status": "Finance", "description": "Finance manager", "data": map[string]interface{}{"team": "FIN"}},
						map[string]interface{}{"id": "u2", "label": "Bob"},
					},
				},
			},
		},
	}

	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"draft": "kept"}, []string{"approver_id"})
	if view["type"] != "resource_picker" {
		t.Fatalf("expected resource_picker view, got %#v", view)
	}
	if view["id"] != "tool:run:assign_approver" || view["dataKey"] != "approver_id" || view["resourceType"] != "employee" {
		t.Fatalf("unexpected resource picker metadata: %#v", view)
	}
	hidden, ok := view["hiddenData"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hiddenData, got %#v", view["hiddenData"])
	}
	base, ok := hidden[registeredToolAgentViewArgsField].(map[string]interface{})
	if !ok || base["draft"] != "kept" {
		t.Fatalf("expected hidden tool args, got %#v", hidden)
	}
	options, ok := view["options"].([]map[string]interface{})
	if !ok || len(options) != 2 || options[0]["value"] != "u1" || options[0]["status"] != "Finance" {
		t.Fatalf("expected resource options, got %#v", view["options"])
	}
}

func TestBuildRegisteredToolAgentViewUsesFieldMapperAnnotation(t *testing.T) {
	tool := RegisteredTool{
		Name: "import_expenses",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"mapping"},
			"properties": map[string]interface{}{
				"mapping": map[string]interface{}{
					"type":            "object",
					"x-agent-view":    "field_mapper",
					"x-source-fields": []interface{}{"金额", "日期", "备注"},
					"x-target-fields": []interface{}{
						map[string]interface{}{"name": "amount", "label": "Amount", "type": "number", "required": true},
						map[string]interface{}{"name": "expense_date", "label": "Expense date", "type": "date"},
					},
				},
			},
		},
	}

	view := buildRegisteredToolAgentView(tool, map[string]interface{}{"mapping": map[string]interface{}{"amount": "金额"}}, nil)
	if view["type"] != "field_mapper" {
		t.Fatalf("expected field_mapper view, got %#v", view)
	}
	if view["dataKey"] != "mapping" {
		t.Fatalf("expected mapping dataKey, got %#v", view["dataKey"])
	}
	sourceFields, ok := view["sourceFields"].([]string)
	if !ok || len(sourceFields) != 3 || sourceFields[0] != "金额" {
		t.Fatalf("expected source fields, got %#v", view["sourceFields"])
	}
	targetFields, ok := view["targetFields"].([]map[string]interface{})
	if !ok || len(targetFields) != 2 || targetFields[0]["name"] != "amount" || targetFields[0]["required"] != true {
		t.Fatalf("expected target fields, got %#v", view["targetFields"])
	}
	value, ok := view["value"].(map[string]interface{})
	if !ok || value["amount"] != "金额" {
		t.Fatalf("expected existing mapping value, got %#v", view["value"])
	}
}
