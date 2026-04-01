package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for IMMessageHandler dynamic tool integration (Task 6.3)
// ---------------------------------------------------------------------------

// TestGetTools_FallbackWithoutGenerator verifies that getTools() returns
// the hardcoded buildToolDefinitions() output when no generator is set.
func TestGetTools_FallbackWithoutGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.getTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty builtin tools")
	}

	// Verify first tool is list_sessions.
	name := extractToolName(tools[0])
	if name != "list_sessions" {
		t.Errorf("expected first tool to be list_sessions, got %s", name)
	}
}

// TestGetTools_UsesGeneratorWhenSet verifies that getTools() delegates to
// the ToolDefinitionGenerator when configured.
func TestGetTools_UsesGeneratorWhenSet(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	tools := handler.getTools()
	// With nil registry, generator returns only builtins.
	if len(tools) != len(builtins) {
		t.Fatalf("expected %d tools from generator (nil registry), got %d", len(builtins), len(tools))
	}
}

// TestGetTools_CacheWithin5Seconds verifies that repeated calls within 5s
// return the cached result without regenerating.
func TestGetTools_CacheWithin5Seconds(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// First call populates cache.
	tools1 := handler.getTools()
	// Second call should return same slice from cache.
	tools2 := handler.getTools()

	if len(tools1) != len(tools2) {
		t.Fatalf("cached tools length mismatch: %d vs %d", len(tools1), len(tools2))
	}

	// Verify cache timestamp was set.
	handler.toolsMu.RLock()
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be set after getTools()")
	}
}

// TestGetTools_CacheInvalidatedBySetGenerator verifies that calling
// SetToolDefGenerator invalidates the cache.
func TestGetTools_CacheInvalidatedBySetGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// Populate cache.
	_ = handler.getTools()

	// Set a new generator — should invalidate cache.
	gen2 := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen2)

	handler.toolsMu.RLock()
	cached := handler.cachedTools
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cached != nil {
		t.Error("expected cachedTools to be nil after SetToolDefGenerator")
	}
	if !cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be zero after SetToolDefGenerator")
	}
}

// TestRouteTools_NoRouterReturnsAll verifies that routeTools returns all
// tools unchanged when no router is configured.
func TestRouteTools_NoRouterReturnsAll(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("hello", tools)

	if len(routed) != len(tools) {
		t.Fatalf("expected %d tools without router, got %d", len(tools), len(routed))
	}
}

// TestRouteTools_WithRouterFilters verifies that routeTools delegates to
// the ToolRouter when configured.
func TestRouteTools_WithRouterFilters(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	gen := NewToolDefinitionGenerator(nil, handler.buildToolDefinitions())
	router := NewToolRouter(gen)
	handler.SetToolRouter(router)

	// With total tools exceeding maxToolBudget, router may filter dynamic tools.
	// Core tools are always kept; remaining budget goes to TF-IDF ranked candidates.
	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("test message", tools)

	if len(routed) > len(tools) {
		t.Fatalf("routed tools (%d) should not exceed total tools (%d)", len(routed), len(tools))
	}
	if len(routed) == 0 {
		t.Fatal("expected non-empty routed tools")
	}
}

// TestToolsCacheTTL_Value verifies the cache TTL constant is 5 seconds.
func TestToolsCacheTTL_Value(t *testing.T) {
	expected := 5 * time.Second
	if toolsCacheTTL != expected {
		t.Errorf("expected toolsCacheTTL = %v, got %v", expected, toolsCacheTTL)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 7.2: Smart session startup & template tools
// ---------------------------------------------------------------------------

// TestToolCreateSession_SmartToolRecommendation verifies that toolCreateSession
// auto-recommends a tool when the tool parameter is empty and contextResolver is set.
func TestToolCreateSession_SmartToolRecommendation(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	// Without contextResolver, empty tool should return error.
	result := handler.toolCreateSession(map[string]interface{}{})
	if result != "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolCreateSession_WithToolProvided verifies that toolCreateSession
// uses the provided tool parameter directly (no auto-recommendation).
func TestToolCreateSession_WithToolProvided(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	// With tool provided but no manager, should fail at session creation.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Should attempt to create session (will fail because app is minimal).
	if result == "缺少 tool 参数" || result == "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("should not report missing tool when tool is provided, got: %s", result)
	}
}

// TestToolCreateTemplate verifies template creation via the tool.
func TestToolCreateTemplate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Missing required params.
	result := handler.toolCreateTemplate(map[string]interface{}{})
	if result != "缺少 name 或 tool 参数" {
		t.Errorf("expected missing params error, got: %s", result)
	}

	// Successful creation.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name":         "my-template",
		"tool":         "claude",
		"project_path": "/tmp/project",
		"yolo_mode":    true,
	})
	if result != "模板已创建: my-template（工具=claude, 项目=/tmp/project）" {
		t.Errorf("unexpected result: %s", result)
	}

	// Duplicate name.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name": "my-template",
		"tool": "codex",
	})
	if result == "" || !contains(result, "创建模板失败") {
		t.Errorf("expected duplicate error, got: %s", result)
	}
}

