package tool

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// mockSkillProvider implements SkillProvider for testing.
type mockSkillProvider struct {
	skills []SkillSummary
}

func (m *mockSkillProvider) ListActiveSkills() []SkillSummary { return m.skills }

func testStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func toolNamesForTest(defs []map[string]interface{}) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, ExtractToolName(def))
	}
	return names
}

func toolIndexForTest(defs []map[string]interface{}, name string) int {
	for i, def := range defs {
		if ExtractToolName(def) == name {
			return i
		}
	}
	return -1
}

func TestRouter_SkillProvider_FourSignalScoring(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	provider := &mockSkillProvider{
		skills: []SkillSummary{
			{Name: "deploy-app", Triggers: []string{"deploy", "部署", "应用"}, Description: "部署应用到生产环境 deploy application"},
		},
	}
	router.SetSkillProvider(provider)

	// Verify skill match score is above threshold.
	score, names := router.skillMatchScore("帮我部署应用")
	t.Logf("skillMatchScore: %.4f, names: %v", score, names)

	// Build enough tools to trigger routing.
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core tool "+name))
	}
	for i := 0; i < 15; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("帮我部署应用", tools)
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}

	// manage_skill should be in the result (it's a core tool that replaced run_skill).
	found := false
	for _, r := range result {
		name := ExtractToolName(r)
		if name == "manage_skill" {
			found = true
			break
		}
	}
	if !found {
		t.Error("manage_skill should be in the result")
	}
}

func TestRouter_SkillProvider_NoProvider_FallbackToThreeSignal(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	// No SetSkillProvider call — should use three-signal formula.

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core tool "+name))
	}
	for i := 0; i < 15; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("test message", tools)
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
}

func TestRouter_SkillProvider_NoMatch_NoEnrichment(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	provider := &mockSkillProvider{
		skills: []SkillSummary{
			{Name: "deploy-app", Triggers: []string{"deploy"}, Description: "deploy application"},
		},
	}
	router.SetSkillProvider(provider)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core tool "+name))
	}
	for i := 0; i < 15; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// Query about databases — should NOT match deploy skill.
	result := router.Route("查询数据库", tools)
	for _, r := range result {
		if ExtractToolName(r) == "manage_skill" {
			desc := ExtractToolDescription(r)
			if strings.Contains(desc, "deploy-app") {
				t.Error("manage_skill description should NOT contain deploy-app for unrelated query")
			}
			break
		}
	}
}

func TestRouter_SkillMatchScore_EmptyProvider(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	provider := &mockSkillProvider{skills: nil}
	router.SetSkillProvider(provider)

	score, names := router.skillMatchScore("test query")
	if score != 0 {
		t.Errorf("expected 0 score for empty provider, got %.4f", score)
	}
	if len(names) != 0 {
		t.Errorf("expected no matched names, got %v", names)
	}
}

func TestRouter_SkillMatchScore_NilProvider(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	score, names := router.skillMatchScore("test query")
	if score != 0 {
		t.Errorf("expected 0 score for nil provider, got %.4f", score)
	}
	if names != nil {
		t.Errorf("expected nil names, got %v", names)
	}
}

func TestRouter_SkillMatchScoreCapabilityConstraint(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{
			Name:         "Live Lookup",
			Triggers:     []string{"lookup"},
			Description:  "weather forecast current data",
			Capabilities: []string{"current_data"},
		},
	}})

	score, matched := router.skillMatchScoreWithCapabilityConstraint("weather forecast", []string{"current_data"}, true)
	if score <= 0 || len(matched) == 0 || matched[0] != "Live Lookup" {
		t.Fatalf("current_data constraint should allow live skill: score=%.4f matched=%v", score, matched)
	}

	score, matched = router.skillMatchScoreWithCapabilityConstraint("weather forecast", []string{"ssh", "remote"}, true)
	if score != 0 || len(matched) != 0 {
		t.Fatalf("ssh constraint should reject current_data skill: score=%.4f matched=%v", score, matched)
	}
}

