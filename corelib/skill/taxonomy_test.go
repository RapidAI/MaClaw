package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestClassifySkill_Executable(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}},
		},
	}
	if got := ClassifySkill(skill); got != TaxonomyExecutable {
		t.Errorf("expected executable, got %s", got)
	}
}

func TestClassifySkill_Knowledge(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Type:    "knowledge",
		Content: "This is a knowledge skill",
	}
	if got := ClassifySkill(skill); got != TaxonomyKnowledge {
		t.Errorf("expected knowledge, got %s", got)
	}
}

func TestClassifySkill_Craftable(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "generate code"}},
		},
	}
	if got := ClassifySkill(skill); got != TaxonomyCraftable {
		t.Errorf("expected craftable, got %s", got)
	}
}

func TestClassifySkill_MixedSteps_CraftTakePrecedence(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "setup.sh"}},
			{Action: "craft_tool", Params: map[string]interface{}{"task": "generate"}},
			{Action: "bash", Params: map[string]interface{}{"command": "cleanup.sh"}},
		},
	}
	if got := ClassifySkill(skill); got != TaxonomyCraftable {
		t.Errorf("expected craftable (craft_tool present), got %s", got)
	}
}

func TestClassifySkill_SolidifiedStillCraftable(t *testing.T) {
	// A solidified skill has FallbackStep pointing to the original craft_tool.
	// Classification should be stable — still craftable, not executable.
	originalCraft := corelib.NLSkillStep{Action: "craft_tool", Params: map[string]interface{}{"task": "generate"}}
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python /tmp/script.py"}, FallbackStep: &originalCraft},
		},
	}
	if got := ClassifySkill(skill); got != TaxonomyCraftable {
		t.Errorf("solidified skill should still be craftable, got %s", got)
	}
}

func TestFormatSkillForContext_Executable_Compact(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "pdf-converter",
		Description: "Convert documents to PDF",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Name: "Install deps", Params: map[string]interface{}{"command": "pip install pdfplumber"}},
			{Action: "bash", Name: "Convert file", Params: map[string]interface{}{"command": "python convert.py {{input}}"}},
		},
		RequiredArgs: []string{"input"},
	}

	text := FormatSkillForContext(skill, TaxonomyExecutable, "")

	if !strings.Contains(text, "执行步骤") {
		t.Error("executable context should contain step summary")
	}
	if !strings.Contains(text, "Install deps") {
		t.Error("executable context should contain step names")
	}
	if !strings.Contains(text, "input") {
		t.Error("executable context should contain parameter schema")
	}
}

func TestFormatSkillForContext_CompletesPartialParamSchema(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "partial-schema",
		Params: []corelib.NLSkillParam{
			{Name: "format", Description: "Output format"},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "convert {{input}} --format {{format}}"}},
		},
		RequiredArgs: []string{"input"},
	}

	text := FormatSkillForContext(skill, TaxonomyExecutable, "")

	if !strings.Contains(text, "format") || !strings.Contains(text, "input") {
		t.Fatalf("context did not include complete param schema:\n%s", text)
	}
}

func TestFormatSkillForContext_Knowledge_Full(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "coding-guide",
		Description: "Best practices for Go",
	}
	docContent := "# Go Best Practices\n\nAlways handle errors.\n\nUse gofmt."

	text := FormatSkillForContext(skill, TaxonomyKnowledge, docContent)

	if !strings.Contains(text, "Go Best Practices") {
		t.Error("knowledge context should contain full documentation")
	}
	if !strings.Contains(text, "Always handle errors") {
		t.Error("knowledge context should contain full documentation content")
	}
}

func TestFormatSkillForContext_Craftable_Truncated(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "tts-to-mp3",
		Description: "Convert text to speech",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "generate TTS script"}},
		},
	}
	docContent := strings.Repeat("This is a very long documentation line. ", 50)

	text := FormatSkillForContext(skill, TaxonomyCraftable, docContent)

	if !strings.Contains(text, "Convert text to speech") {
		t.Error("craftable context should contain description")
	}
	if !strings.Contains(text, "文档已截断") {
		t.Error("craftable context should truncate long documentation")
	}
	if len(text) > 1000 {
		t.Errorf("craftable context too long: %d chars (expected <1000)", len(text))
	}
}

func TestFormatSkillForContext_Executable_WithOperations(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "api-workflow",
		Description: "API workflow skill",
		Mode:        "api_workflow",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "create"},
			{Action: "bash", Label: "query"},
		},
		Operations: []corelib.NLSkillOperation{
			{Name: "generate", Description: "Create a new session"},
			{Name: "query", Description: "Query session status"},
		},
	}

	text := FormatSkillForContext(skill, TaxonomyExecutable, "")

	if !strings.Contains(text, "操作") {
		t.Error("api_workflow skill should list operations")
	}
	if !strings.Contains(text, "generate") {
		t.Error("should contain operation names")
	}
}

func TestTruncateRunes_Short(t *testing.T) {
	got := truncateRunes("hello", 100)
	if got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}
}

func TestTruncateRunes_ParagraphBreak(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph that is quite long and should be the break point.\n\nThird paragraph."
	got := truncateRunes(text, 80)
	if strings.Contains(got, "Third") {
		t.Error("should truncate before third paragraph")
	}
	if !strings.Contains(got, "First") {
		t.Error("should contain first paragraph")
	}
}