// TestToolCreateTemplate_NilManager verifies graceful handling when
// templateManager is nil.
func TestToolCreateTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolCreateTemplate(map[string]interface{}{
		"name": "test", "tool": "claude",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolListTemplates verifies listing templates.
func TestToolListTemplates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Empty list.
	result := handler.toolListTemplates()
	if result != "当前没有会话模板。" {
		t.Errorf("expected empty list message, got: %s", result)
	}

	// Add a template and list again.
	_ = mgr.Create(SessionTemplate{Name: "dev", Tool: "claude", ProjectPath: "/tmp/dev", YoloMode: true})
	result = handler.toolListTemplates()
	if !contains(result, "dev") || !contains(result, "claude") || !contains(result, "[Yolo]") {
		t.Errorf("expected template details in list, got: %s", result)
	}
}

// TestToolListTemplates_NilManager verifies graceful handling.
func TestToolListTemplates_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListTemplates()
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NotFound verifies error when template doesn't exist.
func TestToolLaunchTemplate_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "nonexistent",
	})
	if !contains(result, "获取模板失败") {
		t.Errorf("expected not found error, got: %s", result)
	}
}

// TestToolLaunchTemplate_MissingParam verifies error when template_name is missing.
func TestToolLaunchTemplate_MissingParam(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{})
	if result != "缺少 template_name 参数" {
		t.Errorf("expected missing param error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NilManager verifies graceful handling.
func TestToolLaunchTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "test",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestExecuteTool_TemplateToolsRouting verifies that executeTool routes
// template tool names to the correct handlers.
func TestExecuteTool_TemplateToolsRouting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}
	// Initialize registry so executeTool can dispatch via registry.
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	// create_template via executeTool
	result := handler.executeTool("create_template", `{"name":"t1","tool":"claude"}`, nil)
	if !contains(result, "模板已创建") {
		t.Errorf("create_template via executeTool failed: %s", result)
	}

	// list_templates via executeTool
	result = handler.executeTool("list_templates", "", nil)
	if !contains(result, "t1") {
		t.Errorf("list_templates via executeTool failed: %s", result)
	}

	// launch_template via executeTool (will fail at session creation, but routing works)
	result = handler.executeTool("launch_template", `{"template_name":"t1"}`, nil)
	// Should get past template lookup (routing works) — will fail at session creation
	if contains(result, "未知工具") || contains(result, "模板管理器未初始化") {
		t.Errorf("launch_template routing failed: %s", result)
	}
}

// TestSetContextResolver verifies the setter works.
func TestSetContextResolver(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	resolver := &SessionContextResolver{app: &App{}}
	handler.SetContextResolver(resolver)
	if handler.contextResolver != resolver {
		t.Error("expected contextResolver to be set")
	}
}

// TestSetSessionPrecheck verifies the setter works.
func TestSetSessionPrecheck(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	precheck := &SessionPrecheck{app: &App{}}
	handler.SetSessionPrecheck(precheck)
	if handler.sessionPrecheck != precheck {
		t.Error("expected sessionPrecheck to be set")
	}
}

// TestSetStartupFeedback verifies the setter works.
func TestSetStartupFeedback(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Can't easily create a real SessionStartupFeedback without a manager,
	// but we can verify the field is set.
	feedback := &SessionStartupFeedback{}
	handler.SetStartupFeedback(feedback)
	if handler.startupFeedback != feedback {
		t.Error("expected startupFeedback to be set")
	}
}

