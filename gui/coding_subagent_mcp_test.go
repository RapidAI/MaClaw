package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestSelectRelevantMCPToolsForTask_NilCallbacks(t *testing.T) {
	var cb *codingSubAgentCallbacks
	if result := cb.selectRelevantMCPToolsForTask("test"); result != nil {
		t.Fatalf("nil callback selection = %#v, want nil", result)
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

func TestExtractMCPToolRequiredArgumentHints(t *testing.T) {
	hints := extractMCPToolRequiredArgumentHints(map[string]interface{}{
		"required": []interface{}{"userMail", "limit", "missing"},
		"properties": map[string]interface{}{
			"userMail": map[string]interface{}{"type": "string", "description": "The user's corporate email address."},
			"limit":    map[string]interface{}{"type": "integer"},
		},
	})
	want := []string{"userMail (string): The user's corporate email address.", "limit (integer)", "missing"}
	if strings.Join(hints, "|") != strings.Join(want, "|") {
		t.Fatalf("argument hints = %#v, want %#v", hints, want)
	}
}

func TestExtractMCPToolRequiredArgsNormalizesDuplicateAndBlankValues(t *testing.T) {
	for _, schema := range []map[string]interface{}{
		{"required": []interface{}{" userMail ", "", "usermail", "limit", " LIMIT "}},
		{"required": []string{" userMail ", "", "usermail", "limit", " LIMIT "}},
	} {
		got := extractMCPToolRequiredArgs(schema)
		want := []string{"userMail", "limit"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("required args = %#v, want %#v", got, want)
		}
	}
}

func TestBuildCodingSubAgentMCPSectionShowsRequiredArgumentHints(t *testing.T) {
	section := buildCodingSubAgentMCPSection([]codingSubAgentMCPToolMatch{{
		ServerID: "tengyun", ServerName: "Tengyun", ToolName: "work_log_query",
		RequiredArgs:  []string{"userMail"},
		ArgumentHints: []string{"userMail (string): The user's corporate email address."},
	}})
	if !strings.Contains(section, "userMail (string): The user's corporate email address.") {
		t.Fatalf("MCP prompt should include required argument guidance, got %q", section)
	}
}

func TestBuildCodingSubAgentMCPSectionCapsRequiredArgumentHints(t *testing.T) {
	hints := make([]string, codingSubAgentDynamicRequiredArgsMax+2)
	for i := range hints {
		hints[i] = "field_" + strconv.Itoa(i) + " (string): description"
	}
	section := buildCodingSubAgentMCPSection([]codingSubAgentMCPToolMatch{{
		ServerID: "test", ServerName: "Test", ToolName: "many_args",
		RequiredArgs:  []string{"placeholder"},
		ArgumentHints: hints,
	}})
	if !strings.Contains(section, "field_5") || strings.Contains(section, "field_6") || strings.Contains(section, "field_7") {
		t.Fatalf("MCP prompt should cap expanded hints, got %q", section)
	}
	if !strings.Contains(section, "还有 2 项未展开") {
		t.Fatalf("MCP prompt should report omitted hints, got %q", section)
	}
}

func TestCodingSubAgentMCPToolReferencesUseServerIDsAndCapOutput(t *testing.T) {
	tools := make([]codingSubAgentMCPToolMatch, 13)
	for i := range tools {
		tools[i] = codingSubAgentMCPToolMatch{ServerID: "server-" + strconv.Itoa(i), ServerName: "same-name", ToolName: "tool"}
	}
	refs := codingSubAgentMCPToolReferences(tools, 12)
	if len(refs) != 13 || refs[0] != "server-0/tool" || refs[11] != "server-11/tool" || refs[12] != "... +1 more" {
		t.Fatalf("tool references = %#v", refs)
	}
}

func TestSelectRelevantMCPToolsForTask_FullEnvironmentDoesNotCapToolList(t *testing.T) {
	tools := make([]MCPToolView, 17)
	for i := range tools {
		tools[i] = MCPToolView{
			Name:        "tool_" + strconv.Itoa(i),
			Description: "MCP tool description",
		}
	}
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"tengyun-mcp": {
			entry:   corelib.LocalMCPServerEntry{Name: "tengyun-mcp"},
			running: true,
			tools:   tools,
		},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: &App{localMCPManager: manager}},
		fullEnvironment: true,
	}}

	matched := cb.selectRelevantMCPToolsForTask("query this week's work log")
	if len(matched) != len(tools) {
		t.Fatalf("full-environment MCP selection = %d tools, want all %d", len(matched), len(tools))
	}
	matchedNames := make(map[string]bool, len(matched))
	for _, tool := range matched {
		matchedNames[tool.ToolName] = true
	}
	for _, tool := range tools {
		if !matchedNames[tool.Name] {
			t.Fatalf("full-environment MCP selection omitted %q: %#v", tool.Name, matched)
		}
	}
}

