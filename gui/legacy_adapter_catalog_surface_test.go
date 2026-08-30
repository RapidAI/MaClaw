package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func legacyReviewedDefinition(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": name,
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
			},
		},
	}
}

func TestRenderReviewedLegacySurfaceReplacesWithPlanRenderer(t *testing.T) {
	definitions := []map[string]interface{}{
		legacyReviewedDefinition("read_file"),
		legacyReviewedDefinition("async_wait"),
	}
	rendered, planBacked, err := renderReviewedLegacySurface("read a local file", definitions, nil)
	if err != nil || !planBacked {
		t.Fatalf("renderReviewedLegacySurface() planBacked=%v err=%v", planBacked, err)
	}
	if len(rendered) != 2 || extractToolName(rendered[0]) != "async_wait" || extractToolName(rendered[1]) != "read_file" {
		t.Fatalf("renderer did not produce stable replacement surface: %#v", rendered)
	}
	rendered[0]["function"].(map[string]interface{})["description"] = "mutated"
	if definitions[1]["function"].(map[string]interface{})["description"] == "mutated" {
		t.Fatal("renderer leaked mutable source definitions")
	}
}

func TestRenderReviewedLegacySurfaceLeavesDynamicDefinitionInCompatibilityPath(t *testing.T) {
	definitions := []map[string]interface{}{
		legacyReviewedDefinition("read_file"),
		legacyReviewedDefinition("dynamic_client_tool"),
	}
	rendered, planBacked, err := renderReviewedLegacySurface("test", definitions, nil)
	if err != nil || planBacked || len(rendered) != len(definitions) {
		t.Fatalf("dynamic definition must not receive fabricated provision: planBacked=%v rendered=%d err=%v", planBacked, len(rendered), err)
	}
	missing := legacyDefinitionsWithoutLiveProvisions(definitions)
	if len(missing) != 1 || !strings.Contains(missing[0], "dynamic_client_tool") {
		t.Fatalf("unexpected missing provisions: %v", missing)
	}
}

func TestClosedLegacyReplacementRejectsUnprovisionedHostDefinition(t *testing.T) {
	host := []map[string]interface{}{
		legacyReviewedDefinition("read_file"),
		legacyReviewedDefinition("unreviewed_host_tool"),
	}
	rendered, clientNames, planBacked, err := (&IMMessageHandler{}).renderClosedLegacyReplacementSurface("read file", nil, host, nil)
	if err == nil || planBacked || len(rendered) != 0 || len(clientNames) != 0 {
		t.Fatalf("unprovisioned host definition must close the replacement surface: rendered=%#v clients=%v planBacked=%v err=%v", rendered, clientNames, planBacked, err)
	}
	if !strings.Contains(err.Error(), "catalog_incomplete") {
		t.Fatalf("replacement error = %v, want catalog_incomplete", err)
	}
}

func TestClosedLegacyReplacementRebuildsClientBindingAfterHostPlan(t *testing.T) {
	ctx := NewLoopContext("client-replacement", 3, nil)
	ctx.ClientToolContext = &agent.ClientToolContext{ClientID: "device-1", ConversationID: "conversation-1"}
	ctx.ClientTools = []agent.ClientToolDefinition{{
		Name: "alarm_list", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"scope": map[string]any{"type": "string"}}},
	}}
	// The stale client definition simulates a previous request surface. It must
	// be removed before host plan rendering, then recreated only from this
	// request's ClientToolContext.
	staleSurface := []map[string]interface{}{
		legacyReviewedDefinition("read_file"),
		legacyReviewedDefinition("alarm_list"),
	}
	rendered, clientNames, planBacked, err := (&IMMessageHandler{}).renderClosedLegacyReplacementSurface("read file", ctx, staleSurface, nil)
	if err != nil || !planBacked {
		t.Fatalf("closed replacement = (%#v, %v, %v), want plan-backed host surface", rendered, planBacked, err)
	}
	if len(clientNames) != 1 || clientNames[0] != "alarm_list" {
		t.Fatalf("client binding = %v, want request-local alarm_list", clientNames)
	}
	names := toolNameSetForWorkflowFilterTest(rendered)
	if !names["read_file"] || !names["alarm_list"] || len(names) != 2 {
		t.Fatalf("replacement definitions = %v", names)
	}
	for _, definition := range rendered {
		if extractToolName(definition) != "alarm_list" {
			continue
		}
		properties := definition["function"].(map[string]interface{})["parameters"].(map[string]any)["properties"].(map[string]any)
		if _, ok := properties["scope"]; !ok {
			t.Fatalf("client definition was not rebuilt from current client contract: %#v", definition)
		}
	}
}

func TestClientToolDoesNotDemoteReviewedHostSurfaceOrBypassItsProvision(t *testing.T) {
	ctx := NewLoopContext("client-plan-surface", 3, nil)
	ctx.ClientToolContext = &agent.ClientToolContext{ClientID: "device-1", ConversationID: "conversation-1"}
	ctx.ClientTools = []agent.ClientToolDefinition{{
		Name: "alarm_list", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"scope": map[string]any{"type": "string"}}},
	}}
	host := []map[string]interface{}{legacyReviewedDefinition("read_file")}
	renderedHost, planBacked, err := renderReviewedLegacySurface("read file", host, nil)
	if err != nil || !planBacked {
		t.Fatalf("reviewed host surface = (%v, %v), want plan-backed", planBacked, err)
	}
	tools := append(renderedHost, clientToolDefinitionsForAgent(ctx, renderedHost)...)
	surface := newLegacyToolSurfaceWithClientTools(tools, []string{"alarm_list"})
	if !surface.AllowsLiveProvision("read_file") {
		t.Fatal("reviewed host definition lost its live-provision admission")
	}
	if !surface.AllowsLiveProvision("alarm_list") {
		t.Fatal("request-scoped client definition was treated as a missing static provision")
	}
	if err := surface.AllowsArguments("alarm_list", `{"scope":"all","hidden":"no"}`); err == nil {
		t.Fatal("client definition bypassed request-local parameter contract")
	}

	var dispatched bool
	handler := &IMMessageHandler{clientToolDispatcher: func(_ context.Context, target agent.ClientToolContext, definition agent.ClientToolDefinition, callID string, args map[string]any) error {
		dispatched = target.ClientID == "device-1" && definition.Name == "alarm_list" && callID == "call-1" && args["scope"] == "all"
		return nil
	}}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context: ctx, LegacySurface: surface,
		ToolCall: llm.ToolCall{ID: "call-1", Function: llm.ToolCallFunction{Name: "alarm_list", Arguments: `{"scope":"all"}`}},
	})
	if !dispatched || result.Outcome != toolOutcomeSucceeded {
		t.Fatalf("client tool did not dispatch through the request surface: dispatched=%v result=%+v", dispatched, result)
	}
}

