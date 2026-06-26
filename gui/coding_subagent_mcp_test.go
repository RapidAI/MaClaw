package main

import (
	"strings"
	"testing"
)

func TestSelectRelevantMCPToolsForTask_NilHandler(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: nil},
	}
	result := cb.selectRelevantMCPToolsForTask("测试登录页面")
	if result != nil {
		t.Errorf("expected nil with nil handler, got %v", result)
	}
}

func TestSelectRelevantMCPToolsForTask_EmptyDescription(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
	}
	result := cb.selectRelevantMCPToolsForTask("")
	if result != nil {
		t.Errorf("expected nil with empty description, got %v", result)
	}
}

func TestBuildCodingSubAgentMCPSection_Empty(t *testing.T) {
	section := buildCodingSubAgentMCPSection(nil)
	if section != "" {
		t.Errorf("expected empty section for nil tools, got %q", section)
	}
}

func TestBuildCodingSubAgentMCPSection_WithTools(t *testing.T) {
	tools := []codingSubAgentMCPToolMatch{
		{ServerID: "playwright-1", ServerName: "playwright", ToolName: "navigate", Description: "Navigate to a URL", RequiredArgs: []string{"url"}},
		{ServerID: "playwright-1", ServerName: "playwright", ToolName: "screenshot", Description: "Take a screenshot"},
	}
	section := buildCodingSubAgentMCPSection(tools)

	if !strings.Contains(section, "navigate") {
		t.Error("section should contain tool name")
	}
	if !strings.Contains(section, "screenshot") {
		t.Error("section should contain second tool name")
	}
	if !strings.Contains(section, "playwright") {
		t.Error("section should contain server name")
	}
	if !strings.Contains(section, "call_mcp_tool") {
		t.Error("section should mention call_mcp_tool usage")
	}
	if !strings.Contains(section, "必需参数: url") {
		t.Error("section should show required args for navigate")
	}
}

func TestBuildCodingSubAgentMCPSectionCapsRequiredArgs(t *testing.T) {
	section := buildCodingSubAgentMCPSection([]codingSubAgentMCPToolMatch{{
		ServerID: "browser-1", ServerName: "browser", ToolName: "screenshot", Description: "capture screen",
		RequiredArgs: []string{"url", "selector", "viewport", "wait_until", "timeout", "format", "quality", "clip"},
	}})

	if !strings.Contains(section, "url, selector, viewport, wait_until, timeout, format") {
		t.Fatalf("section should include first required args, got %q", section)
	}
	if strings.Contains(section, "quality") || strings.Contains(section, "clip") {
		t.Fatalf("section should cap expanded required args, got %q", section)
	}
	if !strings.Contains(section, "还有 2 项未展开") {
		t.Fatalf("section should report omitted required args, got %q", section)
	}
}

func TestBuildCallMCPToolDefinition_Structure(t *testing.T) {
	def := buildCallMCPToolDefinition()

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("expected function field")
	}
	name, _ := fn["name"].(string)
	if name != "call_mcp_tool" {
		t.Errorf("expected name=call_mcp_tool, got %q", name)
	}

	params, ok := fn["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters field")
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required field")
	}
	if len(required) != 2 || required[0] != "server_id" || required[1] != "tool_name" {
		t.Errorf("expected required=[server_id, tool_name], got %v", required)
	}
}

func TestExecuteCallMCPTool_NoMatchedTools(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent:        &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedMCPTools: nil,
	}
	result := cb.executeCallMCPTool(map[string]interface{}{"server_id": "playwright", "tool_name": "navigate"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Errorf("expected blocked outcome, got %v", result.Outcome)
	}
}

func TestExecuteCallMCPTool_UnmatchedTool(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedMCPTools: []codingSubAgentMCPToolMatch{
			{ServerID: "playwright-1", ServerName: "playwright", ToolName: "navigate"},
		},
	}
	result := cb.executeCallMCPTool(map[string]interface{}{"server_id": "playwright", "tool_name": "click"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Errorf("expected blocked for unmatched tool, got %v", result.Outcome)
	}
	if !strings.Contains(result.Text, "not available") {
		t.Errorf("expected not available message, got %q", result.Text)
	}
}

