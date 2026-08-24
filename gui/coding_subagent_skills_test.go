package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestSelectRelevantSkillsForTaskExcludesInstructionOnlyAppContainers(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{Name: "pdf-translator-app", Status: "active", Type: "instruction", Description: "translate PDF documents", Triggers: []string{"pdf", "translate"}},
		{Name: "pdf-translator", Status: "active", Description: "translate PDF documents", Triggers: []string{"pdf", "translate"}, Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo translate"}}}},
	}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	callback := &codingSubAgentCallbacks{subagent: &CodingSubAgent{handler: &IMMessageHandler{app: app}}}

	matched := callback.selectRelevantSkillsForTask("translate a PDF document")
	for _, item := range matched {
		if item.Name == "pdf-translator-app" {
			t.Fatalf("instruction-only app container leaked into coding subagent matches: %#v", matched)
		}
	}
	if len(matched) != 1 || matched[0].Name != "pdf-translator" {
		t.Fatalf("matched skills = %#v, want only executable pdf-translator", matched)
	}
}

func TestSelectRelevantSkillsForTaskExcludesGUIIncompatibleLegacySkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:        "legacy-web-search",
			Status:      "active",
			Description: "search current weather online",
			Triggers:    []string{"weather", "search"},
			Steps:       []corelib.NLSkillStep{{Action: "web_search"}},
		},
		{
			Name:        "gui-safe-weather",
			Status:      "active",
			Description: "inspect weather data through a GUI-safe command",
			Triggers:    []string{"weather", "search"},
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo weather"}}},
		},
	}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	callback := &codingSubAgentCallbacks{subagent: &CodingSubAgent{handler: &IMMessageHandler{app: app}}}

	matched := callback.selectRelevantSkillsForTask("search current weather")
	for _, item := range matched {
		if item.Name == "legacy-web-search" {
			t.Fatalf("GUI-incompatible legacy skill leaked into CodingSubAgent matches: %#v", matched)
		}
	}
	if len(matched) != 1 || matched[0].Name != "gui-safe-weather" {
		t.Fatalf("matched skills = %#v, want only gui-safe-weather", matched)
	}
}

// A full coding environment widens the candidate pool, but it must not pad
// unfilled slots with unscored skills: a learned skill's description is the
// raw request of the session it came from, so padding put an unrelated past
// task in front of the model as if it were the current one.
func TestSelectRelevantSkillsForTaskFullEnvDoesNotPadWithUnrelatedSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name: "craft_task_ded4ddee", Status: "active", Source: "learned",
			Description: "北京天气",
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo weather"}}},
		},
		{
			Name: "craft_api2api2mac", Status: "active", Source: "learned",
			Description: "api2服务器信息保存进知识库",
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo api2"}}},
		},
	}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: app},
		fullEnvironment: true,
	}}

	if matched := cb.selectRelevantSkillsForTask("push"); len(matched) != 0 {
		t.Fatalf("unrelated learned skills leaked into a full-env coding turn: %#v", matched)
	}
}

// Relevance scoring is not the isolation boundary. A skill learned from a
// general chat is excluded from a coding turn even when its text scores well,
// while the coding pool itself keeps accumulating across coding tasks.
func TestSelectRelevantSkillsForTaskExcludesOtherExperienceDomain(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name: "craft_chat_eslint", Status: "active", Source: "learned",
			ExperienceDomain: corelib.SkillDomainGeneral,
			Description:      "自动修复 eslint 报错并格式化代码",
			Triggers:         []string{"eslint", "lint"},
			Steps:            []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo chat"}}},
		},
		{
			Name: "craft_coding_eslint", Status: "active", Source: "learned",
			ExperienceDomain: corelib.SkillDomainCoding,
			Description:      "自动修复 eslint 报错并格式化代码",
			Triggers:         []string{"eslint", "lint"},
			Steps:            []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo coding"}}},
		},
	}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: app},
		fullEnvironment: true,
	}}

	matched := cb.selectRelevantSkillsForTask("修复 eslint 报错")
	for _, item := range matched {
		if item.Name == "craft_chat_eslint" {
			t.Fatalf("chat-learned skill leaked into a coding turn: %#v", matched)
		}
	}
	if len(matched) != 1 || matched[0].Name != "craft_coding_eslint" {
		t.Fatalf("matched skills = %#v, want only the coding-pool skill", matched)
	}
}

