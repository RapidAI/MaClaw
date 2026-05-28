package tool

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestRouter_Route_NoDuplicateToolNames verifies the invariant:
// Route() output never contains duplicate tool names.
//
// Regression test for DeepSeek API error "Tool names must be unique" (HTTP 400).
//
// Root cause: search_and_install_skill entered the result twice — once from
// routed tools and once from the matchRecommendations hint. The fix enforces
// uniqueness against the final selected result, not the whole input list.
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

func TestRouter_Route_RecommendationHintCanAppendUnselectedAvailableTool(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRecommender(&alwaysMatchRecommender{})

	tools := make([]map[string]interface{}, 0, MaxToolBudget*2)
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "description for "+name))
	}
	for i := 0; i < MaxToolBudget; i++ {
		tools = append(tools, makeToolDef(
			"pdf_candidate_"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"PDF conversion document workflow",
		))
	}
	tools = append(tools, makeToolDef("search_and_install_skill", "hub installer without pdf keywords"))

	result := router.Route("PDF", tools)
	seen := make(map[string]int, len(result))
	for _, td := range result {
		seen[ExtractToolName(td)]++
	}
	if seen["search_and_install_skill"] != 1 {
		t.Fatalf("search_and_install_skill should be appended once as recommendation hint, got %d in %#v", seen["search_and_install_skill"], seen)
	}
}

func TestRouter_Route_RecommendationHintRespectsSuppression(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRecommender(&alwaysMatchRecommender{})
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentSSH, nil })
	router.SetIntentClassifier(ic)

	tools := makeCoreSSHRouteTools(20)
	tools = append(tools, makeToolDef("search_and_install_skill", "hub installer PDF conversion"))

	result := router.Route("SSH into the PDF server and check usage", tools)
	seen := make(map[string]int, len(result))
	for _, td := range result {
		seen[ExtractToolName(td)]++
	}
	if seen["search_and_install_skill"] != 0 {
		t.Fatalf("search_and_install_skill hint should respect SSH suppression, got %d in %#v", seen["search_and_install_skill"], seen)
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
