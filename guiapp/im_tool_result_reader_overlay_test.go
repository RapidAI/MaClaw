package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func spilledHandleHistory() []agent.ConversationEntry {
	return []agent.ConversationEntry{{
		Role:    "tool",
		Content: "preview\n\n" + toolresult.HandleFooterMarker + "\nid: 20260927T033427_ssh_abc\ntool: ssh\n",
	}}
}

func TestSpilledToolResultHandleOverlaysReaderOnSemanticSurface(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "read_tool_result",
		Description: "read spilled tool output",
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"id"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{
			grants:   map[string]tool.InvocationGrant{},
			registry: tool.NewCapabilityRegistry("test"),
		},
		checkpointHistory: spilledHandleHistory(),
	}
	tools := cb.BuildToolsForModelRequest("查询api2服务器状态", 3)
	found := false
	for _, def := range tools {
		if extractToolName(def) != "read_tool_result" {
			continue
		}
		found = true
		fn, _ := def["function"].(map[string]interface{})
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		if _, ok := props["id"]; !ok {
			t.Fatalf("overlaid reader schema missing id: %#v", params)
		}
	}
	if !found {
		t.Fatalf("spilled handle must list read_tool_result, got %v", toolNamesForTest(tools))
	}
	if !cb.IsToolAllowed("read_tool_result") {
		t.Fatal("overlaid reader must be executable on this turn")
	}
}

func TestSpilledToolResultHandleDoesNotOverlayWithoutMarker(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "read_tool_result", Description: "reader", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{
			grants:   map[string]tool.InvocationGrant{},
			registry: tool.NewCapabilityRegistry("test"),
		},
		checkpointHistory: []agent.ConversationEntry{{Role: "tool", Content: "ssh ok\nuptime 18 min"}},
	}
	tools := cb.BuildToolsForModelRequest("查询", 1)
	for _, def := range tools {
		if extractToolName(def) == "read_tool_result" {
			t.Fatal("reader must stay off the surface until a handle is spilled")
		}
	}
}

func TestSpilledToolResultReaderPetitionRetriesInsteadOfHardDenial(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "read_tool_result",
		Description: "read spilled tool output",
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{"type": "object"},
	}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{
			grants:   map[string]tool.InvocationGrant{},
			registry: tool.NewCapabilityRegistry("test"),
		},
		checkpointHistory: spilledHandleHistory(),
	}
	granted, message := cb.PetitionToolCall("read_tool_result")
	if !granted {
		t.Fatal("spilled handle must grant read_tool_result")
	}
	if strings.Contains(message, "was not available") || strings.Contains(message, "Do not retry") {
		t.Fatalf("petition must ask for a retry, got %q", message)
	}
	if !cb.IsToolAllowed("read_tool_result") {
		t.Fatal("granted reader must be executable on the next request")
	}
	tools := cb.BuildToolsForModelRequest("查询api2服务器状态", 4)
	if !strings.Contains(toolNamesForTest(tools), "read_tool_result") {
		t.Fatalf("next request missing reader: %s", toolNamesForTest(tools))
	}
}

func TestSpilledToolResultHandleStaysOffGroupSurface(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "read_tool_result", Description: "reader", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{
			grants:   map[string]tool.InvocationGrant{},
			registry: tool.NewCapabilityRegistry("test"),
		},
		loopCtx:           &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}},
		checkpointHistory: spilledHandleHistory(),
	}
	cb.maybeOverlaySpilledToolResultReader()
	if cb.legacyPetitionAllows("read_tool_result") {
		t.Fatal("group policy must not gain read_tool_result from a spilled handle")
	}
}

func toolNamesForTest(tools []map[string]interface{}) string {
	names := make([]string, 0, len(tools))
	for _, def := range tools {
		names = append(names, extractToolName(def))
	}
	return strings.Join(names, ",")
}