func TestSelectRelevantSkillsForTaskFullEnvKeepsRelevantSkill(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name: "craft_task_ded4ddee", Status: "active", Source: "learned",
			Description: "北京天气",
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo weather"}}},
		},
		{
			Name: "eslint-fixer", Status: "active",
			Description: "自动修复 eslint 报错并格式化代码",
			Triggers:    []string{"eslint", "lint"},
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo lint"}}},
		},
	}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler:         &IMMessageHandler{app: app},
		fullEnvironment: true,
	}}

	matched := cb.selectRelevantSkillsForTask("修复 eslint 报错")
	if len(matched) != 1 || matched[0].Name != "eslint-fixer" {
		t.Fatalf("matched = %#v, want only eslint-fixer", matched)
	}
	if matched[0].Score <= 0 {
		t.Fatalf("matched skill must carry a positive score, got %v", matched[0].Score)
	}
}

// Short or unrecognized task text used to fall through the fit filter and
// admit every installed skill, including document/office ones.
func TestCodingSubAgentSkillFitsTaskRejectsDocumentSkillsForUnrecognizedTask(t *testing.T) {
	for _, doc := range []string{
		"pptx-generator Generate PowerPoint presentation slides deck",
		"contract-review Review contract clauses and legal document text",
	} {
		if codingSubAgentSkillFitsTask("push", doc) {
			t.Fatalf("document skill %q should not fit an unrecognized short task", doc)
		}
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

func TestCodingSubAgentSkillFitsTaskFiltersDocumentSkillsForSoftwareTask(t *testing.T) {
	task := "开发一个 windows 文件重定向驱动，支持 windows 8,10,11 (tests)"
	for _, doc := range []string{
		"pptx-generator Generate PowerPoint presentation slides deck",
		"contract-review Review contract clauses and legal document text",
		"pdf-word Convert PDF and Word docx documents",
	} {
		if codingSubAgentSkillFitsTask(task, doc) {
			t.Fatalf("document skill %q should not fit software driver task", doc)
		}
	}
}

func TestCodingSubAgentSkillFitsTaskAllowsCodingAndExplicitDocumentIntent(t *testing.T) {
	driverTask := "开发一个 windows 文件重定向驱动，支持 tests"
	if !codingSubAgentSkillFitsTask(driverTask, "eslint-fixer 自动修复代码 lint 和 test 报错") {
		t.Fatal("coding skill should fit software task")
	}
	if !codingSubAgentSkillFitsTask(driverTask, "ui-review 分析前端界面截图并给出优化建议") {
		t.Fatal("UI skill should remain available for coding/frontend-adjacent tasks")
	}

	docTask := "为这个驱动项目生成开发文档和 pptx 演示"
	if !codingSubAgentSkillFitsTask(docTask, "pptx-generator Generate PowerPoint presentation slides deck") {
		t.Fatal("explicit document request should allow document skills")
	}
}

func TestCodingSubAgentDocumentMarkersRecognizeAllOfficeFormats(t *testing.T) {
	for _, format := range []string{"doc", "docx", "xls", "xlsx", "ppt", "pptx"} {
		t.Run(format, func(t *testing.T) {
			if !containsAny("convert "+format+" document", codingSubAgentDocumentIntentMarkers()...) {
				t.Fatalf("document intent markers omit %q", format)
			}
			if !containsAny("parse "+format+" file", codingSubAgentDocumentSkillMarkers()...) {
				t.Fatalf("document skill markers omit %q", format)
			}
		})
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
	if !strings.Contains(section, "Skill 函数") {
		t.Error("section should describe the request-local Skill function")
	}
	if strings.Contains(section, "manage_skill(action") {
		t.Error("section must not instruct the model to call the generic gateway")
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

func TestBuildCodingSkillInvocationDefinition_Structure(t *testing.T) {
	def := buildCodingSkillInvocationDefinition("skill_01_alias", codingSubAgentSkillMatch{Name: "formatter"})

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("expected function field")
	}
	name, _ := fn["name"].(string)
	if name != "skill_01_alias" {
		t.Errorf("expected request-local alias, got %q", name)
	}

	params, ok := fn["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters field")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties field")
	}

	if _, exists := props["name"]; exists {
		t.Error("request-local Skill invocation must not accept a skill name")
	}
	if _, exists := props["run_id"]; exists {
		t.Error("request-local Skill invocation must not accept a run ID")
	}
	if _, exists := props["action"]; exists {
		t.Error("request-local Skill invocation must not accept an action selector")
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
	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("status without a request-local run binding outcome = %q, want blocked; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "request-local run binding") {
		t.Fatalf("status must be rejected without a request-local run binding, got %q", result.Text)
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
		"skill failed",
		" failed: runner crashed",
		"Failure: browser disconnected",
		"Exception: timeout waiting for selector",
		"Panic: nil pointer",
		"Tool error: missing dependency",
		"MCP call failed: runtime owner is missing",
		"[MCP ERROR] server=ews tool=find_person code=validation\nRequired parameter 'query' (string) is missing",
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

// The SubAgent authorizes a run against its matched set, but the host resolves
// the name again over every loaded skill and matches stable identities first,
// so a display name can be captured by a package that carries it as an alias.
// These tests pin that a reference covering several matched skills is refused,
// and that the name handed to the host is the matched entry's own identity.

func TestExecuteManageSkillBindsQualifiedIdentity(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "PDF Translator", QualifiedID: "acme.pdf-translator"},
		},
	}
	args := map[string]interface{}{"action": "run", "name": "pdf translator"}
	cb.executeManageSkill(args)

	if got := args["name"]; got != "pdf translator" {
		t.Fatalf("compatibility input must not be mutated, got %v", got)
	}
}

func TestExecuteManageSkillKeepsDisplayNameWithoutQualifiedID(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "Local Helper"},
		},
	}
	args := map[string]interface{}{"action": "run", "name": "local helper"}
	cb.executeManageSkill(args)

	// A skill with no stable identity has nothing better to travel as. The
	// canonical spelling still comes from the matched entry, not the model.
	if got := args["name"]; got != "local helper" {
		t.Fatalf("compatibility input must not be mutated, got %v", got)
	}
}

