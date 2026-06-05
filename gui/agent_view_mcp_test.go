package main

import (
	"strings"
	"testing"

	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestBuildMCPToolAgentViewShowsValidationErrors(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"path"},
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "File path"},
			"mode": map[string]interface{}{"type": "string", "enum": []interface{}{"read", "write"}},
		},
	}
	view := buildMCPToolAgentView("local", "srv-1", "fs_read", schema, map[string]interface{}{"mode": "read"}, []mcputil.ValidationError{{
		Param:   "path",
		Code:    "missing_required",
		Message: "Required parameter 'path' (string) is missing",
	}})
	if view == nil {
		t.Fatal("expected MCP agent view")
	}
	if view["type"] != "form" || view["id"] != "mcp:call" {
		t.Fatalf("unexpected view: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected fields, got %#v", view["fields"])
	}
	var pathField map[string]interface{}
	var mcpHidden map[string]interface{}
	var schemaVersionHidden map[string]interface{}
	for _, field := range fields {
		switch field["name"] {
		case "path":
			pathField = field
		case mcpAgentViewCallArgsField:
			mcpHidden = field
		case agentViewSchemaVersionField:
			schemaVersionHidden = field
		}
	}
	if pathField == nil || pathField["error"] == "" || pathField["required"] != true {
		t.Fatalf("expected required path field with validation error, got %#v", pathField)
	}
	if mcpHidden == nil || mcpHidden["type"] != "hidden" {
		t.Fatalf("expected hidden MCP call context, got %#v", mcpHidden)
	}
	if schemaVersionHidden == nil || schemaVersionHidden["value"] == "" {
		t.Fatalf("expected hidden schema version, got %#v", schemaVersionHidden)
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta["schemaVersion"] == "" || meta["schemaSource"] != "mcp.adapter" {
		t.Fatalf("expected schema metadata, got %#v", meta)
	}
}

func TestBuildMCPToolAgentViewArraySchemaUsesTable(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"items"},
		"properties": map[string]interface{}{
			"items": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":  map[string]interface{}{"type": "string"},
						"amount": map[string]interface{}{"type": "number"},
					},
				},
			},
		},
	}
	view := buildMCPToolAgentView("srv", "srv", "submit_expenses", schema, map[string]interface{}{}, []mcputil.ValidationError{{
		Param:   "items",
		Code:    "missing_required",
		Message: "Required parameter 'items' (array) is missing",
	}})
	fields := view["fields"].([]map[string]interface{})
	var itemsField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "items" {
			itemsField = field
			break
		}
	}
	if itemsField == nil || itemsField["type"] != "array_table" {
		t.Fatalf("expected MCP array_table field, got %#v", itemsField)
	}
	if columns, ok := itemsField["columns"].([]map[string]interface{}); !ok || len(columns) != 2 {
		t.Fatalf("expected MCP table columns, got %#v", itemsField["columns"])
	}
}

func TestBuildMCPToolAgentViewArrayEnumUsesMultiselect(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"scopes"},
		"properties": map[string]interface{}{
			"scopes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
					"enum": []interface{}{"read", "write"},
				},
			},
		},
	}
	view := buildMCPToolAgentView("srv", "srv", "grant_access", schema, map[string]interface{}{}, []mcputil.ValidationError{{
		Param:   "scopes",
		Code:    "missing_required",
		Message: "Required parameter 'scopes' (array) is missing",
	}})
	fields := view["fields"].([]map[string]interface{})
	var scopesField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "scopes" {
			scopesField = field
			break
		}
	}
	if scopesField == nil || scopesField["type"] != "multiselect" {
		t.Fatalf("expected MCP multiselect field, got %#v", scopesField)
	}
}

func TestBuildMCPToolAgentViewObjectSchemaUsesObjectForm(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"metadata"},
		"properties": map[string]interface{}{
			"metadata": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]interface{}{"type": "string"},
					"dryRun": map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
	view := buildMCPToolAgentView("srv", "srv", "submit", schema, map[string]interface{}{}, []mcputil.ValidationError{{
		Param:   "metadata",
		Code:    "missing_required",
		Message: "Required parameter 'metadata' (object) is missing",
	}})
	fields := view["fields"].([]map[string]interface{})
	var metadataField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "metadata" {
			metadataField = field
			break
		}
	}
	if metadataField == nil || metadataField["type"] != "object_form" {
		t.Fatalf("expected MCP object_form field, got %#v", metadataField)
	}
}