func TestClientToolCannotOverrideExposedHostToolName(t *testing.T) {
	ctx := NewLoopContext("client-host-name-collision", 3, nil)
	ctx.ClientToolContext = &agent.ClientToolContext{ClientID: "device-1", ConversationID: "conversation-1"}
	ctx.ClientTools = []agent.ClientToolDefinition{{
		Name: "read_file", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}}
	host := []map[string]interface{}{legacyReviewedDefinition("read_file")}
	clientDefinitions := clientToolDefinitionsForAgent(ctx, host)
	if len(clientDefinitions) != 0 {
		t.Fatalf("colliding client definition must not be exposed: %#v", clientDefinitions)
	}

	calledClient := false
	handler := &IMMessageHandler{clientToolDispatcher: func(context.Context, agent.ClientToolContext, agent.ClientToolDefinition, string, map[string]any) error {
		calledClient = true
		return nil
	}}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context: ctx, LegacySurface: newLegacyToolSurfaceWithClientTools(host, nil),
		ToolCall: llm.ToolCall{ID: "call-host", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"README.md"}`}},
	})
	if calledClient {
		t.Fatal("hidden colliding client declaration hijacked host dispatch")
	}
	if strings.Contains(result.Text, "client tool") {
		t.Fatalf("host tool was incorrectly treated as a client tool: %+v", result)
	}
}

func TestLegacyModelMCPGatewayIsRejectedBeforeHandlerDispatch(t *testing.T) {
	registry := NewToolRegistry()
	var called bool
	if err := registry.Register(RegisteredTool{
		Name: "call_mcp_tool", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			called = true
			return "unexpected MCP dispatch"
		},
	}); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	surface := newLegacyToolSurface([]map[string]interface{}{toolDef("call_mcp_tool", "legacy gateway", map[string]interface{}{
		"server_id": map[string]interface{}{"type": "string"},
		"tool_name": map[string]interface{}{"type": "string"},
		"arguments": map[string]interface{}{"type": "object"},
	}, []string{"server_id", "tool_name"})})
	result := (&IMMessageHandler{registry: registry}).executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		LegacySurface: surface,
		ToolCall:      llm.ToolCall{Function: llm.ToolCallFunction{Name: "call_mcp_tool", Arguments: `{"server_id":"unbound","tool_name":"execute","arguments":{}}`}},
	})
	if called {
		t.Fatal("legacy model MCP gateway reached its handler")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "dynamic_mcp_requires_managed_surface") {
		t.Fatalf("unexpected gateway result: %+v", result)
	}
}

func TestLegacyModelManageSkillGatewayRejectsDynamicActionBeforeHandlerDispatch(t *testing.T) {
	registry := NewToolRegistry()
	var called bool
	if err := registry.Register(RegisteredTool{
		Name: "manage_skill", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			called = true
			return "unexpected skill dispatch"
		},
	}); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	surface := newLegacyToolSurface([]map[string]interface{}{toolDef("manage_skill", "legacy skill gateway", map[string]interface{}{
		"action": map[string]interface{}{"type": "string"},
		"name":   map[string]interface{}{"type": "string"},
		"args":   map[string]interface{}{"type": "object"},
	}, []string{"action"})})
	result := (&IMMessageHandler{registry: registry}).executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		LegacySurface: surface,
		ToolCall:      llm.ToolCall{Function: llm.ToolCallFunction{Name: "manage_skill", Arguments: `{"action":"run","name":"unbound-skill","args":{}}`}},
	})
	if called {
		t.Fatal("legacy model manage_skill gateway reached its handler")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "dynamic_skill_requires_managed_surface") {
		t.Fatalf("unexpected gateway result: %+v", result)
	}
}

func TestUnionMissFloorToolsForSurface(t *testing.T) {
	base := []map[string]interface{}{
		legacyReviewedDefinition("bash"),
		legacyReviewedDefinition("write_file"),
		legacyReviewedDefinition("edit_file"),
	}
	// Missing floor tools are appended from the base surface.
	got := unionMissFloorToolsForSurface([]map[string]interface{}{legacyReviewedDefinition("knowledge_search")}, base)
	names := agentLoopToolNamesForLog(got)
	if len(names) != 3 || names[0] != "knowledge_search" || names[1] != "bash" || names[2] != "write_file" {
		t.Fatalf("union = %v, want knowledge_search + bash + write_file", names)
	}
	// Idempotent: existing floor tools are not duplicated, and non-floor base
	// tools (edit_file) are never pulled in.
	got = unionMissFloorToolsForSurface(got, base)
	if names = agentLoopToolNamesForLog(got); len(names) != 3 {
		t.Fatalf("union not idempotent: %v", names)
	}
}