func TestExecuteManageSkillRefusesAmbiguousSkillName(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "Translator", QualifiedID: "acme.translator"},
			{Name: "Translator", QualifiedID: "other.translator"},
		},
	}
	result := cb.executeManageSkill(map[string]interface{}{"action": "run", "name": "Translator"})

	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("outcome = %v, want blocked", result.Outcome)
	}
	if !strings.Contains(result.Text, "ambiguous") ||
		!strings.Contains(result.Text, "acme.translator") ||
		!strings.Contains(result.Text, "other.translator") {
		t.Fatalf("ambiguity rejection should name both skills, got %q", result.Text)
	}
	if _, ok := cb.matchedSkill("Translator"); ok {
		t.Fatal("matchedSkill must fail closed on an ambiguous reference")
	}
}

func TestMatchedSkillAcceptsQualifiedIdentity(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "Translator", QualifiedID: "acme.translator"},
			{Name: "Linter", QualifiedID: "other.linter"},
		},
	}
	// A qualified id picks out one skill even though a colliding display name
	// would not.
	match, ok := cb.matchedSkill("acme.translator")
	if !ok {
		t.Fatal("a qualified id should resolve to its matched skill")
	}
	if match.Name != "Translator" {
		t.Fatalf("resolved skill = %q, want Translator", match.Name)
	}
}

func TestCodingSubAgentSkillQualifiedIDPrefersStableIdentity(t *testing.T) {
	cases := []struct {
		name string
		def  NLSkillDefinition
		want string
	}{
		{"skill id wins", NLSkillDefinition{Name: "T", SkillID: "a.t", HubSkillID: "hub_t", Publisher: "p"}, "a.t"},
		{"hub id next", NLSkillDefinition{Name: "T", HubSkillID: "hub_t", Publisher: "p"}, "hub_t"},
		{"publisher qualified last", NLSkillDefinition{Name: "T", Publisher: "p"}, "p:T"},
		{"display name only has none", NLSkillDefinition{Name: "T"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codingSubAgentSkillQualifiedID(tc.def); got != tc.want {
				t.Fatalf("qualified id = %q, want %q", got, tc.want)
			}
		})
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
