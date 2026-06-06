package tool

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	uicintent "github.com/RapidAI/CodeClaw/corelib/intent"
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

func TestRouter_Route_RecommendationHintBlockedForQueryIntent(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRecommender(&alwaysMatchRecommender{})
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentQuery, nil })
	router.SetIntentClassifier(ic)

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
	tools = append(tools, makeToolDef("search_and_install_skill", "hub installer PDF conversion"))

	result := router.Route("What is PDF conversion?", tools)
	seen := make(map[string]int, len(result))
	for _, td := range result {
		seen[ExtractToolName(td)]++
	}
	if seen["search_and_install_skill"] != 0 {
		t.Fatalf("query intent should not receive search_and_install_skill hint, got %d in %#v", seen["search_and_install_skill"], seen)
	}
}

func TestRouter_Route_SearchInstallBlockedForUICMaintenanceIntent(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRecommender(&alwaysMatchRecommender{})
	router.SetUnifiedClassifier(uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"maintenance","score":0.95},{"skill":"coding","score":0.20}]}`, nil
		},
	}))

	tools := make([]map[string]interface{}, 0, MaxToolBudget*2)
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "description for "+name))
	}
	for i := 0; i < MaxToolBudget; i++ {
		tools = append(tools, makeToolDef(
			"maintenance_candidate_"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"maintenance workflow",
		))
	}
	tools = append(tools, makeToolDef("search_and_install_skill", "hub installer maintenance helper"))

	result := router.Route("review/fix/optimize", tools)
	for _, td := range result {
		if ExtractToolName(td) == "search_and_install_skill" {
			t.Fatalf("UIC maintenance intent should not receive search_and_install_skill, got %#v", result)
		}
	}
}

func TestSkillInstallEligibilityBlocksLookupAndInfrastructureIntents(t *testing.T) {
	for _, tc := range []struct {
		intent string
		want   bool
	}{
		{intent: IntentQuery, want: false},
		{intent: IntentSSH, want: false},
		{intent: IntentBrowser, want: false},
		{intent: IntentShortCommand, want: false},
		{intent: IntentUnknown, want: false},
		{intent: IntentCoding, want: true},
		{intent: IntentContent, want: true},
	} {
		if got := intentClassifierSkillInstallEligible(IntentResult{Intent: tc.intent}); got != tc.want {
			t.Fatalf("intentClassifierSkillInstallEligible(%q) = %v, want %v", tc.intent, got, tc.want)
		}
	}

	for _, tc := range []struct {
		label uicintent.IntentLabel
		want  bool
	}{
		{label: uicintent.LabelSearch, want: false},
		{label: uicintent.LabelLiveData, want: false},
		{label: uicintent.LabelSSH, want: false},
		{label: uicintent.LabelBrowser, want: false},
		{label: uicintent.LabelNonCoding, want: false},
		{label: uicintent.LabelUnknown, want: false},
		{label: uicintent.LabelMaintenance, want: false},
		{label: uicintent.LabelBugFix, want: false},
		{label: uicintent.LabelCoding, want: true},
		{label: uicintent.LabelOffice, want: true},
	} {
		if got := uicSkillInstallEligible(uicintent.ClassificationResult{Primary: tc.label}); got != tc.want {
			t.Fatalf("uicSkillInstallEligible(%q) = %v, want %v", tc.label, got, tc.want)
		}
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
