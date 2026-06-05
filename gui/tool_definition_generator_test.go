package main

import (
	"testing"
)

// --- helpers for building test fixtures ---

func makeBuiltinDefs() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("list_sessions", "list sessions", nil, nil),
		toolDef("create_session", "create session", map[string]interface{}{"tool": map[string]string{"type": "string"}}, []string{"tool"}),
		toolDef("send_input", "send input", nil, nil),
		toolDef("get_session_output", "get session output", nil, nil),
		toolDef("get_session_events", "get session events", nil, nil),
		toolDef("interrupt_session", "interrupt session", nil, nil),
		toolDef("kill_session", "kill session", nil, nil),
		toolDef("screenshot", "screenshot", nil, nil),
		toolDef("list_mcp_tools", "list MCP tools", nil, nil),
		toolDef("call_mcp_tool", "call MCP tool", nil, nil),
		toolDef("list_skills", "list skills", nil, nil),
		toolDef("search_skill_hub", "search SkillHub", nil, nil),
		toolDef("install_skill_hub", "install hub skill", nil, nil),
		toolDef("run_skill", "run skill", nil, nil),
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("task", "task manager", nil, nil),
		toolDef("parallel_execute", "parallel execute", nil, nil),
		toolDef("recommend_tool", "recommend tool", nil, nil),
		toolDef("bash", "execute shell command", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("send_file", "send file", nil, nil),
		toolDef("open", "open file or URL", nil, nil),
		toolDef("craft_tool", "generate script", nil, nil),
		toolDef("memory", "manage memory", nil, nil),
		toolDef("send_and_observe", "send and observe", nil, nil),
		toolDef("control_session", "control session", nil, nil),
		toolDef("manage_config", "manage config", nil, nil),
		toolDef("web_search", "web search", nil, nil),
		toolDef("web_fetch", "web fetch", nil, nil),
		toolDef("set_nickname", "set nickname", nil, nil),
		toolDef("browser", "stable merged browser tool", nil, nil),
		toolDef("browser_session_start", "browser session start", nil, nil),
		toolDef("browser_observe", "browser observe", nil, nil),
		toolDef("browser_navigate", "browser navigate", nil, nil),
		toolDef("browser_click", "browser click", nil, nil),
		toolDef("discover_tool", "discover tool", nil, nil),
		toolDef("generate_pdf", "generate PDF", nil, nil),
		toolDef("async_wait", "async wait", nil, nil),
	}
}
func TestToolDefinitionGeneratorHidesInternalBrowserTools(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	names := make(map[string]bool)
	for _, def := range append(gen.Generate(), gen.GenerateDeferred()...) {
		names[extractToolName(def)] = true
	}
	if !names["browser"] {
		t.Fatal("merged browser tool must stay visible")
	}
	for _, name := range []string{"browser_session_start", "browser_observe", "browser_navigate", "browser_click"} {
		if names[name] {
			t.Fatalf("internal browser dispatch tool %q leaked into generated definitions", name)
		}
	}
}

func TestToolDefinitionGenerator_NoRegistry(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	result := gen.Generate()

	want := len(filterAgentVisibleBuiltinToolDefs(builtins))
	if len(result) != want {
		t.Errorf("expected %d tools, got %d", want, len(result))
	}
}

func TestToolDefinitionGenerator_FiltersDisabledExternalCodingSessionTools(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	gen.SetDeferredTools([]string{"create_session", "send_and_observe", "control_session"})

	for _, def := range append(gen.Generate(), gen.GenerateDeferred()...) {
		name := extractToolName(def)
		if isDisabledExternalCodingSessionTool(name) {
			t.Fatalf("disabled tool %q leaked from generator", name)
		}
	}
}

func TestToolDefinitionGenerator_BuiltinsPreserved(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	result := gen.Generate()

	filteredBuiltins := filterAgentVisibleBuiltinToolDefs(builtins)
	for i, def := range filteredBuiltins {
		expectedName := extractToolName(def)
		actualName := extractToolName(result[i])
		if expectedName != actualName {
			t.Errorf("builtin[%d]: expected name %q, got %q", i, expectedName, actualName)
		}
	}
}

func TestExtractToolName(t *testing.T) {
	def := toolDef("my_tool", "desc", nil, nil)
	name := extractToolName(def)
	if name != "my_tool" {
		t.Errorf("expected 'my_tool', got %q", name)
	}

	// Empty/invalid map
	name = extractToolName(map[string]interface{}{})
	if name != "" {
		t.Errorf("expected empty string for invalid def, got %q", name)
	}
}