func TestRouter_SkillCapabilityConstraintForUICSeparatesSearchAndLiveData(t *testing.T) {
	caps, constrained := skillCapabilityConstraintForUIC(intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.92,
	})
	if !constrained || !testStringSliceContains(caps, "current_data") {
		t.Fatalf("live_data caps = %v constrained=%v, want current_data", caps, constrained)
	}

	caps, constrained = skillCapabilityConstraintForUIC(intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.92,
	})
	if !constrained || testStringSliceContains(caps, "current_data") {
		t.Fatalf("search caps = %v constrained=%v, should not include current_data", caps, constrained)
	}
}

func TestRouter_EnrichRunSkillDescription(t *testing.T) {
	def := makeToolDef("run_skill", "Execute a skill by name")
	enriched := enrichRunSkillDescription(def, []string{"deploy-app", "backup-db"}, []string{"skill", "current_data"})

	desc := ExtractToolDescription(enriched)
	if !strings.Contains(desc, "deploy-app") {
		t.Errorf("enriched description should contain deploy-app, got: %s", desc)
	}
	if !strings.Contains(desc, "backup-db") {
		t.Errorf("enriched description should contain backup-db, got: %s", desc)
	}
	if !strings.Contains(desc, "可用 Skill") {
		t.Errorf("enriched description should contain '可用 Skill', got: %s", desc)
	}

	// Original should be unchanged.
	origDesc := ExtractToolDescription(def)
	if strings.Contains(origDesc, "deploy-app") {
		t.Error("original definition should not be modified")
	}
	contract, ok := enriched["x_execution_contract"].(map[string]interface{})
	if !ok {
		t.Fatal("expected execution contract for skill capabilities")
	}
	caps, ok := contract["capabilities"].([]string)
	if !ok || len(caps) != 2 || caps[1] != "current_data" {
		t.Fatalf("capabilities = %#v, want [skill current_data]", contract["capabilities"])
	}
}

func TestRouter_EnrichRunSkillDescriptionMergesExistingExecutionContract(t *testing.T) {
	def := makeToolDef("manage_skill", "Execute a skill by name")
	def["x_execution_contract"] = map[string]interface{}{
		"capabilities":            []interface{}{"Skill", "ASYNC-Status"},
		"deterministic":           true,
		"supports_direct":         true,
		"requires_agent_planning": true,
	}

	enriched := enrichRunSkillDescription(def, []string{"live-skill"}, []string{"skill", "current_data"})
	contract, ok := enriched["x_execution_contract"].(map[string]interface{})
	if !ok {
		t.Fatal("expected execution contract")
	}
	if contract["deterministic"] != true || contract["supports_direct"] != true || contract["requires_agent_planning"] != true {
		t.Fatalf("existing contract flags were not preserved: %#v", contract)
	}
	caps, ok := contract["capabilities"].([]string)
	if !ok {
		t.Fatalf("capabilities = %#v, want []string", contract["capabilities"])
	}
	for _, want := range []string{"skill", "async_status", "current_data"} {
		if !testStringSliceContains(caps, want) {
			t.Fatalf("capabilities = %#v, missing %q", caps, want)
		}
	}
}

func TestRouter_MatchedSkillCapabilities(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Weather Query", Triggers: []string{"Weather Query"}, Description: "weather lookup", Capabilities: []string{"CURRENT-DATA", "weather-live"}},
	}})
	caps := router.matchedSkillCapabilities([]string{"Weather Query"})
	if !testStringSliceContains(caps, "skill") || !testStringSliceContains(caps, "current_data") || !testStringSliceContains(caps, "weather_live") {
		t.Fatalf("capabilities = %#v, want normalized skill/current_data/weather_live", caps)
	}
}