func TestIsMatchedMCPTool_CaseInsensitive(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		matchedMCPTools: []codingSubAgentMCPToolMatch{
			{ServerID: "Playwright-1", ServerName: "Playwright", ToolName: "Navigate"},
		},
	}
	// Match by server name (case insensitive)
	if !cb.isMatchedMCPTool("playwright", "navigate") {
		t.Error("isMatchedMCPTool should be case-insensitive")
	}
	// Match by server ID (case insensitive)
	if !cb.isMatchedMCPTool("playwright-1", "navigate") {
		t.Error("isMatchedMCPTool should match by server ID")
	}
	// Non-matching tool
	if cb.isMatchedMCPTool("playwright", "click") {
		t.Error("isMatchedMCPTool should return false for non-matched tool")
	}
}

func TestExecuteCallMCPTool_MissingParams(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedMCPTools: []codingSubAgentMCPToolMatch{
			{ServerID: "pw", ServerName: "playwright", ToolName: "navigate"},
		},
	}
	result := cb.executeCallMCPTool(map[string]interface{}{"server_id": "pw"})
	if result.Outcome != codingToolOutcomeFailed {
		t.Errorf("expected failed for missing tool_name, got %v", result.Outcome)
	}
}

func TestMCPRequiredArgsAllowTopLevelCompatibility(t *testing.T) {
	tool := codingSubAgentMCPToolMatch{ServerName: "Wiki", ToolName: "get_page_children", RequiredArgs: []string{"parent_id", "limit"}}
	args := map[string]interface{}{
		"server_id":  "wiki",
		"tool_name":  "get_page_children",
		"parent_id":  "root",
		"limit":      float64(25),
		"arguments":  map[string]interface{}{"limit": float64(10)},
		"irrelevant": "left alone",
	}

	if result, rejected := rejectMissingCodingSubAgentMCPRequiredArguments(tool, args); rejected {
		t.Fatalf("top-level required MCP args should be normalized instead of rejected, got %#v", result)
	}
	arguments, ok := args["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("arguments should be a JSON object after normalization, got %#v", args["arguments"])
	}
	if arguments["parent_id"] != "root" {
		t.Fatalf("top-level parent_id should be copied into arguments, got %#v", arguments)
	}
	if arguments["limit"] != float64(10) {
		t.Fatalf("existing arguments.limit should not be overwritten by top-level value, got %#v", arguments)
	}
}

func TestMCPRequiredArgsCreateArgumentsFromTopLevelCompatibility(t *testing.T) {
	tool := codingSubAgentMCPToolMatch{ServerName: "Browser", ToolName: "navigate", RequiredArgs: []string{"url"}}
	args := map[string]interface{}{"server_id": "browser", "tool_name": "navigate", "url": "https://example.test"}

	if result, rejected := rejectMissingCodingSubAgentMCPRequiredArguments(tool, args); rejected {
		t.Fatalf("top-level MCP arg should create arguments object, got %#v", result)
	}
	arguments, ok := args["arguments"].(map[string]interface{})
	if !ok || arguments["url"] != "https://example.test" {
		t.Fatalf("expected arguments.url to be normalized from top-level url, got %#v", args["arguments"])
	}
}
func TestExtractMCPToolRequiredArgs(t *testing.T) {
	// Normal case: []interface{} of strings
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"url", "timeout"},
	}
	args := extractMCPToolRequiredArgs(schema)
	if len(args) != 2 || args[0] != "url" || args[1] != "timeout" {
		t.Errorf("expected [url timeout], got %v", args)
	}

	// Nil schema
	if extractMCPToolRequiredArgs(nil) != nil {
		t.Error("expected nil for nil schema")
	}

	// No required field
	if extractMCPToolRequiredArgs(map[string]interface{}{"type": "object"}) != nil {
		t.Error("expected nil for missing required field")
	}

	// Empty required array
	if extractMCPToolRequiredArgs(map[string]interface{}{"required": []interface{}{}}) != nil {
		t.Error("expected nil for empty required array")
	}
}