func TestBuildMCPToolAgentViewUsesSpecializedResourcePicker(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"assignee"},
		"properties": map[string]interface{}{
			"assignee": map[string]interface{}{
				"type":            "string",
				"x-agent-view":    "resource_picker",
				"x-resource-type": "user",
				"resources": []interface{}{
					map[string]interface{}{"id": "u1", "label": "Alice"},
				},
			},
		},
	}
	view := buildMCPToolAgentView("srv", "srv-id", "assign", schema, map[string]interface{}{}, []mcputil.ValidationError{{
		Param:   "assignee",
		Code:    "missing_required",
		Message: "Required parameter 'assignee' is missing",
	}})
	if view["type"] != "resource_picker" || view["id"] != "mcp:call" || view["dataKey"] != "assignee" {
		t.Fatalf("expected MCP resource picker, got %#v", view)
	}
	hidden, ok := view["hiddenData"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hiddenData, got %#v", view["hiddenData"])
	}
	callArgs, ok := hidden[mcpAgentViewCallArgsField].(map[string]interface{})
	if !ok || callArgs["server_id"] != "srv" || callArgs["tool_name"] != "assign" {
		t.Fatalf("expected MCP call context, got %#v", hidden)
	}
}

func TestRegisteredToolValidationIssuesForMCPKeepsParam(t *testing.T) {
	issues := []registeredToolValidationIssue{
		{Path: "items.amount", Message: "items[1].amount must be at least 1"},
		{Path: "email", Message: "email must be a valid email"},
	}
	errs := registeredToolValidationIssuesForMCP(issues)
	if len(errs) != 2 {
		t.Fatalf("expected two MCP validation errors, got %#v", errs)
	}
	if errs[0].Param != "items" || errs[1].Param != "email" {
		t.Fatalf("expected top-level params, got %#v", errs)
	}
}

func TestHandleMCPToolAgentViewSubmitHonorsWorkflowPolicy(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "mcp-agent-view-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	resp := h.handleMCPToolAgentViewSubmit(map[string]interface{}{
		mcpAgentViewCallArgsField: map[string]interface{}{
			"server_id":                      "fs",
			"tool_name":                      "write_file",
			"arguments":                      map[string]interface{}{"path": "out.txt", "content": "x"},
			registeredToolPolicyOwnerIDField: userID,
		},
	})
	if resp == nil || !strings.Contains(resp.Text, "not allowed by the current workflow tool policy") {
		t.Fatalf("expected workflow policy rejection, got %#v", resp)
	}
}

func TestHandleMCPToolAgentViewSubmitRejectsDisabledExternalCodingSessionTarget(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})

	resp := h.handleMCPToolAgentViewSubmit(map[string]interface{}{
		mcpAgentViewCallArgsField: map[string]interface{}{
			"server_id": "legacy",
			"tool_name": " CREATE_SESSION ",
			"arguments": map[string]interface{}{},
		},
	})
	if resp == nil || !strings.Contains(resp.Text, "create_session is disabled") || !strings.Contains(resp.Error, "external coding-session MCP target disabled") {
		t.Fatalf("expected disabled MCP target rejection, got %#v", resp)
	}
}

func TestHandleMCPToolAgentViewSubmitWithoutOwnerDoesNotUseCurrentRuntime(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "mcp-agent-view-current-runtime-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	h.currentLoopCtx = &LoopContext{Runtime: RuntimeContext{RequestID: "req-current", PolicyOwnerID: userID}}

	resp := h.handleMCPToolAgentViewSubmit(map[string]interface{}{
		mcpAgentViewCallArgsField: map[string]interface{}{
			"server_id": "fs",
			"tool_name": "write_file",
			"arguments": map[string]interface{}{"path": "out.txt", "content": "x"},
		},
	})
	if resp == nil {
		t.Fatal("expected MCP submit response")
	}
	if strings.Contains(resp.Text, "not allowed by the current workflow tool policy") || strings.Contains(resp.Error, "not allowed by the current workflow tool policy") {
		t.Fatalf("MCP task panel submit without hidden owner should not inherit current runtime policy, got %#v", resp)
	}
}

func TestBuildMCPToolAgentViewCarriesRuntimePolicyOwner(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"required":   []interface{}{"path"},
		"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
	}
	view := buildMCPToolAgentViewWithPolicyOwner("srv", "srv", "read", schema, map[string]interface{}{}, []mcputil.ValidationError{{
		Param: "path", Code: "missing_required", Message: "missing path",
	}}, "remote:mobile")
	fields := view["fields"].([]map[string]interface{})
	for _, field := range fields {
		if field["name"] != mcpAgentViewCallArgsField {
			continue
		}
		callArgs, _ := field["value"].(map[string]interface{})
		if callArgs[registeredToolPolicyOwnerIDField] != "remote:mobile" {
			t.Fatalf("hidden MCP call owner = %#v", callArgs[registeredToolPolicyOwnerIDField])
		}
		return
	}
	t.Fatal("missing MCP call hidden field")
}