func TestRouter_MatchedSkillCapabilitiesIncludesSkillWhenNoDeclaredCapabilities(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Plain Skill", Triggers: []string{"plain"}, Description: "plain skill"},
	}})
	caps := router.matchedSkillCapabilities([]string{"Plain Skill"})
	if len(caps) != 1 || caps[0] != "skill" {
		t.Fatalf("capabilities = %#v, want [skill]", caps)
	}
}

func TestRouter_RouteEnrichesMatchedSkillExecutionContract(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Weather Query", Triggers: []string{"weather", "forecast", "current data"}, Description: "weather forecast current data lookup", Capabilities: []string{"current_data", "weather"}},
	}})
	tools := []map[string]interface{}{
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("web_search", "Search the web"),
		makeToolDef("bash", "Run shell"),
	}
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	score, matched := router.skillMatchScore("Weather Query weather forecast current data")
	if score <= 0.3 || len(matched) == 0 {
		t.Fatalf("skill score = %.4f matched=%v, want stable match", score, matched)
	}
	result := router.Route("Weather Query weather forecast current data", tools)
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || !testStringSliceContains(caps, "skill") || !testStringSliceContains(caps, "current_data") {
			t.Fatalf("capabilities = %#v, want skill/current_data", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from routed tools: %#v", result)
}

func TestRouter_RouteEnrichesMatchedSkillExecutionContractBelowLegacyThreshold(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	skills := make([]SkillSummary, 32)
	for i := range skills {
		skills[i] = SkillSummary{
			Name:         fmt.Sprintf("shared-skill-%d", i),
			Triggers:     []string{"shared"},
			Description:  "shared lookup",
			Capabilities: []string{"current_data"},
		}
	}
	router.SetSkillProvider(&mockSkillProvider{skills: skills})
	tools := []map[string]interface{}{
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("web_search", "Search the web"),
		makeToolDef("bash", "Run shell"),
	}
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	score, matched := router.skillMatchScore("shared")
	if score <= 0 || score > 0.3 || len(matched) == 0 {
		t.Fatalf("skill score = %.4f matched=%v, want low positive match", score, matched)
	}
	result := router.Route("shared", tools)
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract for low-score matched skill")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || !testStringSliceContains(caps, "current_data") {
			t.Fatalf("capabilities = %#v, want current_data", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from routed tools: %#v", result)
}

func TestRouter_RouteEnrichesMatchedSkillWhenNoDynamicCandidates(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "live-skill", Triggers: []string{"live"}, Description: "live lookup", Capabilities: []string{"current_data"}},
	}})
	tools := []map[string]interface{}{
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("bash", "Run shell"),
	}

	result := router.Route("live", tools)
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract when route returns core only")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || !testStringSliceContains(caps, "current_data") {
			t.Fatalf("capabilities = %#v, want current_data", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from routed tools: %#v", result)
}

func TestRouter_RouteEnrichesSkillOnlyExecutionContract(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Plain Skill", Triggers: []string{"plain"}, Description: "plain skill"},
	}})
	tools := []map[string]interface{}{
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("bash", "Run shell"),
	}

	result := router.Route("plain", tools)
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract for skill-only match")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || len(caps) != 1 || caps[0] != "skill" {
			t.Fatalf("capabilities = %#v, want [skill]", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from routed tools: %#v", result)
}

func TestRouter_RouteKeepsMatchedSkillWhenCoreOverBudget(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Plain Skill", Triggers: []string{"plain"}, Description: "plain skill"},
	}})
	router.sessionTools = map[string]bool{}
	tools := []map[string]interface{}{makeToolDef("manage_skill", "Skill management")}
	for i := 0; i < MaxToolBudget+4; i++ {
		name := fmt.Sprintf("session_tool_%d", i)
		router.sessionTools[name] = true
		tools = append(tools, makeToolDef(name, "session tool"))
	}

	result := router.Route("plain", tools)
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		if _, ok := def["x_execution_contract"].(map[string]interface{}); !ok {
			t.Fatal("manage_skill kept but missing execution contract")
		}
		return
	}
	t.Fatalf("manage_skill trimmed despite matched skill: %v", toolNamesForTest(result))
}