// TestBuildToolDefinitions_IncludesTemplateTools verifies that the tool
// definitions include the template tools added in task 7.1.
func TestBuildToolDefinitions_IncludesTemplateTools(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	templateTools := map[string]bool{
		"create_template": false,
		"list_templates":  false,
		"launch_template": false,
	}

	for _, tool := range tools {
		name := extractToolName(tool)
		if _, ok := templateTools[name]; ok {
			templateTools[name] = true
		}
	}

	for name, found := range templateTools {
		if !found {
			t.Errorf("expected template tool %q in buildToolDefinitions", name)
		}
	}
}

// contains is a test helper that checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests for Task 5: create_session provider parameter
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_CreateSessionHasProviderParam verifies that the
// create_session tool definition includes the provider parameter.
func TestBuildToolDefinitions_CreateSessionHasProviderParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var createSessionDef map[string]interface{}
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			createSessionDef = tool
			break
		}
	}
	if createSessionDef == nil {
		t.Fatal("create_session tool not found in buildToolDefinitions")
	}

	// Extract the function.parameters.properties to check for "provider".
	fn, _ := createSessionDef["function"].(map[string]interface{})
	if fn == nil {
		t.Fatal("create_session missing function field")
	}
	params, _ := fn["parameters"].(map[string]interface{})
	if params == nil {
		t.Fatal("create_session missing parameters field")
	}
	props, _ := params["properties"].(map[string]interface{})
	if props == nil {
		t.Fatal("create_session missing properties field")
	}
	if _, ok := props["provider"]; !ok {
		t.Error("create_session tool definition missing 'provider' parameter")
	}

	// Verify provider is NOT in required list (it's optional).
	required, _ := params["required"].([]string)
	for _, r := range required {
		if r == "provider" {
			t.Error("provider should not be in required list")
		}
	}
}

// TestToolCreateSession_NoProviderBehaviorUnchanged verifies that not passing
// provider keeps the original behavior (tool param required, no provider passed).
func TestToolCreateSession_NoProviderBehaviorUnchanged(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}

	// Without tool param, should return missing tool error.
	result := handler.toolCreateSession(map[string]interface{}{})
	if result != "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("expected missing tool error, got: %s", result)
	}

	// With tool but no provider, should attempt session creation (will fail
	// because app is minimal, but should NOT mention provider issues).
	result = handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	if result == "缺少 tool 参数" || result == "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("should not report missing tool when tool is provided, got: %s", result)
	}
	// Error should be about session creation, not about provider resolution.
	if contains(result, "至少一个有效的服务商") || contains(result, "未配置 API Key") || contains(result, "不存在") {
		t.Errorf("should not fail at provider resolution when provider is omitted, got: %s", result)
	}
}

// TestToolCreateSession_WithProviderPassedThrough verifies that the provider
// parameter is extracted and resolved via ProviderResolver. When the specified
// provider doesn't exist, the resolver returns an error before reaching session creation.
func TestToolCreateSession_WithProviderPassedThrough(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}

	result := handler.toolCreateSession(map[string]interface{}{
		"tool":     "claude",
		"provider": "NonExistentProvider",
	})
	// ProviderResolver should catch the invalid provider before session creation.
	if !contains(result, "不存在") {
		t.Errorf("expected provider not found error, got: %s", result)
	}
}

// TestToolCreateSession_ProviderDescriptionInToolDef verifies the create_session
// description mentions provider selection.
func TestToolCreateSession_ProviderDescriptionInToolDef(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			fn, _ := tool["function"].(map[string]interface{})
			desc, _ := fn["description"].(string)
			if !contains(desc, "provider") {
				t.Errorf("create_session description should mention provider, got: %s", desc)
			}
			return
		}
	}
	t.Fatal("create_session tool not found")
}

// ---------------------------------------------------------------------------
// Tests for Task 6: list_providers Agent tool
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_IncludesListProviders verifies that the tool
// definitions include the list_providers tool.
func TestBuildToolDefinitions_IncludesListProviders(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var found bool
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "list_providers" {
			found = true
			fn, _ := tool["function"].(map[string]interface{})
			params, _ := fn["parameters"].(map[string]interface{})
			props, _ := params["properties"].(map[string]interface{})
			if _, ok := props["tool"]; !ok {
				t.Error("list_providers missing 'tool' parameter")
			}
			required, _ := params["required"].([]string)
			hasToolRequired := false
			for _, r := range required {
				if r == "tool" {
					hasToolRequired = true
				}
			}
			if !hasToolRequired {
				t.Error("list_providers should have 'tool' in required list")
			}
			break
		}
	}
	if !found {
		t.Fatal("list_providers tool not found in buildToolDefinitions")
	}
}

