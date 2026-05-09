package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestSkillDocInject_KnowledgeSkillInlined verifies that knowledge-type skills
// with inline Content are injected into the system prompt when triggers match.
func TestSkillDocInject_KnowledgeSkillInlined(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:     "git-guide",
			Type:     "knowledge",
			Status:   "active",
			Content:  "# Git Guide\nUse `git commit -m` to commit changes.",
			Triggers: []string{"git", "commit"},
		},
	}

	result := runInjection(t, skills, "帮我用 git commit 提交代码", 0)

	assertContains(t, result, "### Skill: git-guide")
	assertContains(t, result, "Git Guide")
}

// TestSkillDocInject_ExecutableWithSKILLMD verifies that executable skills
// with a real SKILL.md file on disk are injected when triggers match.
func TestSkillDocInject_ExecutableWithSKILLMD(t *testing.T) {
	// Create a temp skill directory with a SKILL.md file.
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "drawio-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docContent := "# DrawIO Skill\n\nGenerate XML first, then call run.js to convert."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := []NLSkillDefinition{
		{
			Name:     "drawio-skill",
			Type:     "executable",
			Status:   "active",
			Triggers: []string{"drawio", "diagram"},
			SkillDir: skillDir,
		},
	}

	result := runInjection(t, skills, "用 drawio-skill 画一个架构图", 0)

	assertContains(t, result, "### Skill: drawio-skill")
	assertContains(t, result, "Generate XML first")
}

// TestSkillDocInject_ExecutableNoSKILLMD verifies that executable skills
// without SKILL.md are not injected (zero overhead).
func TestSkillDocInject_ExecutableNoSKILLMD(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:     "simple-tool",
			Type:     "executable",
			Status:   "active",
			Triggers: []string{"simple"},
			// No Content, no SkillDir → no injection.
		},
	}

	result := runInjection(t, skills, "run simple tool", 0)

	assertNotContains(t, result, "simple-tool")
}

// TestSkillDocInject_DisabledSkillNotInjected verifies that disabled skills
// are never injected regardless of trigger match.
func TestSkillDocInject_DisabledSkillNotInjected(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:     "disabled-skill",
			Type:     "knowledge",
			Status:   "disabled",
			Content:  "# Should not appear",
			Triggers: []string{"disabled"},
		},
	}

	result := runInjection(t, skills, "test disabled skill", 0)

	assertNotContains(t, result, "disabled-skill")
}

// TestSkillDocInject_NoTriggerMatch verifies that skills whose triggers
// don't match the user message are not injected.
func TestSkillDocInject_NoTriggerMatch(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:     "weather-skill",
			Type:     "knowledge",
			Status:   "active",
			Content:  "# Weather API\nQuery weather data.",
			Triggers: []string{"weather", "天气"},
		},
	}

	result := runInjection(t, skills, "帮我写一个贪吃蛇游戏", 0)

	assertNotContains(t, result, "weather-skill")
}

// TestSkillDocInject_TokenBudgetTruncation verifies that content is truncated
// when it exceeds the token budget.
func TestSkillDocInject_TokenBudgetTruncation(t *testing.T) {
	longContent := strings.Repeat("这是一段很长的文档内容。", 500) // ~5000 runes
	skills := []NLSkillDefinition{
		{
			Name:     "long-skill",
			Type:     "knowledge",
			Status:   "active",
			Content:  longContent,
			Triggers: []string{"long"},
		},
	}

	result := runInjection(t, skills, "test long skill", 100) // 100 token budget ≈ 300 runes

	assertContains(t, result, "### Skill: long-skill")
	// The injected content should be much shorter than the original.
	if len([]rune(result)) > 600 {
		t.Errorf("content should be truncated to ~300 runes, got %d runes total", len([]rune(result)))
	}
}

func TestBuildParamSchemaForSkillCompletesPartialSchema(t *testing.T) {
	schema := buildParamSchemaForSkill(NLSkillDefinition{
		Name: "partial-schema",
		Params: []corelib.NLSkillParam{
			{Name: "format", Description: "Output format"},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "convert {{input}} --format {{format}}"}},
		},
		RequiredArgs: []string{"input"},
	})

	if !strings.Contains(schema, "format") || !strings.Contains(schema, "input") {
		t.Fatalf("schema did not include complete params:\n%s", schema)
	}
}

// --- Helpers ---

// runInjection exercises the same matching/filtering/injection logic as
// appendKnowledgeSkillSection, without requiring an IMMessageHandler.
// tokenBudget=0 uses the default.
func runInjection(t *testing.T, skills []NLSkillDefinition, userMessage string, tokenBudget int) string {
	t.Helper()
	if tokenBudget <= 0 {
		tokenBudget = defaultKnowledgeSkillTokenBudget
	}

	msgLower := strings.ToLower(userMessage)

	var matched []matchedKnowledgeSkill
	for _, s := range skills {
		if s.Status != "active" {
			continue
		}

		var content string
		switch {
		case s.Type == "knowledge" && s.Content != "":
			content = s.Content
		case s.Type != "knowledge" && s.SkillDir != "":
			content = loadSkillDocContent(s.SkillDir)
		}
		if content == "" {
			continue
		}

		triggers := s.Triggers
		if len(triggers) == 0 {
			continue
		}
		score := countTriggerMatches(triggers, msgLower)
		if score == 0 && strings.Contains(msgLower, strings.ToLower(s.Name)) {
			score = 1
		}
		if score == 0 {
			continue
		}

		matched = append(matched, matchedKnowledgeSkill{
			Name:    s.Name,
			Content: content,
			Score:   score,
		})
	}

	if len(matched) == 0 {
		return ""
	}

	sortMatchedKnowledgeSkills(matched)

	var b strings.Builder
	totalTokensUsed := 0
	b.WriteString("\n## Skill 使用文档\n")
	for _, m := range matched {
		if totalTokensUsed >= tokenBudget {
			break
		}
		content := m.Content
		contentTokens := estimateTokens(content)
		remaining := tokenBudget - totalTokensUsed
		if contentTokens > remaining {
			content = truncateToTokenBudget(content, remaining)
			contentTokens = estimateTokens(content)
		}
		totalTokensUsed += contentTokens
		b.WriteString("\n### Skill: " + m.Name + "\n")
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n---\n")
	}
	return b.String()
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", needle, haystack)
	}
}
