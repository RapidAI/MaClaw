package main

import (
	"fmt"
	"testing"
)

// makeDynamicTool creates a tool definition with the given name and description.
func makeDynamicTool(name, desc string) map[string]interface{} {
	return toolDef(name, desc, nil, nil)
}

// makeAllTools creates a slice of 28 builtins + n dynamic tools.
func makeAllTools(dynamicCount int) []map[string]interface{} {
	builtins := makeBuiltinDefs()
	for i := 0; i < dynamicCount; i++ {
		builtins = append(builtins, makeDynamicTool(
			fmt.Sprintf("dynamic_tool_%d", i),
			fmt.Sprintf("Dynamic tool number %d for testing", i),
		))
	}
	return builtins
}

func TestToolRouter_BelowBudget(t *testing.T) {
	// Use a subset of builtins (fewer than maxToolBudget) to test below-budget path.
	allTools := makeBuiltinDefs()[:20]
	router := NewToolRouter(nil)
	result := router.Route("hello world", allTools)

	if len(result) != len(allTools) {
		t.Errorf("expected %d tools (unchanged), got %d", len(allTools), len(result))
	}
}

func TestToolRouter_ExactlyAtBudget(t *testing.T) {
	// 28 builtins + 0 dynamic = 28 total, exactly at maxToolBudget.
	allTools := makeAllTools(0)
	router := NewToolRouter(nil)
	result := router.Route("test message", allTools)

	if len(result) != maxToolBudget {
		t.Errorf("expected %d tools (unchanged at budget), got %d", maxToolBudget, len(result))
	}
}

func TestToolRouter_AboveBudget_KeepsCoreTools(t *testing.T) {
	// 28 builtins + 20 dynamic = 48 total, above maxToolBudget.
	allTools := makeAllTools(20)
	router := NewToolRouter(nil)
	result := router.Route("some query", allTools)

	if len(result) > maxToolBudget {
		t.Errorf("expected at most %d tools, got %d", maxToolBudget, len(result))
	}

	// Verify all core tools are present.
	resultNames := make(map[string]bool)
	for _, tool := range result {
		resultNames[extractToolName(tool)] = true
	}
	for name := range coreToolNames {
		if !resultNames[name] {
			t.Errorf("core tool %q missing from result", name)
		}
	}
}

func TestToolRouter_AboveBudget_LimitsDynamic(t *testing.T) {
	// 28 builtins + 25 dynamic = 53 total.
	allTools := makeAllTools(25)
	router := NewToolRouter(nil)
	result := router.Route("some query", allTools)

	// Count dynamic tools in result.
	dynamicCount := 0
	for _, tool := range result {
		if !isBuiltinToolName(extractToolName(tool)) {
			dynamicCount++
		}
	}
	if dynamicCount > maxDynamicRouted {
		t.Errorf("expected at most %d dynamic tools, got %d", maxDynamicRouted, dynamicCount)
	}
}

func TestToolRouter_RelevanceRanking(t *testing.T) {
	builtins := makeBuiltinDefs()
	// 28 builtins = below maxToolBudget — all should be returned.
	router := NewToolRouter(nil)
	result := router.Route("search the web for golang tutorials", builtins)

	if len(result) != len(builtins) {
		t.Errorf("expected %d tools, got %d", len(builtins), len(result))
	}
}

func TestToolRouter_RelevanceRanking_AboveBudget(t *testing.T) {
	builtins := makeBuiltinDefs()
	// Add enough dynamic tools to exceed budget.
	var dynamic []map[string]interface{}
	dynamic = append(dynamic, makeDynamicTool("search_web", "Search the web for information"))
	for i := 0; i < 15; i++ {
		dynamic = append(dynamic, makeDynamicTool(
			fmt.Sprintf("filler_%d", i),
			fmt.Sprintf("Filler tool %d does nothing useful", i),
		))
	}
	allTools := append(builtins, dynamic...)

	router := NewToolRouter(nil)
	result := router.Route("search the web for golang tutorials", allTools)

	// search_web should be included due to high relevance.
	found := false
	for _, tool := range result {
		if extractToolName(tool) == "search_web" {
			found = true
			break
		}
	}
	if !found {
		t.Error("search_web tool should be included due to high relevance")
	}
}

