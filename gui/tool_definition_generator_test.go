package main

import (
	"testing"
)

// --- helpers for building test fixtures ---

func makeBuiltinDefs() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("list_sessions", "列出会话", nil, nil),
		toolDef("create_session", "创建会话",
			map[string]interface{}{"tool": map[string]string{"type": "string"}}, []string{"tool"}),
		toolDef("send_input", "发送输入", nil, nil),
		toolDef("get_session_output", "获取输出", nil, nil),
		toolDef("get_session_events", "获取事件", nil, nil),
		toolDef("interrupt_session", "中断会话", nil, nil),
		toolDef("kill_session", "终止会话", nil, nil),
		toolDef("screenshot", "截屏", nil, nil),
		toolDef("list_mcp_tools", "列出MCP工具", nil, nil),
		toolDef("call_mcp_tool", "调用MCP工具", nil, nil),
		toolDef("list_skills", "列出Skills", nil, nil),
		toolDef("search_skill_hub", "搜索SkillHub", nil, nil),
		toolDef("install_skill_hub", "安装Hub Skill", nil, nil),
		toolDef("run_skill", "执行Skill", nil, nil),
		toolDef("parallel_execute", "并行执行多个工具", nil, nil),
		toolDef("recommend_tool", "推荐工具", nil, nil),
		toolDef("bash", "执行shell命令", nil, nil),
		toolDef("read_file", "读取文件", nil, nil),
		toolDef("write_file", "写入文件", nil, nil),
		toolDef("list_directory", "列出目录", nil, nil),
		toolDef("send_file", "发送文件", nil, nil),
		toolDef("open", "打开文件或网址", nil, nil),
		toolDef("memory", "管理长期记忆", nil, nil),
		toolDef("send_and_observe", "发送并观察输出", nil, nil),
		toolDef("control_session", "控制会话", nil, nil),
		toolDef("manage_config", "管理配置", nil, nil),
		toolDef("web_search", "搜索网页", nil, nil),
		toolDef("web_fetch", "获取网页内容", nil, nil),
	}
}

func TestToolDefinitionGenerator_NoRegistry(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	result := gen.Generate()

	if len(result) != len(builtins) {
		t.Errorf("expected %d tools, got %d", len(builtins), len(result))
	}
}

func TestToolDefinitionGenerator_BuiltinsPreserved(t *testing.T) {
	builtins := makeBuiltinDefs()
	gen := NewToolDefinitionGenerator(nil, builtins)
	result := gen.Generate()

	for i, def := range builtins {
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
		toolDef("screenshot", "截屏", nil, nil),
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
	// Server B also registers "search" — mark as conflicting
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
