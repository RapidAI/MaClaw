package main

import (
	"strings"
	"testing"
)

func TestSelectRelevantSkillsForTask_NilHandler(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: nil},
	}
	result := cb.selectRelevantSkillsForTask("优化登录页面 UI")
	if result != nil {
		t.Errorf("expected nil with nil handler, got %v", result)
	}
}

func TestSelectRelevantSkillsForTask_EmptyDescription(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
	}
	// Empty task description → nil result
	result := cb.selectRelevantSkillsForTask("")
	if result != nil {
		t.Errorf("expected nil with empty description, got %v", result)
	}
}

func TestExtractBigrams(t *testing.T) {
	bigrams := extractBigrams("优化ui")
	if bigrams == nil {
		t.Fatal("expected non-nil bigrams")
	}
	// "优化ui" → runes: [优,化,u,i] → bigrams: "优化", "化u", "ui"
	if !bigrams["优化"] {
		t.Error("expected bigram '优化'")
	}
	if !bigrams["ui"] {
		t.Error("expected bigram 'ui'")
	}
}

func TestBigramJaccard(t *testing.T) {
	a := extractBigrams("优化登录页面ui")
	b := extractBigrams("分析ui截图优化建议")

	score := bigramJaccard(a, b)
	if score <= 0 {
		t.Errorf("expected positive Jaccard score for overlapping Chinese text, got %f", score)
	}

	// Completely unrelated text should score near 0
	c := extractBigrams("天气预报北京温度")
	scoreUnrelated := bigramJaccard(a, c)
	if scoreUnrelated >= score {
		t.Errorf("unrelated text should score lower: related=%f unrelated=%f", score, scoreUnrelated)
	}
}

func TestBigramJaccard_Empty(t *testing.T) {
	score := bigramJaccard(nil, extractBigrams("hello"))
	if score != 0 {
		t.Errorf("expected 0 for nil input, got %f", score)
	}
}

func TestBuildCodingSubAgentSkillSection_Empty(t *testing.T) {
	section := buildCodingSubAgentSkillSection(nil)
	if section != "" {
		t.Errorf("expected empty section for nil skills, got %q", section)
	}
}

func TestBuildCodingSubAgentSkillSection_WithSkills(t *testing.T) {
	skills := []codingSubAgentSkillMatch{
		{Name: "ui-ux-pro-max", Description: "分析 UI 截图并生成优化建议", RequiredArgs: []string{"input"}},
		{Name: "eslint-fixer", Description: "自动修复 ESLint 报错"},
	}
	section := buildCodingSubAgentSkillSection(skills)

	if !strings.Contains(section, "ui-ux-pro-max") {
		t.Error("section should contain skill name")
	}
	if !strings.Contains(section, "eslint-fixer") {
		t.Error("section should contain second skill name")
	}
	if !strings.Contains(section, "manage_skill") {
		t.Error("section should mention manage_skill usage")
	}
	if !strings.Contains(section, "action=\"run\"") {
		t.Error("section should only allow run action")
	}
	// Should include required args for skills that have them.
	if !strings.Contains(section, "参数: input") {
		t.Error("section should show required args for ui-ux-pro-max")
	}
}

func TestBuildManageSkillToolDefinition_Structure(t *testing.T) {
	def := buildManageSkillToolDefinition()

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("expected function field")
	}
	name, _ := fn["name"].(string)
	if name != "manage_skill" {
		t.Errorf("expected name=manage_skill, got %q", name)
	}

	params, ok := fn["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters field")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties field")
	}

	// Should have action with enum restriction
	actionProp, ok := props["action"].(map[string]interface{})
	if !ok {
		t.Fatal("expected action property")
	}
	enumVals, ok := actionProp["enum"].([]string)
	if !ok {
		t.Fatal("expected enum field in action")
	}
	if len(enumVals) != 2 || enumVals[0] != "run" || enumVals[1] != "status" {
		t.Errorf("expected enum=[run,status], got %v", enumVals)
	}
}

func TestExecuteManageSkill_NoMatchedSkills(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent:      &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: nil,
	}
	result := cb.executeManageSkill(map[string]interface{}{"action": "run", "name": "some-skill"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Errorf("expected blocked outcome, got %v", result.Outcome)
	}
}

func TestExecuteManageSkill_DisallowedAction(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "ui-ux-pro-max"},
		},
	}
	result := cb.executeManageSkill(map[string]interface{}{"action": "install", "name": "something"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Errorf("expected blocked for install action, got %v", result.Outcome)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Errorf("expected not allowed message, got %q", result.Text)
	}
}

func TestExecuteManageSkill_UnmatchedSkillName(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "ui-ux-pro-max"},
			{Name: "eslint-fixer"},
		},
	}
	result := cb.executeManageSkill(map[string]interface{}{"action": "run", "name": "tts-to-mp3"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Errorf("expected blocked for unmatched skill, got %v", result.Outcome)
	}
	if !strings.Contains(result.Text, "not available") {
		t.Errorf("expected not available message, got %q", result.Text)
	}
}

func TestIsMatchedSkill_CaseInsensitive(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "UI-UX-Pro-Max"},
		},
	}
	if !cb.isMatchedSkill("ui-ux-pro-max") {
		t.Error("isMatchedSkill should be case-insensitive")
	}
	if cb.isMatchedSkill("other-skill") {
		t.Error("isMatchedSkill should return false for non-matched skill")
	}
}

func TestCodingSubAgentAllowedSkillActions(t *testing.T) {
	if !codingSubAgentAllowedSkillActions["run"] {
		t.Error("run should be allowed")
	}
	if !codingSubAgentAllowedSkillActions["status"] {
		t.Error("status should be allowed")
	}
	if codingSubAgentAllowedSkillActions["install"] {
		t.Error("install should not be allowed")
	}
	if codingSubAgentAllowedSkillActions["uninstall"] {
		t.Error("uninstall should not be allowed")
	}
	if codingSubAgentAllowedSkillActions["patch"] {
		t.Error("patch should not be allowed")
	}
}


func TestExecuteManageSkill_CaseInsensitiveToolName(t *testing.T) {
	// LLM might output "Manage_Skill" or "MANAGE_SKILL" — should still route correctly.
	// Simulate what executeToolWithOutcome does: canonicalize then check dynamic tools.
	canonical := canonicalCodingSubAgentToolName("Manage_Skill")
	if canonical != "manage_skill" {
		t.Errorf("expected canonical name 'manage_skill', got %q", canonical)
	}

	canonical2 := canonicalCodingSubAgentToolName("MANAGE_SKILL")
	if canonical2 != "manage_skill" {
		t.Errorf("expected canonical name 'manage_skill', got %q", canonical2)
	}
}
