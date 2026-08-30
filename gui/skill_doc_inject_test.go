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

	githubOnly := runInjection(t, skills, "帮我看看 github 上的项目", 0)
	assertNotContains(t, githubOnly, "git-guide")
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

func TestSkillDocInject_ShellBrowserAutomationSkillNotInjected(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:        "zhihu-poster",
			Type:        "knowledge",
			Status:      "active",
			RequiresGUI: true,
			Content:     "# Zhihu Poster\nUse playwright connect_over_cdp and --screenshot.",
			Triggers:    []string{"zhihu", "publish"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "python post.py --screenshot"},
			}},
		},
	}

	result := runInjection(t, skills, "publish to zhihu", 0)

	assertNotContains(t, result, "zhihu-poster")
	assertNotContains(t, result, "connect_over_cdp")
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

func TestSkillDocInject_BookPDFAgentGuidedWorkflowFromInstructions(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:     "Book-PDF: 书籍级PDF手册全流程",
			Type:     "executable",
			Status:   "active",
			Source:   "clawhub",
			Triggers: []string{"huashu-book-pdf"},
			Steps: []corelib.NLSkillStep{{
				Action: "craft_tool",
				Params: map[string]interface{}{
					"instructions": "# Book-PDF：书籍级PDF手册全流程\n\n五个阶段：调研 → 规划 → 写作 → 构建 → 版本更新。\n启动多个background agent并行调研，多Agent并行写作。\n与用户确认大纲后进入写作。使用 templates/ 和 scripts/init-project.sh。version.json 记录语义化版本。",
				},
			}},
		},
	}

	for _, msg := range []string{
		"使用book-pdf skill 编写人工智能数学入门教授，先列个大纲",
		"book pdf 已经安装",
		"按书籍级PDF手册全流程写一本入门教程",
	} {
		result := runInjection(t, skills, msg, 0)
		if !strings.Contains(result, "### Skill: Book-PDF: 书籍级PDF手册全流程") {
			t.Fatalf("msg %q did not inject Book-PDF skill docs:\n%s", msg, result)
		}
		if !strings.Contains(result, "五个阶段") {
			t.Fatalf("msg %q dropped the workflow body:\n%s", msg, result)
		}
		if !strings.Contains(result, "不要 discover_tool") {
			t.Fatalf("msg %q missing agent-guided lead-in:\n%s", msg, result)
		}
	}

	unrelated := runInjection(t, skills, "南京天气，生成pdf报告", 0)
	if strings.Contains(unrelated, "Book-PDF") || strings.Contains(unrelated, "五个阶段") {
		t.Fatalf("unrelated PDF request must not inject Book-PDF:\n%s", unrelated)
	}
	for _, msg := range []string{
		"facebook pdf 怎么导出",
		"handbook pdf 在哪",
		"handbook-pdf 在哪",
		"facebook-pdf 怎么导出",
	} {
		got := runInjection(t, skills, msg, 0)
		if strings.Contains(got, "Book-PDF") {
			t.Fatalf("msg %q must not inject Book-PDF via compact-token overlap:\n%s", msg, got)
		}
	}

	shortName := []NLSkillDefinition{{
		Name:     "Book-PDF",
		Type:     "executable",
		Status:   "active",
		Source:   "clawhub",
		Triggers: []string{"huashu-book-pdf"},
		Steps:    skills[0].Steps,
	}}
	if got := runInjection(t, shortName, "handbook-pdf 在哪", 0); strings.Contains(got, "Book-PDF") {
		t.Fatalf("bare name Book-PDF must not substring-match handbook-pdf:\n%s", got)
	}
	if got := runInjection(t, shortName, "使用 book_pdf 编写教程", 0); !strings.Contains(got, "### Skill: Book-PDF") {
		t.Fatalf("underscore variant of the skill name must still inject:\n%s", got)
	}

	promptAlias := []NLSkillDefinition{{
		Name:     skills[0].Name,
		Type:     "executable",
		Status:   "active",
		Source:   "clawhub",
		Triggers: []string{"huashu-book-pdf"},
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{
				"prompt": skills[0].Steps[0].Params["instructions"],
			},
		}},
	}}
	if got := runInjection(t, promptAlias, "使用book-pdf skill 编写教程", 0); !strings.Contains(got, "五个阶段") {
		t.Fatalf("craft_tool prompt alias must still load the workflow body:\n%s", got)
	}

	manual := []NLSkillDefinition{{
		Name:     skills[0].Name,
		Type:     "executable",
		Status:   "active",
		Source:   "manual",
		Triggers: skills[0].Triggers,
		Steps:    skills[0].Steps,
	}}
	if got := runInjection(t, manual, "使用book-pdf skill 编写教程", 0); strings.Contains(got, "五个阶段") {
		t.Fatalf("manual craft_tool must not inject as agent-guided docs:\n%s", got)
	}

	if !agentGuidedSkillDocsMatch(skills, "按书籍级PDF手册全流程写一本入门教程") {
		t.Fatal("agent-guided Book-PDF must match the inject phrase without the word skill")
	}
	if !agentGuidedSkillDocsMatch(skills, "使用book pdf生成书籍") {
		t.Fatal("agent-guided Book-PDF must match the spaced skill name without the word skill")
	}
	if agentGuidedSkillDocsMatch(manual, "使用book-pdf skill 编写教程") {
		t.Fatal("manual craft_tool must not count as an agent-guided named skill turn")
	}
	if agentGuidedSkillDocsMatch(skills, "南京天气，生成pdf报告") {
		t.Fatal("unrelated PDF request must not count as an agent-guided named skill turn")
	}
}