func TestToolRouter_NonCoreBuiltinCompetes(t *testing.T) {
	// Non-core builtins like config tools should be included when relevant.
	builtins := makeBuiltinDefs()
	// Add config tools (non-core builtins).
	builtins = append(builtins,
		toolDef("get_config", "获取配置", nil, nil),
		toolDef("update_config", "修改配置", nil, nil),
		toolDef("craft_tool", "自动生成脚本", nil, nil),
		toolDef("create_scheduled_task", "创建定时任务", nil, nil),
	)
	// Add enough dynamic tools to exceed budget.
	for i := 0; i < 15; i++ {
		builtins = append(builtins, makeDynamicTool(
			fmt.Sprintf("filler_%d", i),
			fmt.Sprintf("Filler tool %d", i),
		))
	}

	router := NewToolRouter(nil)
	result := router.Route("帮我修改配置", builtins)

	// get_config and update_config should rank high due to "配置" match.
	resultNames := make(map[string]bool)
	for _, tool := range result {
		resultNames[extractToolName(tool)] = true
	}
	if !resultNames["get_config"] {
		t.Error("get_config should be included when user mentions 配置")
	}
	if !resultNames["update_config"] {
		t.Error("update_config should be included when user mentions 配置")
	}
}

func TestToolRouter_EmptyMessage(t *testing.T) {
	allTools := makeAllTools(20)
	router := NewToolRouter(nil)
	result := router.Route("", allTools)

	// Empty message → should cap at maxToolBudget.
	if len(result) > maxToolBudget {
		t.Errorf("expected at most %d tools, got %d", maxToolBudget, len(result))
	}
}

func TestToolRouter_EmptyTools(t *testing.T) {
	router := NewToolRouter(nil)
	result := router.Route("hello", nil)

	if len(result) != 0 {
		t.Errorf("expected 0 tools for nil input, got %d", len(result))
	}

	result = router.Route("hello", []map[string]interface{}{})
	if len(result) != 0 {
		t.Errorf("expected 0 tools for empty input, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Tokenization tests (now using bm25.Tokenize)
// ---------------------------------------------------------------------------

func TestExtractToolDescription(t *testing.T) {
	def := toolDef("my_tool", "A useful tool", nil, nil)
	desc := extractToolDescription(def)
	if desc != "A useful tool" {
		t.Errorf("expected 'A useful tool', got %q", desc)
	}

	// Invalid definition.
	desc = extractToolDescription(map[string]interface{}{})
	if desc != "" {
		t.Errorf("expected empty string, got %q", desc)
	}
}

// ---------------------------------------------------------------------------
// Hub recommendation matching tests
// ---------------------------------------------------------------------------

func TestToolRouter_SetHubClient(t *testing.T) {
	router := NewToolRouter(nil)
	if router.hubClient != nil {
		t.Error("expected nil hubClient initially")
	}

	router.SetHubClient(nil)
	if router.hubClient != nil {
		t.Error("expected nil hubClient after SetHubClient(nil)")
	}
}

func TestToolRouter_MatchRecommendations_NilHubClient(t *testing.T) {
	// When hubClient is nil, Route should not append any hint.
	allTools := makeAllTools(20)
	router := NewToolRouter(nil)
	result := router.Route("deploy my application", allTools)

	for _, tool := range result {
		if extractToolName(tool) == "search_and_install_skill" {
			t.Error("should not have search_and_install_skill hint when hubClient is nil")
		}
	}
}

func TestToolRouter_MatchRecommendations_NoTokens(t *testing.T) {
	// matchRecommendations with empty tokens should return nil.
	router := NewToolRouter(nil)
	hint := router.matchRecommendations(nil)
	if hint != nil {
		t.Error("expected nil hint for empty tokens")
	}
}

func TestSearchAndInstallSkillHint(t *testing.T) {
	hint := searchAndInstallSkillHint()

	name := extractToolName(hint)
	if name != "search_and_install_skill" {
		t.Errorf("expected name 'search_and_install_skill', got %q", name)
	}

	desc := extractToolDescription(hint)
	if desc == "" {
		t.Error("expected non-empty description for hint")
	}
}