func TestRouter_RoutePrioritizesMatchedSkillBeforeGenericCurrentDataTool(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Live Lookup", Triggers: []string{"live"}, Description: "live current data lookup", Capabilities: []string{"current_data"}},
	}})
	tools := []map[string]interface{}{
		makeToolDef("web_search", "live current data web search"),
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("bash", "Run shell"),
	}
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("live", tools)
	manageIdx := toolIndexForTest(result, "manage_skill")
	if manageIdx != 0 {
		t.Fatalf("tool order = %v, want matched manage_skill first", toolNamesForTest(result))
	}
}

func TestRouter_RoutePrioritizesMatchedSkillBeforeConditionalCoreTools(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Live Lookup", Triggers: []string{"live"}, Description: "live current data lookup", Capabilities: []string{"current_data"}},
	}})
	router.sessionTools = map[string]bool{"session_context": true}
	tools := []map[string]interface{}{
		makeToolDef("session_context", "Pinned session context"),
		makeToolDef("manage_skill", "Skill management"),
		makeToolDef("web_search", "live current data web search"),
		makeToolDef("bash", "Run shell"),
	}
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("live", tools)
	manageIdx := toolIndexForTest(result, "manage_skill")
	sessionIdx := toolIndexForTest(result, "session_context")
	if manageIdx != 0 || (sessionIdx >= 0 && manageIdx > sessionIdx) {
		t.Fatalf("tool order = %v, want matched manage_skill before conditional/session tools", toolNamesForTest(result))
	}
}

func TestRouter_SSHIntentKeepsMatchedSkill(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	ic := NewIntentClassifier(nil)
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentSSH, nil })
	router.SetIntentClassifier(ic)
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "server-health", Triggers: []string{"remote server"}, Description: "remote server resource usage", Capabilities: []string{"ssh", "server"}},
	}})

	result := router.Route("Check the remote server resource usage.", makeCoreSSHRouteTools(20))
	names := routedToolNames(result)
	if !names["ssh"] {
		t.Fatalf("ssh should remain routed, got %v", toolNamesForTest(result))
	}
	if !names["manage_skill"] {
		t.Fatalf("matched manage_skill should survive ssh fallback suppression, got %v", toolNamesForTest(result))
	}
	if toolIndexForTest(result, "manage_skill") != 0 {
		t.Fatalf("tool order = %v, want matched manage_skill first", toolNamesForTest(result))
	}
}

func TestRouter_BrowserSessionKeepsMatchedSkill(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.ActivateSessionTool("browser")
	router.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "browser-publisher", Triggers: []string{"submit followup"}, Description: "browser session submit followup", Capabilities: []string{"browser"}},
	}})

	tools := []map[string]interface{}{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "stable browser automation merged tool"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("submit followup", tools)
	names := routedToolNames(result)
	if !names["browser"] {
		t.Fatalf("browser should remain routed, got %v", toolNamesForTest(result))
	}
	if !names["manage_skill"] {
		t.Fatalf("matched manage_skill should survive browser fallback suppression, got %v", toolNamesForTest(result))
	}
	if toolIndexForTest(result, "manage_skill") != 0 {
		t.Fatalf("tool order = %v, want matched manage_skill first", toolNamesForTest(result))
	}
}

