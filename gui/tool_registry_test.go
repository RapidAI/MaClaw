package main

import (
	"strings"
	"sync"
	"testing"
)

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(RegisteredTool{
		Name: "test_tool", Description: "a test tool",
		Category: ToolCategoryBuiltin, Status: RegToolAvailable,
		Handler: func(args map[string]interface{}) string { return "ok" },
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	tool, ok := r.Get("test_tool")
	if !ok {
		t.Fatal("Get returned false")
	}
	if tool.Name != "test_tool" {
		t.Errorf("Name = %q, want test_tool", tool.Name)
	}
	if tool.Handler == nil {
		t.Error("Handler is nil")
	}
}

func TestToolRegistry_RegisterEmptyName(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(RegisteredTool{Name: ""})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestToolRegistry_RegisterRejectsExternalCodingSessionTools(t *testing.T) {
	r := NewToolRegistry()
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		if err := r.Register(RegisteredTool{Name: name, Status: RegToolAvailable}); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
		if _, ok := r.Get(name); ok {
			t.Fatalf("%s should not be registered", name)
		}
	}
}

func TestToolRegistry_Unregister(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x", Category: ToolCategoryBuiltin})
	r.Unregister("x")
	if _, ok := r.Get("x"); ok {
		t.Error("tool should be unregistered")
	}
}

func TestToolRegistry_ListAvailable(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Status: RegToolAvailable})
	r.Register(RegisteredTool{Name: "b", Status: RegToolUnavailable})
	r.Register(RegisteredTool{Name: "c", Status: RegToolDegraded})

	avail := r.ListAvailable()
	if len(avail) != 1 {
		t.Errorf("ListAvailable len = %d, want 1", len(avail))
	}
	if avail[0].Name != "a" {
		t.Errorf("ListAvailable[0].Name = %q, want a", avail[0].Name)
	}
}

func TestToolRegistry_ListByCategory(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Category: ToolCategoryBuiltin})
	r.Register(RegisteredTool{Name: "b", Category: ToolCategoryMCP})
	r.Register(RegisteredTool{Name: "c", Category: ToolCategoryBuiltin})

	builtins := r.ListByCategory(ToolCategoryBuiltin)
	if len(builtins) != 2 {
		t.Errorf("ListByCategory(builtin) len = %d, want 2", len(builtins))
	}
}

func TestToolRegistry_ListByTags(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Tags: []string{"git", "vcs"}})
	r.Register(RegisteredTool{Name: "b", Tags: []string{"file", "read"}})
	r.Register(RegisteredTool{Name: "c", Tags: []string{"git", "commit"}})

	gitTools := r.ListByTags([]string{"git"})
	if len(gitTools) != 2 {
		t.Errorf("ListByTags(git) len = %d, want 2", len(gitTools))
	}
}

func TestToolRegistry_UpdateStatus(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x", Status: RegToolAvailable})
	r.UpdateStatus("x", RegToolUnavailable)
	tool, _ := r.Get("x")
	if tool.Status != RegToolUnavailable {
		t.Errorf("Status = %q, want unavailable", tool.Status)
	}
}

func TestToolRegistry_OnChange(t *testing.T) {
	r := NewToolRegistry()
	called := 0
	r.OnChange(func() { called++ })
	r.Register(RegisteredTool{Name: "x"})
	if called != 1 {
		t.Errorf("OnChange called %d times, want 1", called)
	}
	r.Unregister("x")
	if called != 2 {
		t.Errorf("OnChange called %d times after unregister, want 2", called)
	}
}

func TestToolRegistry_DefaultStatus(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x"}) // no status set
	tool, _ := r.Get("x")
	if tool.Status != RegToolAvailable {
		t.Errorf("default Status = %q, want available", tool.Status)
	}
}

func TestRegisterBuiltinToolsIncludesMISDataWorkspace(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})

	tool, ok := r.Get("mis_data")
	if !ok {
		t.Fatal("mis_data tool is not registered")
	}
	if tool.Handler == nil {
		t.Fatal("mis_data handler is nil")
	}
	action, ok := tool.InputSchema["action"].(map[string]string)
	if !ok {
		t.Fatalf("mis_data action schema has unexpected type: %#v", tool.InputSchema["action"])
	}
	if !strings.Contains(action["description"], "list_agent_transactions") || !strings.Contains(action["description"], "resolve_object_role") {
		t.Fatalf("action schema should mention workspace and object-role actions, got %q", action["description"])
	}
	for _, prop := range []string{"dataset_id", "object_role", "app_id", "blueprint_id", "require_initialized"} {
		if _, ok := tool.InputSchema[prop]; !ok {
			t.Fatalf("mis_data registry schema missing %s", prop)
		}
	}
}

func TestRegisterBuiltinToolsOmitsExternalCodingSessionTools(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})

	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		if _, ok := r.Get(name); ok {
			t.Fatalf("%s should not be registered", name)
		}
	}
}

func TestRegisterBuiltinToolsExposeWorkflowDocMetadata(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})

	for _, toolName := range []string{"write_file", "send_file", "send_to_im", "office", "generate_pdf"} {
		tool, ok := r.Get(toolName)
		if !ok {
			t.Fatalf("%s tool is not registered", toolName)
		}
		for _, prop := range []string{"phase_id", "doc_type"} {
			if _, ok := tool.InputSchema[prop]; !ok {
				t.Fatalf("%s should expose %s metadata for workflow document filenames", toolName, prop)
			}
		}
	}
}