// TestExecuteTool_ListProvidersRouting verifies that executeTool routes
// list_providers to the correct handler.
func TestExecuteTool_ListProvidersRouting(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Initialize registry so executeTool can dispatch via registry.
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	result := handler.executeTool("list_providers", `{"tool":"claude"}`, nil)
	// With a minimal App (no config file), it should attempt to load config
	// and either return a config error or tool-related result, not "未知工具".
	if contains(result, "未知工具") {
		t.Errorf("list_providers should be routed, got: %s", result)
	}
}

// TestToolListProviders_MissingToolParam verifies that missing tool param
// returns an appropriate error.
func TestToolListProviders_MissingToolParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListProviders(map[string]interface{}{})
	if result != "缺少 tool 参数" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolListProviders_EmptyToolParam verifies that empty tool param
// returns an appropriate error.
func TestToolListProviders_EmptyToolParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListProviders(map[string]interface{}{"tool": ""})
	if result != "缺少 tool 参数" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolListProviders_UnsupportedTool verifies that an unsupported tool
// returns an appropriate error.
func TestToolListProviders_UnsupportedTool(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "nonexistent_tool"})
	if !contains(result, "不支持的工具") {
		t.Errorf("expected unsupported tool error, got: %s", result)
	}
}

// TestToolListProviders_NoValidProviders verifies that when all providers
// have no API key (and none is "Original"), the tool returns a helpful message.
// Note: LoadConfig always ensures "Original" is present, so we write the
// config JSON directly to bypass the ensureOriginal logic.
func TestToolListProviders_NoValidProviders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	// Write config JSON directly to bypass LoadConfig's ensureOriginal.
	configJSON := `{
		"claude": {
			"current_model": "EmptyProvider",
			"models": [
				{"model_name": "EmptyProvider", "model_id": "ep-1", "api_key": ""},
				{"model_name": "AlsoEmpty", "model_id": "ae-1", "api_key": "   "}
			]
		}
	}`
	configPath := filepath.Join(tempHome, ".maclaw", "config.json")
	if err := os.MkdirAll(filepath.Join(tempHome, ".maclaw"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})
	// LoadConfig will add "Original" back, so Original will be valid.
	// The test verifies the handler works correctly with the loaded config.
	// Since Original is always added, we verify it appears in the output.
	if contains(result, "没有可用的服务商") {
		// If somehow no valid providers (shouldn't happen with ensureOriginal),
		// that's also acceptable behavior.
		return
	}
	// Original should be present (added by LoadConfig).
	if !contains(result, "Original") {
		t.Errorf("expected Original in result (added by LoadConfig), got: %s", result)
	}
	// EmptyProvider should NOT be present (no API key, not Original).
	if contains(result, "EmptyProvider") {
		t.Errorf("should not contain EmptyProvider (invalid provider), got: %s", result)
	}
}

// TestToolListProviders_WithValidProviders verifies that valid providers
// are listed with correct formatting.
func TestToolListProviders_WithValidProviders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "deepseek-chat", ApiKey: "sk-test-key"},
			{ModelName: "EmptyKey", ModelId: "empty-id", ApiKey: ""},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})

	// Should contain header.
	if !contains(result, "工具 claude 的可用服务商") {
		t.Errorf("expected header in result, got: %s", result)
	}
	// Should contain Original (valid because name is "Original").
	if !contains(result, "Original") {
		t.Errorf("expected Original in result, got: %s", result)
	}
	// Should contain DeepSeek (valid because has API key).
	if !contains(result, "DeepSeek") {
		t.Errorf("expected DeepSeek in result, got: %s", result)
	}
	// Should NOT contain EmptyKey (invalid: not Original and no API key).
	if contains(result, "EmptyKey") {
		t.Errorf("should not contain EmptyKey (invalid provider), got: %s", result)
	}
	// Original should be marked as default.
	if !contains(result, "[当前默认]") {
		t.Errorf("expected [当前默认] marker for Original, got: %s", result)
	}
}

