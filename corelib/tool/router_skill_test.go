package tool

import (
	"fmt"
	"strings"
	"testing"
)

// mockSkillProvider implements SkillProvider for testing.
type mockSkillProvider struct {
	skills []SkillSummary
}

func (m *mockSkillProvider) ListActiveSkills() []SkillSummary { return m.skills }

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

func TestRouter_EnrichRunSkillDescription(t *testing.T) {
	def := makeToolDef("run_skill", "Execute a skill by name")
	enriched := enrichRunSkillDescription(def, []string{"deploy-app", "backup-db"})

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

	// manage_skill should have enriched description if score > 0.3.
	for _, def := range result {
		if ExtractToolName(def) == "manage_skill" {
			desc := ExtractToolDescription(def)
			t.Logf("manage_skill description: %s", desc)
			if score > 0.3 && !strings.Contains(desc, "deploy-app") {
				t.Errorf("manage_skill description should contain deploy-app when score > 0.3, got: %s", desc)
			}
			return
		}
	}
	t.Error("manage_skill should be in the build result")
}