func TestRegisterBuiltinToolsManageSkillMaintenancePlanSchema(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})

	tool, ok := r.Get("manage_skill")
	if !ok {
		t.Fatal("manage_skill tool is not registered")
	}
	action, ok := tool.InputSchema["action"].(map[string]string)
	if !ok || !strings.Contains(action["description"], "maintenance_plan") {
		t.Fatalf("manage_skill action schema should mention maintenance_plan: %#v", tool.InputSchema["action"])
	}
	for _, prop := range []string{"max_actions", "stale_after_days", "min_failure_runs", "duplicate_similarity", "dry_run", "confirm", "approved_actions", "allow_duplicate_retire"} {
		if _, ok := tool.InputSchema[prop]; !ok {
			t.Fatalf("manage_skill schema missing maintenance property %q", prop)
		}
	}
}

func TestRegisterBuiltinToolsAttachLightExecutionContracts(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})
	for _, name := range []string{"manage_skill", "web_search", "web_fetch", "async_wait", "call_mcp_tool", "current_datetime"} {
		tool, ok := r.Get(name)
		if !ok {
			t.Fatalf("%s tool is not registered", name)
		}
		if len(tool.ExecutionContract) == 0 {
			t.Fatalf("%s missing execution contract", name)
		}
		contract := executionContractFromMetadata(name, tool.ExecutionContract)
		if !contract.Explicit || contract.RequiresAgentPlanning {
			t.Fatalf("%s contract = %+v, want explicit non-planning", name, contract)
		}
		if name == "current_datetime" && (!contract.SupportsDirect || !contract.Deterministic) {
			t.Fatalf("%s contract = %+v, want direct deterministic", name, contract)
		}
	}
}

func TestRegisterBuiltinToolsWorkflowDocMetadataDescriptions(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})

	assertRegistrySchemaDescription(t, r, "write_file", "phase_id", workflowDocPhaseIDSchemaDescription())
	assertRegistrySchemaDescription(t, r, "write_file", "doc_type", workflowDocTypeSchemaDescription())
	assertRegistrySchemaDescription(t, r, "send_file", "phase_id", workflowDocDeliveryPhaseIDSchemaDescription())
	assertRegistrySchemaDescription(t, r, "send_file", "doc_type", workflowDocDeliveryTypeSchemaDescription())
	assertRegistrySchemaDescription(t, r, "send_to_im", "phase_id", workflowDocDeliveryPhaseIDSchemaDescription())
	assertRegistrySchemaDescription(t, r, "send_to_im", "doc_type", workflowDocDeliveryTypeSchemaDescription())
	assertRegistrySchemaDescription(t, r, "office", "phase_id", workflowDocGeneratePDFPhaseIDSchemaDescription())
	assertRegistrySchemaDescription(t, r, "generate_pdf", "phase_id", workflowDocGeneratePDFPhaseIDSchemaDescription())
}

func assertRegistrySchemaDescription(t *testing.T, r *ToolRegistry, toolName, propName, want string) {
	t.Helper()
	tool, ok := r.Get(toolName)
	if !ok {
		t.Fatalf("%s tool is not registered", toolName)
	}
	got := registrySchemaDescription(t, tool.InputSchema, propName)
	if got != want {
		t.Fatalf("%s %s description = %q, want %q", toolName, propName, got, want)
	}
}

func registrySchemaDescription(t *testing.T, schema map[string]interface{}, propName string) string {
	t.Helper()
	raw, ok := schema[propName]
	if !ok {
		t.Fatalf("schema property %s missing", propName)
	}
	switch prop := raw.(type) {
	case map[string]string:
		return prop["description"]
	case map[string]interface{}:
		desc, _ := prop["description"].(string)
		return desc
	default:
		t.Fatalf("schema property %s has unexpected type: %#v", propName, raw)
		return ""
	}
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	r := NewToolRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "tool_" + string(rune('a'+n%26))
			r.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin})
			r.Get(name)
			r.ListAvailable()
			r.ListByCategory(ToolCategoryBuiltin)
		}(i)
	}
	wg.Wait()
}

func TestToolRegistry_ListAvailable_DeterministicOrder(t *testing.T) {
	r := NewToolRegistry()
	// Register in reverse alphabetical order to verify sort is applied.
	r.Register(RegisteredTool{Name: "zeta", Status: RegToolAvailable})
	r.Register(RegisteredTool{Name: "alpha", Status: RegToolAvailable})
	r.Register(RegisteredTool{Name: "middle", Status: RegToolAvailable})

	avail := r.ListAvailable()
	if len(avail) != 3 {
		t.Fatalf("ListAvailable len = %d, want 3", len(avail))
	}
	// Must be alphabetically sorted for LLM API prefix cache stability.
	if avail[0].Name != "alpha" || avail[1].Name != "middle" || avail[2].Name != "zeta" {
		t.Errorf("ListAvailable order = [%s, %s, %s], want [alpha, middle, zeta]",
			avail[0].Name, avail[1].Name, avail[2].Name)
	}

	// Verify idempotency — multiple calls produce identical order.
	avail2 := r.ListAvailable()
	for i := range avail {
		if avail[i].Name != avail2[i].Name {
			t.Errorf("ListAvailable not idempotent: call1[%d]=%s, call2[%d]=%s",
				i, avail[i].Name, i, avail2[i].Name)
		}
	}
}