func TestSkillDocInject_EmptyTriggersNotInjectedByName(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:    "office",
			Type:    "knowledge",
			Status:  "active",
			Content: "# Office\nShould not appear without triggers.",
		},
	}
	got := runInjection(t, skills, "帮我处理一份 office 文档", 0)
	if strings.Contains(got, "Should not appear") {
		t.Fatalf("skills without triggers must not inject:\n%s", got)
	}
}

func TestSkillDocPhraseOccursRequiresBoundary(t *testing.T) {
	if !skillDocPhraseOccurs("使用book-pdf skill", "book-pdf") {
		t.Fatal("hyphenated alias must match")
	}
	if !skillDocPhraseOccurs("book pdf 已经安装", "book-pdf") {
		t.Fatal("spaced alias must match")
	}
	if !skillDocPhraseOccurs("使用book\uff0dpdf skill", "book-pdf") {
		t.Fatal("fullwidth hyphen must count as a separator")
	}
	if skillDocPhraseOccurs("facebook pdf 怎么导出", "book-pdf") {
		t.Fatal("embedded 'book pdf' inside facebook must not match")
	}
	if skillDocPhraseOccurs("handbook pdf 在哪", "book-pdf") {
		t.Fatal("embedded 'book pdf' inside handbook must not match")
	}
	if skillDocPhraseOccurs("bookpdf 已经安装", "book-pdf") {
		t.Fatal("concatenated bookpdf must not match book-pdf")
	}
	if skillDocPhraseOccurs("handbook-pdf 在哪", "book-pdf") {
		t.Fatal("hyphenated handbook-pdf must not substring-match book-pdf")
	}
	if !skillDocPhraseOccurs("使用 book_pdf 编写", "book-pdf") {
		t.Fatal("underscore must count as a separator")
	}
	if !skillDocPhraseOccurs("使用book\u2015pdf skill", "book-pdf") {
		t.Fatal("horizontal bar must count as a separator")
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

	matched := collectMatchedSkillDocs(skills, userMessage)
	if len(matched) == 0 {
		return ""
	}
	var b strings.Builder
	writeMatchedSkillDocs(&b, matched, tokenBudget)
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
