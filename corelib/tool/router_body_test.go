package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// provisionRouterFixtureTools temporarily extends the compile-time legacy
// adapter provision catalog with reviewed entries for fixture tool names, and
// restores the catalog on cleanup. The catalog is owner-reviewed with no
// public test seeding by design; these tests live in-package, so they widen
// the package-private table for their duration. Without this the provision
// gate (legacyAdapterCandidateAllowed) filters every fixture name before
// ranking, leaving zero candidates for the reranker/routing path under test.
func provisionRouterFixtureTools(t *testing.T, names ...string) {
	t.Helper()
	old := legacyAdapterProvisions
	entries := make([]LegacyAdapterProvision, 0, len(old)+len(names))
	for _, e := range old {
		entries = append(entries, e)
	}
	for _, n := range names {
		if _, exists := old[n]; exists {
			// Already reviewed in the compile-time catalog (e.g. session_search);
			// no test entry needed.
			continue
		}
		entries = append(entries, LegacyAdapterProvision{
			ToolName:        n,
			Capability:      "test.fixture",
			Owner:           "router-test",
			AdapterContract: "router-test-v1",
			Effects:         []EffectClass{EffectReadOnly},
			DeleteAfter:     time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	legacyAdapterProvisions = mustLegacyAdapterProvisions(entries)
	t.Cleanup(func() { legacyAdapterProvisions = old })
}

// numberedFixtureNames generates prefix0..prefix(n-1) for provision seeding.
func numberedFixtureNames(prefix string, n int) []string {
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		names = append(names, fmt.Sprintf("%s%d", prefix, i))
	}
	return names
}

// mockReranker for unit tests with configurable behavior.
type mockReranker struct {
	callCount   int
	lastInput   []CandidateSummary
	returnNames []string
	returnErr   error
}

func (m *mockReranker) Rerank(userMessage string, candidates []CandidateSummary, topK int) ([]string, error) {
	m.callCount++
	m.lastInput = candidates
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.returnNames, nil
}

// buildTestToolSet creates a large tool set for reranker unit tests.
func buildTestToolSet(reg *Registry) []map[string]interface{} {
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		reg.Register(RegisteredTool{Name: name, Description: "core " + name, Category: CategoryBuiltin})
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	for i := 0; i < MaxToolBudget+5; i++ {
		name := fmt.Sprintf("test_tool_%d", i)
		desc := fmt.Sprintf("test tool %d for unit testing", i)
		reg.Register(RegisteredTool{Name: name, Description: desc, Category: CategoryNonCode})
		tools = append(tools, makeToolDef(name, desc))
	}
	return tools
}

func TestRouter_Reranker_NotConfigured(t *testing.T) {
	reg := NewRegistry()
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRegistry(reg)
	// No reranker set.

	tools := buildTestToolSet(reg)
	result := router.Route("test query", tools)

	if len(result) == 0 {
		t.Fatal("should return tools")
	}
	if len(result) > MaxToolBudget+2 {
		t.Fatalf("should respect budget, got %d", len(result))
	}
}

func TestRouter_Reranker_Error(t *testing.T) {
	reg := NewRegistry()
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRegistry(reg)

	mock := &mockReranker{returnErr: fmt.Errorf("reranker error")}
	router.SetReranker(mock)

	tools := buildTestToolSet(reg)
	provisionRouterFixtureTools(t, numberedFixtureNames("test_tool_", MaxToolBudget+5)...)
	result := router.Route("test query", tools)

	if mock.callCount == 0 {
		t.Fatal("reranker should have been called")
	}
	// Should still return results (fallback to fused scores).
	if len(result) == 0 {
		t.Fatal("should return tools even when reranker fails")
	}
	if len(result) > MaxToolBudget+2 {
		t.Fatalf("should respect budget, got %d", len(result))
	}
}

func TestRouter_Reranker_UsesRemainingCandidateSlots(t *testing.T) {
	reg := NewRegistry()
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRegistry(reg)
	mock := &mockReranker{returnNames: []string{"slot_tool_0"}}
	router.SetReranker(mock)

	remainingSlots := MaxToolBudget - len(CoreToolNames)
	if remainingSlots <= 0 {
		t.Fatalf("test requires core tool count below budget, core=%d budget=%d", len(CoreToolNames), MaxToolBudget)
	}

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		reg.Register(RegisteredTool{Name: name, Description: "core " + name, Category: CategoryBuiltin})
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	for i := 0; i < remainingSlots+1; i++ {
		name := fmt.Sprintf("slot_tool_%d", i)
		desc := fmt.Sprintf("slot test tool %d", i)
		reg.Register(RegisteredTool{Name: name, Description: desc, Category: CategoryNonCode})
		tools = append(tools, makeToolDef(name, desc))
	}
	provisionRouterFixtureTools(t, numberedFixtureNames("slot_tool_", remainingSlots+1)...)

	_ = router.Route("slot test query", tools)
	if mock.callCount == 0 {
		t.Fatalf("reranker should run when candidate count exceeds remaining slots")
	}
}

func TestRouter_Reranker_PartialResults(t *testing.T) {
	reg := NewRegistry()
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetRegistry(reg)

	// Return only 2 results (< 5).
	mock := &mockReranker{returnNames: []string{"test_tool_0", "test_tool_1"}}
	router.SetReranker(mock)

	tools := buildTestToolSet(reg)
	provisionRouterFixtureTools(t, numberedFixtureNames("test_tool_", MaxToolBudget+5)...)
	result := router.Route("test query", tools)

	if mock.callCount == 0 {
		t.Fatal("reranker should have been called")
	}
	// Should still fill up to budget from fused scores.
	if len(result) == 0 {
		t.Fatal("should return tools")
	}

	// The reranked tools should appear in the result.
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}
	for _, name := range mock.returnNames {
		if !resultNames[name] {
			t.Errorf("reranked tool %q should be in result", name)
		}
	}
}

func TestRouter_BodyAware_LogField(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tool_route.log")
	routeLogPathOverride.Store(logPath)
	defer routeLogPathOverride.Store("")
	SetLogDetailEnabled(true)
	defer SetLogDetailEnabled(false)
	writeRouteLog(
		"test message",
		10, 5, 5,
		true, // hybridActive
		true, // bodyAware
		[]string{"tool_a", "tool_b"},
		[]float64{0.9, 0.8},
		[]float64{0.03, 0},
		[]string{"tool_a"},
		[]string{"screenshot"}, // suppressedNames
		true,                   // browserPublishAffordance
		false,                  // explicitScreenshotRequest
		false,                  // semanticScreenshotRequest
		false,                  // screenshotRequest
		nil,                    // no reranker result
		0,                      // skillMatchScore
		nil,                    // matchedSkills
		nil,                    // matchedSkillCapabilities
		false,                  // skillCapabilityConstrained
		nil,                    // skillRequiredCapabilities
	)

	// Call again with bodyAware=false to verify both paths work.
	writeRouteLog(
		"test message 2",
		10, 5, 5,
		false, // hybridActive
		false, // bodyAware
		[]string{"tool_c"},
		[]float64{0.7},
		[]float64{-0.02},
		[]string{"tool_c"},
		nil,                      // suppressedNames
		false,                    // browserPublishAffordance
		true,                     // explicitScreenshotRequest
		false,                    // semanticScreenshotRequest
		true,                     // screenshotRequest
		[]string{"tool_c"},       // with reranker result
		0.5,                      // skillMatchScore
		[]string{"deploy-app"},   // matchedSkills
		[]string{"skill"},        // matchedSkillCapabilities
		true,                     // skillCapabilityConstrained
		[]string{"current_data"}, // skillRequiredCapabilities
	)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("cannot read log file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Body-aware: true") {
		t.Error("log should contain 'Body-aware: true'")
	}
	if !strings.Contains(content, "Body-aware: false") {
		t.Error("log should contain 'Body-aware: false'")
	}
	if !strings.Contains(content, "routing_hint +0.0300") || !strings.Contains(content, "routing_hint -0.0200") {
		t.Error("log should contain routing hint adjustments")
	}
	if !strings.Contains(content, "Execution affordances: browser_publish=true explicit_screenshot=false semantic_screenshot=false screenshot_requested=false") {
		t.Error("log should contain execution affordances")
	}
	if !strings.Contains(content, "Suppressed tools (1): [screenshot]") {
		t.Error("log should contain suppressed tools")
	}
	if !strings.Contains(content, "Skill capabilities: [skill]") {
		t.Error("log should contain skill capabilities")
	}
	if !strings.Contains(content, "Skill capability constraint: [current_data]") {
		t.Error("log should contain skill capability constraint")
	}
}

func TestWriteToolExposureLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tool_route.log")
	routeLogPathOverride.Store(logPath)
	defer routeLogPathOverride.Store("")
	SetLogDetailEnabled(true)
	defer SetLogDetailEnabled(false)

	WriteToolExposureLog(
		"execution_profile",
		"live lookup",
		"req-1",
		"user-1",
		"light",
		"live_data",
		12,
		[]string{"manage_skill", "web_search"},
	)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("cannot read log file: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"=== Tool Exposure",
		"Stage: execution_profile",
		"Request: req-1 | User: user-1",
		"Profile: layer=light task=live_data",
		"Tools: before=12 after=2",
		"  - manage_skill",
		"  - web_search",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("exposure log missing %q:\n%s", want, content)
		}
	}
}