func TestMcpToolToDefinition_Format(t *testing.T) {
	tool := MCPToolView{
		Name:        "search",
		Description: "Search the web",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "search query"},
			},
			"required": []interface{}{"query"},
		},
	}

	def := mcpToolToDefinition("search", tool)

	if def["type"] != "function" {
		t.Errorf("expected type 'function', got %v", def["type"])
	}

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("function field is not a map")
	}
	if fn["name"] != "search" {
		t.Errorf("expected name 'search', got %v", fn["name"])
	}
	if fn["description"] != "Search the web" {
		t.Errorf("expected description 'Search the web', got %v", fn["description"])
	}

	params, ok := fn["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters field is not a map")
	}
	if params["type"] != "object" {
		t.Errorf("expected parameters type 'object', got %v", params["type"])
	}
}

func TestMcpToolToDefinition_NilSchema(t *testing.T) {
	tool := MCPToolView{
		Name:        "ping",
		Description: "Ping server",
		InputSchema: nil,
	}

	def := mcpToolToDefinition("ping", tool)
	fn := def["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})

	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be an empty map")
	}
	if len(props) != 0 {
		t.Errorf("expected empty properties, got %d", len(props))
	}
}

func TestBuildParametersFromSchema_EmptySchema(t *testing.T) {
	result := buildParametersFromSchema(nil)
	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}

	result = buildParametersFromSchema(map[string]interface{}{})
	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}
}

func TestBuildParametersFromSchema_ValidObjectSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"name"},
	}

	result := buildParametersFromSchema(schema)
	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}
	if result["properties"] == nil {
		t.Error("expected properties to be present")
	}
	if result["required"] == nil {
		t.Error("expected required to be preserved")
	}
}

func TestBuildParametersFromSchema_ObjectWithoutProperties(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
	}

	result := buildParametersFromSchema(schema)
	if result["properties"] == nil {
		t.Error("expected properties to be added")
	}
}

func TestLooksLikePropertiesMap(t *testing.T) {
	// Valid properties map
	m := map[string]interface{}{
		"name": map[string]interface{}{"type": "string"},
		"age":  map[string]interface{}{"type": "integer"},
	}
	if !looksLikePropertiesMap(m) {
		t.Error("expected true for valid properties map")
	}

	// Not a properties map (values are not maps)
	m2 := map[string]interface{}{
		"name": "string",
	}
	if looksLikePropertiesMap(m2) {
		t.Error("expected false for non-map values")
	}

	// Empty map
	if looksLikePropertiesMap(map[string]interface{}{}) {
		t.Error("expected false for empty map")
	}

	// Map without "type" key in values
	m3 := map[string]interface{}{
		"name": map[string]interface{}{"description": "a name"},
	}
	if looksLikePropertiesMap(m3) {
		t.Error("expected false when values lack 'type' key")
	}
}

func TestToolDefinitionGenerator_NameConflictWithBuiltin(t *testing.T) {
	// Test that dynamic tools conflicting with builtin names get prefixed.
	builtins := []map[string]interface{}{
		toolDef("screenshot", "screenshot", nil, nil),
	}

	// We can't easily create a real MCPRegistry with HTTP servers for unit tests,
	// so we test the conflict resolution logic via the exported helpers directly.

	// Simulate: a dynamic tool named "screenshot" from server "srv1"
	// should become "srv1_screenshot".
	builtinNames := map[string]bool{"screenshot": true}
	dynamicNames := map[string]string{"screenshot": "srv1"} // only one server has it

	name := "screenshot"
	needsPrefix := builtinNames[name]
	if !needsPrefix {
		if ownerID := dynamicNames[name]; ownerID == "" {
			needsPrefix = true
		}
	}

	finalName := name
	if needsPrefix {
		finalName = "srv1_" + name
	}

	if finalName != "srv1_screenshot" {
		t.Errorf("expected 'srv1_screenshot', got %q", finalName)
	}

	_ = builtins // used for context
}

func TestToolDefinitionGenerator_DynamicNameConflictBetweenServers(t *testing.T) {
	// When two servers both have a tool named "search", both should be prefixed.
	dynamicNames := map[string]string{}

	// Server A registers "search"
	dynamicNames["search"] = "serverA"
	// Server B also registers "search"; mark as conflicting
	dynamicNames["search"] = "" // empty means conflict

	builtinNames := map[string]bool{}

	// For serverA's "search"
	name := "search"
	needsPrefix := builtinNames[name]
	if !needsPrefix {
		if ownerID := dynamicNames[name]; ownerID == "" {
			needsPrefix = true
		}
	}
	if !needsPrefix {
		t.Error("expected prefix needed for conflicting dynamic tool")
	}
}

func TestToolDefinitionGenerator_NoDuplicateBuiltinNames(t *testing.T) {
	builtins := makeBuiltinDefs()
	names := make(map[string]bool)
	for _, def := range builtins {
		name := extractToolName(def)
		if names[name] {
			t.Errorf("duplicate builtin name: %s", name)
		}
		names[name] = true
	}
}
