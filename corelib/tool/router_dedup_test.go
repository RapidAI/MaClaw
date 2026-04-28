package tool

import (
	"testing"
)

// TestRouter_Route_NoDuplicateToolNames verifies the invariant:
// Route() output never contains duplicate tool names.
//
// Regression test for DeepSeek API error "Tool names must be unique" (HTTP 400).
//
// Root cause: search_and_install_skill entered the result twice — once from
// CoreToolNames (registered by syncSkillHubTools) and once from the
// matchRecommendations hint. The fix enforces uniqueness via a seenNames
// set at the core/candidate split, reused by all downstream append paths.
func TestRouter_Route_NoDuplicateToolNames(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Set up a recommender that always returns a recommendation (triggers hint).
	router.SetRecommender(&alwaysMatchRecommender{})

	// Build tools including search_and_install_skill (a CoreToolName).
	tools := make([]map[string]interface{}, 0, MaxToolBudget+5)
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "description for "+name))
	}
	// Exceed MaxToolBudget to trigger routing logic.
	for i := len(tools); i < MaxToolBudget+5; i++ {
		tools = append(tools, makeToolDef(
			"extra_tool_"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"extra tool",
		))
	}

	result := router.Route("搜索一个PDF转换工具", tools)

	seen := make(map[string]int, len(result))
	for _, td := range result {
		name := ExtractToolName(td)
		seen[name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("duplicate tool name %q appears %d times in Route() output", name, count)
		}
	}
	if seen["search_and_install_skill"] != 1 {
		t.Errorf("search_and_install_skill count = %d, want 1", seen["search_and_install_skill"])
	}
}

// TestRouter_Route_NoDuplicateToolNames_InputDuplicates verifies that even if
// the input allTools contains duplicates (e.g. from a buggy tool builder or
// MCP server), Route() output is still unique.
func TestRouter_Route_NoDuplicateToolNames_InputDuplicates(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	tools := make([]map[string]interface{}, 0, MaxToolBudget+10)
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "description for "+name))
	}
	// Intentionally add a duplicate core tool in the candidate zone.
	tools = append(tools, makeToolDef("bash", "duplicate bash"))
	// Fill to exceed budget.
	for i := len(tools); i < MaxToolBudget+5; i++ {
		tools = append(tools, makeToolDef(
			"filler_"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"filler tool",
		))
	}

	result := router.Route("运行一个脚本", tools)

	seen := make(map[string]int, len(result))
	for _, td := range result {
		name := ExtractToolName(td)
		seen[name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("duplicate tool name %q appears %d times", name, count)
		}
	}
}

// alwaysMatchRecommender is a test recommender that always returns a match.
type alwaysMatchRecommender struct{}

func (r *alwaysMatchRecommender) GetRecommendations() []SkillRecommendation {
	return []SkillRecommendation{
		{Name: "PDF转换", Description: "PDF转换工具，支持多种格式"},
	}
}
