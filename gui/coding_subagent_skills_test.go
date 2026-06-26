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

func TestBuildCodingSubAgentSkillSectionCapsRequiredArgs(t *testing.T) {
	section := buildCodingSubAgentSkillSection([]codingSubAgentSkillMatch{{
		Name:         "schema-heavy-skill",
		Description:  "runs a helper with many required args",
		RequiredArgs: []string{"input", "output", "project", "mode", "timeout", "format", "locale", "theme"},
	}})

	if !strings.Contains(section, "input, output, project, mode, timeout, format") {
		t.Fatalf("section should include first required args, got %q", section)
	}
	if strings.Contains(section, "locale") || strings.Contains(section, "theme") {
		t.Fatalf("section should cap expanded required args, got %q", section)
	}
	if !strings.Contains(section, "还有 2 项未展开") {
		t.Fatalf("section should report omitted required args, got %q", section)
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

func TestExecuteManageSkill_StatusRequiresRunID(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "ui-ux-pro-max"},
		},
	}

	result := cb.executeManageSkill(map[string]interface{}{"action": "status"})
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("missing status run_id outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "missing required argument") || !strings.Contains(result.Text, "run_id") {
		t.Fatalf("missing status run_id should produce targeted required-argument error, got %q", result.Text)
	}
	if len(cb.getDynamicToolsRun()) != 0 {
		t.Fatalf("manage_skill status without run_id should not execute or be tracked, got %#v", cb.getDynamicToolsRun())
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

func TestExecuteManageSkill_MissingRequiredArgs(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "ui-ux-pro-max", RequiredArgs: []string{"input"}},
		},
	}

	result := cb.executeManageSkill(map[string]interface{}{"action": "run", "name": "ui-ux-pro-max", "args": map[string]interface{}{}})
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("missing skill arg outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "missing required skill argument") || !strings.Contains(result.Text, "input") {
		t.Fatalf("missing skill arg should produce targeted recovery error, got %q", result.Text)
	}
	if len(cb.getDynamicToolsRun()) != 0 {
		t.Fatalf("manage_skill with missing args should not execute or be tracked, got %#v", cb.getDynamicToolsRun())
	}
}

func TestExecuteManageSkill_RequiredArgsAllowTopLevelCompatibility(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "ui-ux-pro-max", RequiredArgs: []string{"input", "mode"}},
		},
	}
	args := map[string]interface{}{
		"input": "screen.png",
		"mode":  "fast",
		"args":  map[string]interface{}{"mode": "precise"},
	}

	if result, rejected := rejectMissingCodingSubAgentSkillRequiredArguments(cb.matchedSkills[0], args); rejected {
		t.Fatalf("top-level input compatibility should satisfy required skill args, got %#v", result)
	}
	skillArgs, ok := args["args"].(map[string]interface{})
	if !ok {
		t.Fatalf("args should remain a JSON object after normalization, got %#v", args["args"])
	}
	if skillArgs["input"] != "screen.png" {
		t.Fatalf("top-level input should be copied into args, got %#v", skillArgs)
	}
	if skillArgs["mode"] != "precise" {
		t.Fatalf("existing args.mode should not be overwritten by top-level mode, got %#v", skillArgs)
	}
}

func TestExecuteManageSkill_RequiredArgsCreateArgsFromTopLevelCompatibility(t *testing.T) {
	skill := codingSubAgentSkillMatch{Name: "ui-ux-pro-max", RequiredArgs: []string{"input"}}
	args := map[string]interface{}{"input": "screen.png"}

	if result, rejected := rejectMissingCodingSubAgentSkillRequiredArguments(skill, args); rejected {
		t.Fatalf("top-level skill arg should create args object, got %#v", result)
	}
	skillArgs, ok := args["args"].(map[string]interface{})
	if !ok || skillArgs["input"] != "screen.png" {
		t.Fatalf("expected args.input to be normalized from top-level input, got %#v", args["args"])
	}
}

func TestCodingSubAgentDynamicToolFailureClassification(t *testing.T) {
	failures := []string{
		"",
		"[error] tool timed out",
		"Error: schema validation failed",
		"错误: 参数缺失",
		"错误：参数缺失",
		"失败: browser crashed",
		"失败：browser crashed",
		"❌ skill failed",
		" failed: runner crashed",
		"Failure: browser disconnected",
		"Exception: timeout waiting for selector",
		"Panic: nil pointer",
		"Tool error: missing dependency",
		"MCP call failed: runtime owner is missing",
		"MCP tool error: validation failed",
		"MCP 调用失败: unknown server",
		"MCP 调用被拒绝: builtin tool",
		"MCP Registry 未初始化",
		"本地 MCP Manager 未初始化",
		"Validation failed: missing url",
		"arguments JSON parse failed: unexpected end",
	}
	for _, text := range failures {
		if !isCodingSubAgentDynamicToolFailure(text) {
			t.Fatalf("%q should be classified as failed", text)
		}
	}

	successes := []string{
		"fixed 3 errors and wrote report",
		"No skills found for query: lint",
		"MCP tool completed successfully",
	}
	for _, text := range successes {
		if isCodingSubAgentDynamicToolFailure(text) {
			t.Fatalf("%q should not be classified as failed", text)
		}
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

func TestCodingSubAgentDynamicToolBlocksAreAudited(t *testing.T) {
	skillCB := &codingSubAgentCallbacks{
		subagent:      &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{{Name: "ui-ux-pro-max"}},
	}
	result := skillCB.executeManageSkill(map[string]interface{}{"action": "install", "name": "ui-ux-pro-max"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("disallowed manage_skill outcome = %q, want blocked", result.Outcome)
	}
	violations := skillCB.getGuardrailViolations()
	if len(violations) != 1 || violations[0].Tool != "manage_skill" || violations[0].Category != codingSubAgentGuardrailCategoryPolicy {
		t.Fatalf("manage_skill block should be audited as policy guardrail, got %#v", violations)
	}

	missingSkillCB := &codingSubAgentCallbacks{
		subagent:      &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{{Name: "eslint-fixer"}},
	}
	result = missingSkillCB.executeManageSkill(map[string]interface{}{"action": "run", "name": "tts-to-mp3"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("unmatched manage_skill outcome = %q, want blocked", result.Outcome)
	}
	violations = missingSkillCB.getGuardrailViolations()
	if len(violations) != 1 || violations[0].Tool != "manage_skill" || !strings.Contains(violations[0].Summary, "not available") {
		t.Fatalf("unmatched manage_skill should be audited, got %#v", violations)
	}

	mcpCB := &codingSubAgentCallbacks{}
	result = mcpCB.executeCallMCPTool(map[string]interface{}{"server_id": "browser", "tool_name": "screenshot"})
	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("unavailable call_mcp_tool outcome = %q, want blocked", result.Outcome)
	}
	violations = mcpCB.getGuardrailViolations()
	if len(violations) != 1 || violations[0].Tool != "call_mcp_tool" || violations[0].Category != codingSubAgentGuardrailCategoryPolicy {
		t.Fatalf("call_mcp_tool block should be audited as policy guardrail, got %#v", violations)
	}
}