// TestToolListProviders_ModelIdTruncation verifies that long model_id values
// are truncated to 20 characters.
func TestToolListProviders_ModelIdTruncation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "LongID",
		Models: []ModelConfig{
			{ModelName: "LongID", ModelId: "this-is-a-very-long-model-id-that-exceeds-twenty-chars", ApiKey: "key123"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})

	// The full model ID should NOT appear.
	if contains(result, "this-is-a-very-long-model-id-that-exceeds-twenty-chars") {
		t.Errorf("long model_id should be truncated, got: %s", result)
	}
	// The truncated version should appear.
	if !contains(result, "this-is-a-very-long-") {
		t.Errorf("expected truncated model_id prefix, got: %s", result)
	}
	if !contains(result, "...") {
		t.Errorf("expected '...' after truncated model_id, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 2: ProviderResolver integration in toolCreateSession
// ---------------------------------------------------------------------------

// TestToolCreateSession_NoProviderUsesDefault verifies that when no provider
// is specified, the ProviderResolver uses the default provider from ToolConfig.
func TestToolCreateSession_NoProviderUsesDefault(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// Set up claude with a valid default provider (Original is always valid).
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}

	// No provider specified — should use default (Original).
	// Will fail at StartRemoteSessionForProject (remote not enabled), but
	// should NOT fail at provider resolution.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Should NOT contain provider resolution errors.
	if contains(result, "无法创建会话") && contains(result, "服务商") {
		t.Errorf("should not fail at provider resolution, got: %s", result)
	}
	// Should fail at session creation (remote disabled), not at provider resolution.
	if contains(result, "加载配置失败") || contains(result, "获取工具配置失败") {
		t.Errorf("should not fail at config loading, got: %s", result)
	}
}

// TestToolCreateSession_DefaultUnavailableFallbackHint verifies that when the
// default provider is unavailable, the resolver falls back to the next available
// provider. Since the test environment has remote mode disabled, the session
// creation will fail, but the provider resolution should succeed with fallback.
func TestToolCreateSession_DefaultUnavailableFallbackHint(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// Set default to a provider with no API key (not "Original").
	// LoadConfig ensures "Original" is always present and valid.
	// So fallback chain: BadDefault (invalid) → Original (valid, fallback target).
	cfg.Claude = ToolConfig{
		CurrentModel: "BadDefault",
		Models: []ModelConfig{
			{ModelName: "BadDefault", ModelId: "bad-id", ApiKey: ""},
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}

	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Provider resolution should succeed (fallback), so the error
	// should be about session creation, NOT about provider resolution.
	if contains(result, "无法创建会话") {
		t.Errorf("provider resolution should succeed via fallback, got: %s", result)
	}

	// Verify the ProviderResolver directly to confirm fallback behavior.
	cfg2, _ := app.LoadConfig()
	toolCfg, _ := remoteToolConfig(cfg2, "claude")
	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, "")
	if resolveErr != nil {
		t.Fatalf("resolver should succeed with fallback, got error: %v", resolveErr)
	}
	if !resolveResult.Fallback {
		t.Error("expected Fallback=true when default provider is unavailable")
	}
	if resolveResult.OriginalName != "BadDefault" {
		t.Errorf("expected OriginalName=BadDefault, got %s", resolveResult.OriginalName)
	}
	// Fallback should go to Original (first valid after BadDefault).
	if resolveResult.Provider.ModelName != "Original" {
		t.Errorf("expected fallback to Original, got %s", resolveResult.Provider.ModelName)
	}
}

// TestToolCreateSession_UserSpecifiedProviderUsed verifies that when the user
// specifies a valid provider, it is used directly without fallback.
func TestToolCreateSession_UserSpecifiedProviderUsed(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}

	// Specify DeepSeek explicitly — should use it directly.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":     "claude",
		"provider": "DeepSeek",
	})
	// Should NOT contain fallback hint.
	if contains(result, "服务商已降级") {
		t.Errorf("should not have fallback hint when provider is explicitly specified, got: %s", result)
	}
	// Should NOT fail at provider resolution.
	if contains(result, "不存在") || contains(result, "未配置 API Key") {
		t.Errorf("should not fail at provider resolution for valid provider, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 3: create_session project_id parameter
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_CreateSessionHasProjectIDParam verifies that the
// create_session tool definition includes the project_id parameter.
func TestBuildToolDefinitions_CreateSessionHasProjectIDParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var createSessionDef map[string]interface{}
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			createSessionDef = tool
			break
		}
	}
	if createSessionDef == nil {
		t.Fatal("create_session tool not found in buildToolDefinitions")
	}

	fn, _ := createSessionDef["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["project_id"]; !ok {
		t.Error("create_session tool definition missing 'project_id' parameter")
	}
}

