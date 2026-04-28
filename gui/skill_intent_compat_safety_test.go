package main

import (
	"strings"
	"testing"
)

// Safety audit tests for the intent-capability compatibility system.
//
// Three safety properties:
//   不乱动 (no false action): gate must not let incompatible skills through
//   不误会 (no false rejection): gate must not block legitimate skill matches
//   可管控 (controllable): unknown/ambiguous inputs must fail open, not closed

// --- 不乱动: No False Action ---
// The gate must block skills whose capability is incompatible with user intent.

func TestSafety_NoFalseAction_QueryVsGenerate(t *testing.T) {
	// All query-intent messages must be blocked from generate-capability skills.
	queryMessages := []string{
		"统计d盘上的pdf文件",
		"搜索所有的markdown文档",
		"查找D盘的报告",
		"列出所有pdf",
		"查看这个文档",
		"打开桌面上的pdf",
		"count pdf files",
		"find all markdown files",
		"list reports on disk",
		"search for pdf documents",
	}
	generateSkillDesc := "Converts Markdown files to polished, print-ready PDF with headings, code blocks, tables, and CJK-friendly typography."

	for _, msg := range queryMessages {
		if isIntentCompatibleWithSkill(msg, generateSkillDesc) {
			t.Errorf("SAFETY VIOLATION: query message %q was compatible with generate skill", msg)
		}
		if isIntentSkillPreferenceCompatible(msg) {
			t.Errorf("SAFETY VIOLATION: query message %q entered skill preference path", msg)
		}
	}
}

func TestSafety_NoFalseAction_SkillPreferenceDoesNotStripBash(t *testing.T) {
	// When shouldPreferSkillForTask returns false, the agent loop must NOT
	// call filterToolsForSkillPreference. Verify by checking that query
	// intent messages don't trigger skill preference.
	queryMessages := []string{
		"统计d盘上的pdf文件",
		"搜索D盘的pdf文件",
		"查找所有markdown文档",
		"列出报告文件",
		"打开桌面上的pdf",
	}
	for _, msg := range queryMessages {
		if shouldPreferSkillForTask(msg) {
			t.Errorf("SAFETY VIOLATION: %q triggered skill preference (would strip bash tool)", msg)
		}
	}
}

// --- 不误会: No False Rejection ---
// The gate must not block legitimate skill matches.

func TestSafety_NoFalseRejection_GenerateWithTopicKeyword(t *testing.T) {
	// Generate-intent messages with topic keywords must still trigger skill preference.
	generateMessages := []struct {
		text string
		desc string
	}{
		{"生成一份PDF报告", "Converts Markdown files to PDF"},
		{"把这个md转换成pdf", "Converts Markdown files to PDF"},
		{"导出为PDF格式", "Converts Markdown files to PDF"},
		{"创建一个markdown文档", "Generates Markdown documents"},
		{"制作一份综述", "Generates literature review reports"},
		{"generate a PDF report", "Converts Markdown to PDF"},
		{"convert this to pdf", "Converts Markdown to PDF"},
		{"export as markdown", "Generates Markdown documents"},
	}
	for _, tt := range generateMessages {
		if !isIntentCompatibleWithSkill(tt.text, tt.desc) {
			t.Errorf("FALSE REJECTION: generate message %q blocked from compatible skill %q", tt.text, tt.desc)
		}
	}
}

func TestSafety_NoFalseRejection_SendWithGenerateSkill(t *testing.T) {
	// "发送PDF" needs generation first — must be compatible with generate skills.
	sendMessages := []string{
		"发送这个pdf给我",
		"把文件发给我",
		"send me the pdf",
	}
	generateSkillDesc := "Converts Markdown files to PDF"
	for _, msg := range sendMessages {
		if !isIntentCompatibleWithSkill(msg, generateSkillDesc) {
			t.Errorf("FALSE REJECTION: send message %q blocked from generate skill", msg)
		}
	}
}

func TestSafety_NoFalseRejection_QueryWithQuerySkill(t *testing.T) {
	// Query intent must be compatible with query-capability skills.
	if !isIntentCompatibleWithSkill("搜索论文", "Search local documents using semantic keywords") {
		t.Error("FALSE REJECTION: query message blocked from query skill")
	}
	if !isIntentCompatibleWithSkill("find papers", "Searches academic databases for papers") {
		t.Error("FALSE REJECTION: query message blocked from query skill")
	}
}