func TestCodingSubAgentMCPSection_FullEnvironmentIncludesAllConnectedTools(t *testing.T) {
	tools := make([]MCPToolView, 17)
	for i := range tools {
		tools[i] = MCPToolView{Name: "tool_" + strconv.Itoa(i), Description: "MCP tool description"}
	}
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"tengyun-mcp": {
			entry:   corelib.LocalMCPServerEntry{Name: "tengyun-mcp"},
			running: true,
			tools:   tools,
		},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: &App{localMCPManager: manager}},
		fullEnvironment: true,
	}}

	section := cb.buildCodingSubAgentMCPSection()
	if len(cb.matchedMCPTools) != len(tools) {
		t.Fatalf("selected MCP tools = %d, want %d", len(cb.matchedMCPTools), len(tools))
	}
	for _, tool := range tools {
		if !strings.Contains(section, tool.Name) {
			t.Fatalf("MCP prompt section omitted %q: %q", tool.Name, section)
		}
	}
}

func TestCodingSubAgentMCPSectionShowsServerIDForUnambiguousInvocation(t *testing.T) {
	section := buildCodingSubAgentMCPSection([]codingSubAgentMCPToolMatch{{
		ServerID: "tengyun-msrgp18h", ServerName: "腾云 MCP", ToolName: "work_log_query",
	}})
	if !strings.Contains(section, "腾云 MCP [tengyun-msrgp18h]") {
		t.Fatalf("MCP prompt should include the server id, got %q", section)
	}
}

func TestSelectRelevantMCPToolsForTask_ExcludesDisabledMCPTargets(t *testing.T) {
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"test-mcp": {
			entry:   corelib.LocalMCPServerEntry{Name: "Test MCP"},
			running: true,
			tools: []MCPToolView{
				{Name: "create_session", Description: "disabled external coding session"},
				{Name: "allowed_tool", Description: "usable MCP tool"},
			},
		},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: &App{localMCPManager: manager}},
		fullEnvironment: true,
	}}
	matched := cb.selectRelevantMCPToolsForTask("use the MCP tool")
	if len(matched) != 1 || matched[0].ToolName != "allowed_tool" {
		t.Fatalf("matched tools = %#v, want only allowed_tool", matched)
	}
}

func TestSelectRelevantMCPToolsForTask_DeduplicatesAndNormalizesLocalTools(t *testing.T) {
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		" test-mcp ": {
			entry:   corelib.LocalMCPServerEntry{Name: " Test MCP "},
			running: true,
			tools: []MCPToolView{
				{Name: " lookup ", Description: "first definition"},
				{Name: "lookup", Description: "duplicate definition"},
				{Name: "  ", Description: "invalid empty name"},
			},
		},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: &App{localMCPManager: manager}},
		fullEnvironment: true,
	}}
	matched := cb.selectRelevantMCPToolsForTask("look up a record")
	if len(matched) != 1 {
		t.Fatalf("matched tools = %#v, want one normalized unique tool", matched)
	}
	if got := matched[0]; got.ServerID != "test-mcp" || got.ServerName != "Test MCP" || got.ToolName != "lookup" || got.Description != "first definition" {
		t.Fatalf("normalized tool = %#v", got)
	}
}