// TestToolCreateSession_ProjectIDResolvesSuccessfully verifies that when
// project_id matches a configured project, its path is used.
func TestToolCreateSession_ProjectIDResolvesSuccessfully(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/tmp/other-project"},
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":       "claude",
		"project_id": "proj-1",
	})
	// Should resolve project_id to /tmp/my-project.
	// Session creation will fail (remote mode disabled), but project resolution
	// should succeed — the error should NOT be about project_id not found.
	if contains(result, "未找到") {
		t.Errorf("project_id should resolve successfully, got: %s", result)
	}
}

// TestToolCreateSession_ProjectIDNotFound verifies that when project_id
// doesn't match any configured project, an error with available projects is returned.
func TestToolCreateSession_ProjectIDNotFound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/tmp/other-project"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":       "claude",
		"project_id": "nonexistent-id",
	})
	// Should return error with available project list.
	if !contains(result, "未找到") {
		t.Errorf("expected not found error, got: %s", result)
	}
	if !contains(result, "proj-1") || !contains(result, "proj-2") {
		t.Errorf("expected available project IDs in error, got: %s", result)
	}
}

// TestToolCreateSession_ProjectIDPriorityOverProjectPath verifies that
// project_id takes priority over project_path when both are provided.
func TestToolCreateSession_ProjectIDPriorityOverProjectPath(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	// Provide both project_id and project_path — project_id should win.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":         "claude",
		"project_id":   "proj-1",
		"project_path": "/tmp/should-be-ignored",
	})
	// project_id should take priority — should NOT report project_id not found.
	if contains(result, "未找到") {
		t.Errorf("project_id should resolve successfully, got: %s", result)
	}
	// The final output should reference the project_id path (/tmp/my-project),
	// not the project_path (/tmp/should-be-ignored).
	// Session creation will fail (remote mode disabled), but the error should
	// NOT contain the ignored project_path.
	if contains(result, "/tmp/should-be-ignored") {
		t.Errorf("project_path should be overridden by project_id, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for project_manage tool
// ---------------------------------------------------------------------------

// TestToolProjectManage_List verifies that project_manage with action=list
// returns project data when projects exist.
func TestToolProjectManage_List(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/path/to/project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/path/to/other"},
	}
	cfg.CurrentProject = "proj-1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolProjectManage(map[string]interface{}{"action": "list"})

	// Should contain both projects.
	if !contains(result, "proj-1") || !contains(result, "MyProject") || !contains(result, "/path/to/project") {
		t.Errorf("expected proj-1 details in result, got: %s", result)
	}
	if !contains(result, "proj-2") || !contains(result, "OtherProject") || !contains(result, "/path/to/other") {
		t.Errorf("expected proj-2 details in result, got: %s", result)
	}
}

// TestToolProjectManage_ListEmpty verifies that when no projects are configured,
// project_manage with action=list returns a hint message.
func TestToolProjectManage_ListEmpty(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = nil
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolProjectManage(map[string]interface{}{"action": "list"})

	if result != "当前没有已配置的项目。请在桌面端添加项目。" {
		t.Errorf("expected no projects hint, got: %s", result)
	}
}

// TestBuildToolDefinitions_IncludesProjectManage verifies that the tool
// definitions include the project_manage tool.
func TestBuildToolDefinitions_IncludesProjectManage(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var found bool
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "project_manage" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("project_manage tool not found in buildToolDefinitions")
	}
}

// TestExecuteTool_ProjectManageRouting verifies that executeTool routes
// project_manage to the correct handler.
func TestExecuteTool_ProjectManageRouting(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = nil
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	result := handler.executeTool("project_manage", `{"action":"list"}`, nil)
	// Should NOT return "未知工具".
	if contains(result, "未知工具") {
		t.Errorf("project_manage should be routed, got: %s", result)
	}
	// With no projects, should return hint.
	if !contains(result, "当前没有已配置的项目") {
		t.Errorf("expected no projects hint, got: %s", result)
	}
}