func TestSafety_NoFalseRejection_TriggerBasedMatch(t *testing.T) {
	// Skills matched by explicit triggers (score += 3) should still pass
	// the compatibility gate when intent is compatible.
	// This tests that the gate doesn't interfere with trigger-based matching.
	if !isIntentCompatibleWithSkill("生成天气报告", "Generates weather reports with charts") {
		t.Error("FALSE REJECTION: generate intent blocked from generate skill with trigger match")
	}
}

// --- 可管控: Controllable / Fail-Open ---
// Unknown or ambiguous inputs must not be blocked.

func TestSafety_FailOpen_UnknownIntent(t *testing.T) {
	// Messages with no recognizable verb must return intentCatUnknown
	// and be compatible with ALL skills.
	unknownMessages := []string{
		"pdf",
		"hello",
		"报告",
		"markdown文件",
		"daily papers",
		"这个文件",
		"D盘",
	}
	for _, msg := range unknownMessages {
		intent := extractUserIntentCategory(msg)
		if intent != intentCatUnknown {
			t.Errorf("CONTROL VIOLATION: %q classified as %q instead of unknown", msg, intent)
		}
		// Unknown intent must be compatible with everything
		if !isIntentCompatibleWithSkill(msg, "Converts Markdown to PDF") {
			t.Errorf("CONTROL VIOLATION: unknown intent %q blocked from skill", msg)
		}
		if !isIntentCompatibleWithSkill(msg, "Search documents") {
			t.Errorf("CONTROL VIOLATION: unknown intent %q blocked from skill", msg)
		}
	}
}

func TestSafety_FailOpen_NoCapabilities(t *testing.T) {
	// Skills with no extractable capabilities must not be blocked.
	noCapDescs := []string{
		"A useful tool",
		"Does something with files",
		"PDF helper",
		"",
	}
	for _, desc := range noCapDescs {
		if !isIntentCompatibleWithSkill("统计pdf文件", desc) {
			t.Errorf("CONTROL VIOLATION: query intent blocked from no-capability skill %q", desc)
		}
	}
}

func TestSafety_FailOpen_UnknownIntentEntersSkillPreference(t *testing.T) {
	// Unknown intent must still enter skill preference path (fail open).
	if !isIntentSkillPreferenceCompatible("pdf报告") {
		t.Error("CONTROL VIOLATION: unknown intent blocked from skill preference path")
	}
	if !isIntentSkillPreferenceCompatible("daily papers") {
		t.Error("CONTROL VIOLATION: unknown intent blocked from skill preference path")
	}
}

// --- 边界条件: Adversarial Inputs ---

func TestSafety_Adversarial_MixedIntentVerbs(t *testing.T) {
	// When a message contains verbs from multiple categories, first-match wins.
	// "查找并生成PDF报告" — "查找" (query) appears first → query intent.
	// This is conservative: query blocks generate skills.
	intent := extractUserIntentCategory("查找并生成PDF报告")
	if intent != intentCatQuery {
		t.Errorf("mixed intent: got %q, want query (first-match)", intent)
	}

	// "生成并查找PDF" — "生成" (generate) appears first → generate intent.
	intent = extractUserIntentCategory("生成并查找PDF")
	if intent != intentCatGenerate {
		t.Errorf("mixed intent: got %q, want generate (first-match)", intent)
	}
}

func TestSafety_Adversarial_VerbInMiddleOfCompound(t *testing.T) {
	// "自动生成" — "生成" at position 1 (0-indexed rune) should match.
	intent := extractUserIntentCategory("自动生成PDF")
	if intent != intentCatGenerate {
		t.Errorf("compound verb: got %q, want generate", intent)
	}

	// "重新统计" — "统计" at position 1 should match.
	intent = extractUserIntentCategory("重新统计文件")
	if intent != intentCatQuery {
		t.Errorf("compound verb: got %q, want query", intent)
	}
}

func TestSafety_Adversarial_VeryLongInput(t *testing.T) {
	// Long input should not panic or take excessive time.
	long := "统计" + strings.Repeat("这是一段很长的描述文字用来测试性能", 100) + "pdf文件"
	intent := extractUserIntentCategory(long)
	if intent != intentCatQuery {
		t.Errorf("long input: got %q, want query", intent)
	}
}

func TestSafety_Adversarial_EmptyAndWhitespace(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n"} {
		intent := extractUserIntentCategory(input)
		if intent != intentCatUnknown {
			t.Errorf("empty/whitespace %q: got %q, want unknown", input, intent)
		}
		if !isIntentSkillPreferenceCompatible(input) {
			t.Errorf("empty/whitespace %q: blocked from skill preference", input)
		}
	}
}