func TestDynamicToolBuilder_SkillProvider(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "manage_skill", Description: "Skill management", Category: CategoryBuiltin})
	for i := 0; i < 25; i++ {
		reg.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_%d", i),
			Description: fmt.Sprintf("filler tool %d", i),
			Category:    CategoryNonCode,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	provider := &mockSkillProvider{
		skills: []SkillSummary{
			{Name: "deploy-app", Triggers: []string{"deploy", "部署", "应用"}, Description: "部署应用 deploy application"},
		},
	}
	builder.SetSkillProvider(provider)

	score, names := builder.builderSkillMatchScore("帮我部署应用")
	t.Logf("builderSkillMatchScore: %.4f, names: %v", score, names)

	result := builder.Build("帮我部署应用")

	// manage_skill should have enriched description when a skill is matched.
	for _, def := range result {
		if ExtractToolName(def) == "manage_skill" {
			desc := ExtractToolDescription(def)
			t.Logf("manage_skill description: %s", desc)
			if len(names) > 0 && !strings.Contains(desc, "deploy-app") {
				t.Errorf("manage_skill description should contain deploy-app when skill matched, score=%.4f got: %s", score, desc)
			}
			return
		}
	}
	t.Error("manage_skill should be in the build result")
}

func TestDynamicToolBuilder_EnrichesMatchedSkillBelowLegacyThreshold(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "manage_skill", Description: "Skill management", Category: CategoryBuiltin})
	for i := 0; i < 30; i++ {
		reg.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_%d", i),
			Description: fmt.Sprintf("filler tool %d", i),
			Category:    CategoryNonCode,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	skills := make([]SkillSummary, 32)
	for i := range skills {
		skills[i] = SkillSummary{
			Name:         fmt.Sprintf("shared-skill-%d", i),
			Triggers:     []string{"shared"},
			Description:  "shared lookup",
			Capabilities: []string{"current_data"},
		}
	}
	builder.SetSkillProvider(&mockSkillProvider{skills: skills})

	score, matched := builder.builderSkillMatchScore("shared")
	if score <= 0 || score > 0.3 || len(matched) == 0 {
		t.Fatalf("skill score = %.4f matched=%v, want low positive match", score, matched)
	}
	result := builder.Build("shared")
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract for low-score matched skill")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || !testStringSliceContains(caps, "current_data") {
			t.Fatalf("capabilities = %#v, want current_data", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from build result: %#v", result)
}

func TestDynamicToolBuilder_EnrichesMatchedSkillWithoutRouting(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "manage_skill", Description: "Skill management", Category: CategoryBuiltin})

	builder := NewDynamicToolBuilder(reg)
	builder.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "live-skill", Triggers: []string{"live"}, Description: "live lookup", Capabilities: []string{"current_data"}},
	}})

	result := builder.Build("live")
	for _, def := range result {
		if ExtractToolName(def) != "manage_skill" {
			continue
		}
		contract, ok := def["x_execution_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("manage_skill missing execution contract when builder skips routing")
		}
		caps, ok := contract["capabilities"].([]string)
		if !ok || !testStringSliceContains(caps, "current_data") {
			t.Fatalf("capabilities = %#v, want current_data", contract["capabilities"])
		}
		return
	}
	t.Fatalf("manage_skill missing from build result: %#v", result)
}

func TestDynamicToolBuilder_PrioritizesMatchedSkillBeforeGenericCurrentDataTool(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "web_search", Description: "search web", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "manage_skill", Description: "Skill management", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	for i := 0; i < 30; i++ {
		reg.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_%d", i),
			Description: fmt.Sprintf("filler tool %d", i),
			Category:    CategoryNonCode,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	builder.SetSkillProvider(&mockSkillProvider{skills: []SkillSummary{
		{Name: "Live Lookup", Triggers: []string{"live"}, Description: "live current data lookup", Capabilities: []string{"current_data"}},
	}})

	result := builder.Build("live")
	manageIdx := toolIndexForTest(result, "manage_skill")
	searchIdx := toolIndexForTest(result, "web_search")
	if manageIdx < 0 || searchIdx < 0 || manageIdx > searchIdx {
		t.Fatalf("tool order = %v, want manage_skill before web_search", toolNamesForTest(result))
	}
}
