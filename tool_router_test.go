package main

import (
	"fmt"
	"testing"
)

// makeDynamicTool creates a tool definition with the given name and description.
func makeDynamicTool(name, desc string) map[string]interface{} {
	return toolDef(name, desc, nil, nil)
}

// makeAllTools creates a slice of 20 builtins + n dynamic tools.
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
	// 20 builtins + 5 dynamic = 25 total, below maxToolBudget of 30.
	allTools := makeAllTools(5)
	router := NewToolRouter(nil)
	result := router.Route("hello world", allTools)

	if len(result) != len(allTools) {
		t.Errorf("expected %d tools (unchanged), got %d", len(allTools), len(result))
	}
}

func TestToolRouter_ExactlyAtBudget(t *testing.T) {
	// 20 builtins + 10 dynamic = 30 total, at maxToolBudget.
	allTools := makeAllTools(10)
	router := NewToolRouter(nil)
	result := router.Route("test message", allTools)

	if len(result) != 30 {
		t.Errorf("expected 30 tools (unchanged at budget), got %d", len(result))
	}
}

func TestToolRouter_AboveBudget_KeepsCoreTools(t *testing.T) {
	// 20 builtins + 20 dynamic = 40 total, above maxToolBudget.
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
	// 20 builtins + 25 dynamic = 45 total.
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
	dynamic := []map[string]interface{}{
		makeDynamicTool("search_web", "Search the web for information"),
		makeDynamicTool("custom_read", "Read a custom resource"),
		makeDynamicTool("custom_write", "Write a custom resource"),
		makeDynamicTool("run_tests", "Run unit tests for the project"),
		makeDynamicTool("deploy_app", "Deploy the application to production"),
		makeDynamicTool("lint_code", "Lint source code for style issues"),
		makeDynamicTool("format_code", "Format source code"),
		makeDynamicTool("git_commit", "Commit changes to git repository"),
		makeDynamicTool("docker_build", "Build a Docker container image"),
		makeDynamicTool("database_query", "Query the database"),
	}
	allTools := append(builtins, dynamic...)

	router := NewToolRouter(nil)
	result := router.Route("search the web for golang tutorials", allTools)

	// 30 total, at budget — all should be returned.
	if len(result) != len(allTools) {
		t.Errorf("expected %d tools, got %d", len(allTools), len(result))
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
		toolDef("save_memory", "保存记忆", nil, nil),
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
// Tokenization tests
// ---------------------------------------------------------------------------

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"Hello, World!", []string{"hello", "world"}},
		{"search_web tool", []string{"search", "web", "tool"}},
		{"TF-IDF similarity", []string{"tf", "idf", "similarity"}},
		{"", nil},
		{"a b c", nil}, // single chars filtered out
		{"  spaces  everywhere  ", []string{"spaces", "everywhere"}},
	}

	for _, tt := range tests {
		result := tokenize(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("tokenize(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, tok := range result {
			if tok != tt.expected[i] {
				t.Errorf("tokenize(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], tok)
			}
		}
	}
}

func TestTokenize_CJK(t *testing.T) {
	// CJK characters should be split into individual tokens.
	result := tokenize("列出配置")
	if len(result) == 0 {
		t.Fatal("expected CJK tokens, got none")
	}
	// Each Chinese character should be a separate token.
	found := make(map[string]bool)
	for _, tok := range result {
		found[tok] = true
	}
	if !found["列"] || !found["出"] || !found["配"] || !found["置"] {
		t.Errorf("expected individual CJK chars as tokens, got %v", result)
	}

	// Mixed CJK + ASCII should produce both types of tokens.
	result2 := tokenize("修改config配置")
	hasASCII := false
	hasCJK := false
	for _, tok := range result2 {
		if tok == "config" {
			hasASCII = true
		}
		if tok == "修" || tok == "改" || tok == "配" || tok == "置" {
			hasCJK = true
		}
	}
	if !hasASCII || !hasCJK {
		t.Errorf("mixed CJK+ASCII tokenize failed: %v", result2)
	}
}

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

func TestToolTokens(t *testing.T) {
	def := toolDef("search_web", "Search the web for information", nil, nil)
	tokens := toolTokens(def)

	// Should contain tokens from both name and description.
	found := make(map[string]bool)
	for _, tok := range tokens {
		found[tok] = true
	}
	for _, expected := range []string{"search", "web", "the", "for", "information"} {
		if !found[expected] {
			t.Errorf("expected token %q in toolTokens result", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// TF-IDF tests
// ---------------------------------------------------------------------------

func TestTermFrequency(t *testing.T) {
	tokens := []string{"hello", "world", "hello"}
	tf := termFrequency(tokens)

	if tf["hello"] != 2.0/3.0 {
		t.Errorf("expected tf[hello]=%.4f, got %.4f", 2.0/3.0, tf["hello"])
	}
	if tf["world"] != 1.0/3.0 {
		t.Errorf("expected tf[world]=%.4f, got %.4f", 1.0/3.0, tf["world"])
	}
}

func TestTermFrequency_Empty(t *testing.T) {
	tf := termFrequency(nil)
	if len(tf) != 0 {
		t.Errorf("expected empty tf for nil input, got %v", tf)
	}
}

func TestTfidfSimilarity_IdenticalDocs(t *testing.T) {
	// Use a larger corpus so IDF values are non-zero for the target terms.
	docs := [][]string{
		{"hello", "world"},
		{"foo", "bar"},
		{"baz", "qux"},
		{"alpha", "beta"},
	}
	idf := computeIDF(docs)

	// A document compared to itself should have similarity ~1.0.
	sim := tfidfSimilarity([]string{"hello", "world"}, []string{"hello", "world"}, idf)
	if sim < 0.99 {
		t.Errorf("expected similarity ~1.0 for identical docs, got %.4f", sim)
	}
}

func TestTfidfSimilarity_DisjointDocs(t *testing.T) {
	docs := [][]string{
		{"hello", "world"},
		{"foo", "bar"},
		{"baz", "qux"},
		{"alpha", "beta"},
	}
	idf := computeIDF(docs)

	sim := tfidfSimilarity([]string{"hello", "world"}, []string{"foo", "bar"}, idf)
	if sim != 0 {
		t.Errorf("expected similarity 0 for disjoint docs, got %.4f", sim)
	}
}

func TestTfidfSimilarity_EmptyInputs(t *testing.T) {
	idf := map[string]float64{"hello": 1.0}

	if sim := tfidfSimilarity(nil, []string{"hello"}, idf); sim != 0 {
		t.Errorf("expected 0 for nil query, got %.4f", sim)
	}
	if sim := tfidfSimilarity([]string{"hello"}, nil, idf); sim != 0 {
		t.Errorf("expected 0 for nil doc, got %.4f", sim)
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