func TestSelectRelevantMCPToolsForTask_FullEnvironmentIncludesHealthyRemoteTools(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{
		{ID: "remote-mcp", Name: "Remote MCP", EndpointURL: "http://example.test/mcp"},
		{ID: "slow-mcp", Name: "Slow MCP", EndpointURL: "http://example.test/slow"},
	}}); err != nil {
		t.Fatal(err)
	}
	registry := NewMCPRegistry(app)
	registry.health["remote-mcp"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	registry.health["slow-mcp"] = &mcpHealthState{Status: mcpHealthStatusSlow}
	registry.toolsCache["remote-mcp"] = []MCPToolView{{Name: "remote_lookup", Description: "Look up remote records"}}
	registry.toolsCache["slow-mcp"] = []MCPToolView{{Name: "slow_lookup", Description: "Look up records on a slow server"}}
	app.mcpRegistry = registry

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: app},
		fullEnvironment: true,
	}}
	matched := cb.selectRelevantMCPToolsForTask("look up a remote record")
	if len(matched) != 2 {
		t.Fatalf("full-environment MCP selection = %#v, want both reachable remote tools", matched)
	}
	got := make(map[string]string, len(matched))
	for _, tool := range matched {
		got[tool.ServerID] = tool.ToolName
	}
	if got["remote-mcp"] != "remote_lookup" || got["slow-mcp"] != "slow_lookup" {
		t.Fatalf("remote tools = %#v", got)
	}
}

func TestSelectRelevantMCPToolsForTask_SkipsReachableRemoteServerWithoutCachedTools(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID: "remote-mcp", Name: "Remote MCP", EndpointURL: "http://127.0.0.1:1/mcp",
	}}}); err != nil {
		t.Fatal(err)
	}
	registry := NewMCPRegistry(app)
	registry.health["remote-mcp"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry = registry
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: app},
		fullEnvironment: true,
	}}

	if got := cb.selectRelevantMCPToolsForTask("look up a remote record"); got != nil {
		t.Fatalf("selection = %#v, want nil when no remote tool cache is available", got)
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

func TestIsMatchedMCPTool_WhitespaceInsensitive(t *testing.T) {
	cb := &codingSubAgentCallbacks{matchedMCPTools: []codingSubAgentMCPToolMatch{{
		ServerID: " test-mcp ", ServerName: " Test MCP ", ToolName: " lookup ",
	}}}
	if !cb.isMatchedMCPTool(" test-mcp ", " lookup ") {
		t.Fatal("server id and tool name should match after trimming whitespace")
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
	if !strings.Contains(result.Text, `missing required argument "tool_name"`) ||
		!strings.Contains(result.Text, `Example valid arguments:`) ||
		!strings.Contains(result.Text, `{"server_id":"server","tool_name":"tool","arguments":{}}`) {
		t.Fatalf("missing tool_name should use standard argument guidance, got %q", result.Text)
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

func TestMCPRequiredArgsMissingIncludesTargetSpecificExample(t *testing.T) {
	tool := codingSubAgentMCPToolMatch{ServerID: "browser", ServerName: "Browser", ToolName: "navigate", RequiredArgs: []string{"url", "timeout"}}
	args := map[string]interface{}{"server_id": "browser", "tool_name": "navigate", "arguments": map[string]interface{}{"timeout": float64(5)}}

	result, rejected := rejectMissingCodingSubAgentMCPRequiredArguments(tool, args)
	if !rejected {
		t.Fatal("expected missing MCP required arg to be rejected")
	}
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("outcome = %s, want failed", result.Outcome)
	}
	if !strings.Contains(result.Text, `arguments.url`) ||
		!strings.Contains(result.Text, `Example valid arguments:`) ||
		!strings.Contains(result.Text, `"server_id":"browser"`) ||
		!strings.Contains(result.Text, `"tool_name":"navigate"`) ||
		!strings.Contains(result.Text, `"url":"<url>"`) ||
		!strings.Contains(result.Text, `"timeout":"<timeout>"`) {
		t.Fatalf("missing MCP arg should include target-specific arguments example, got %q", result.Text)
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
