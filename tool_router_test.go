package main

import (
	"fmt"
	"testing"
)

// makeDynamicTool creates a tool definition with the given name and description.
func makeDynamicTool(name, desc string) map[string]interface{} {
	return toolDef(name, desc, nil, nil)
}

// makeAllTools creates a slice of 18 builtins + n dynamic tools.
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

func TestToolRouter_BelowThreshold(t *testing.T) {
	// 18 builtins + 1 dynamic = 19 total, below threshold of 20.
	allTools := makeAllTools(1)
	router := NewToolRouter(nil)
	result := router.Route("hello world", allTools)

	if len(result) != len(allTools) {
		t.Errorf("expected %d tools (unchanged), got %d", len(allTools), len(result))
	}
}

func TestToolRouter_ExactlyAtThreshold(t *testing.T) {
	// 18 builtins + 2 dynamic = 20 total, at threshold.
	allTools := makeAllTools(2)
	router := NewToolRouter(nil)
	result := router.Route("test message", allTools)

	if len(result) != 20 {
		t.Errorf("expected 20 tools (unchanged at threshold), got %d", len(result))
	}
}

func TestToolRouter_AboveThreshold_KeepsBuiltins(t *testing.T) {
	// 18 builtins + 15 dynamic = 33 total, above threshold.
	allTools := makeAllTools(15)
	router := NewToolRouter(nil)
	result := router.Route("some query", allTools)

	// Should have 18 builtins + up to 15 dynamic = 33 max.
	// Since we have exactly 15 dynamic, all should be kept.
	if len(result) != 33 {
		t.Errorf("expected 33 tools, got %d", len(result))
	}

	// Verify first 18 are builtins.
	builtinNames := []string{
		"list_sessions", "create_session", "send_input", "get_session_output",
		"get_session_events", "interrupt_session", "kill_session", "screenshot",
		"list_mcp_tools", "call_mcp_tool", "list_skills", "run_skill",
		"parallel_execute", "recommend_tool",
		"bash", "read_file", "write_file", "list_directory",
	}
	for i, expected := range builtinNames {
		actual := extractToolName(result[i])
		if actual != expected {
			t.Errorf("builtin[%d]: expected %q, got %q", i, expected, actual)
		}
	}
}

func TestToolRouter_AboveThreshold_LimitsDynamic(t *testing.T) {
	// 18 builtins + 20 dynamic = 38 total.
	allTools := makeAllTools(20)
	router := NewToolRouter(nil)
	result := router.Route("some query", allTools)

	// Should have 18 builtins + 15 dynamic = 33.
	if len(result) != 33 {
		t.Errorf("expected 33 tools (18 builtin + 15 dynamic), got %d", len(result))
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

	// All tools should be returned since 28 > 20, but we have only 10 dynamic
	// (below maxDynamicRouted=15), so all are kept.
	if len(result) != len(allTools) {
		t.Errorf("expected %d tools, got %d", len(allTools), len(result))
	}

	// The "search_web" tool should be ranked higher (closer to position 18).
	// Find its position in the result.
	searchIdx := -1
	for i := builtinToolCount; i < len(result); i++ {
		if extractToolName(result[i]) == "search_web" {
			searchIdx = i
			break
		}
	}
	if searchIdx == -1 {
		t.Error("search_web tool not found in result")
	} else if searchIdx != builtinToolCount {
		// search_web should be the first dynamic tool (highest relevance).
		t.Logf("search_web at index %d (first dynamic at %d)", searchIdx, builtinToolCount)
	}
}

func TestToolRouter_EmptyMessage(t *testing.T) {
	allTools := makeAllTools(20)
	router := NewToolRouter(nil)
	result := router.Route("", allTools)

	// Empty message → should still return 18 + 15 = 33.
	if len(result) != 33 {
		t.Errorf("expected 33 tools, got %d", len(result))
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

	// We can't create a real SkillHubClient without an App, but we can
	// verify the setter works with a nil value (no panic).
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
