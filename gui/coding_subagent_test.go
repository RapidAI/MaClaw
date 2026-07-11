package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestCodingSubAgentStartFailuresReturnCompleteResult(t *testing.T) {
	var nilAgent *CodingSubAgent
	result := nilAgent.ExecuteTask(&TaskItem{Index: 1, Title: "Task"}, "", "", nil)
	if result.Status != TaskExecFailed || result.Error == "" || !strings.Contains(result.Summary, "任务运行错误") || result.QualityStatus != codingSubAgentQualityFailed || result.QualityIssueCount != 1 {
		t.Fatalf("nil subagent should return complete failed result, got %#v", result)
	}
	if !strings.Contains(result.Summary, "## 质量审计") || strings.Count(result.Summary, "## 质量审计") != 1 {
		t.Fatalf("nil subagent summary should include one quality audit section, got %q", result.Summary)
	}

	result = (&CodingSubAgent{}).ExecuteTask(nil, "", "", nil)
	if result.Status != TaskExecFailed || !strings.Contains(result.Error, "task is nil") || !strings.Contains(result.Summary, result.Error) || result.QualityStatus != codingSubAgentQualityFailed {
		t.Fatalf("nil task should return complete failed result, got %#v", result)
	}
	if !strings.Contains(result.Summary, "## 质量审计") || strings.Count(result.Summary, "## 质量审计") != 1 {
		t.Fatalf("nil task summary should include one quality audit section, got %q", result.Summary)
	}
}

func TestCodingSubAgentCodeSessionIDUsesLoopContext(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{loopCtx: &LoopContext{codeSessionID: "preview-session-42"}},
	}
	if got := cb.codeSessionID(); got != "preview-session-42" {
		t.Fatalf("codeSessionID = %q, want active loop session", got)
	}

	if got := (&codingSubAgentCallbacks{}).codeSessionID(); got != "subagent-workflow" {
		t.Fatalf("codeSessionID fallback = %q, want subagent-workflow", got)
	}
}

func TestBuildCodingSubAgentSystemPrompt_Minimal(t *testing.T) {
	task := &TaskItem{
		Index:       1,
		Title:       "实现 Player 类",
		Description: "实现玩家角色的移动和跳跃逻辑",
		Files:       []string{"src/player.h", "src/player.cpp"},
		AcceptanceCriteria: []string{
			"玩家可以左右移动",
			"玩家可以跳跃",
		},
	}

	prompt := buildCodingSubAgentSystemPrompt(task, "D:\\workprj\\morio", "", "", nil)

	// Should contain coding rules.
	if !strings.Contains(prompt, "编码执行器") {
		t.Error("prompt should contain coding executor role")
	}
	// Should contain project path.
	if !strings.Contains(prompt, "D:\\workprj\\morio") {
		t.Error("prompt should contain project path")
	}
	// Should contain platform info.
	if !strings.Contains(prompt, "平台:") {
		t.Error("prompt should contain platform info")
	}
	// Should contain edit_file/edit_lines strategy.
	if !strings.Contains(prompt, "edit_file") || !strings.Contains(prompt, "edit_lines") {
		t.Error("prompt should contain edit_file/edit_lines strategy")
	}
	if !strings.Contains(prompt, "codegraph explore") || !strings.Contains(prompt, "codegraph node") || !strings.Contains(prompt, ".codegraph/") {
		t.Error("prompt should prefer CodeGraph when an index is available")
	}
	if !strings.Contains(prompt, "ripgrep") || !strings.Contains(prompt, "Glob") {
		t.Error("prompt should keep Glob/ripgrep fallback before reading/editing")
	}
	if !strings.Contains(prompt, "git_diff") {
		t.Error("prompt should require git_diff self-check")
	}
	if !strings.Contains(prompt, "验证优先流程") || !strings.Contains(prompt, "test/build/lint/typecheck") {
		t.Error("prompt should prefer focused verification over mandatory report writing")
	}
	if strings.Contains(prompt, "每个任务必须执行") || strings.Contains(prompt, "将测试用例和测试结果写入") {
		t.Error("prompt should not require every task to write TEST_REPORT.md")
	}
	if !strings.Contains(prompt, "Single-task contract") || !strings.Contains(prompt, "Avoid broad refactors") {
		t.Error("prompt should contain explicit single-task scope contract")
	}
	if !strings.Contains(prompt, "Quality audit gates") || !strings.Contains(prompt, "hard gates") || !strings.Contains(prompt, "explore before existing-file edits") {
		t.Error("prompt should describe enforced quality hard gates")
	}
	if !strings.Contains(prompt, "no-change tasks") || !strings.Contains(prompt, "project-context evidence for new files") {
		t.Error("prompt should describe no-change and created-file evidence requirements")
	}
	if !strings.Contains(prompt, "actual modified/created file paths") || !strings.Contains(prompt, "map every acceptance criterion") || !strings.Contains(prompt, "scope expansion") || !strings.Contains(prompt, "remaining risk") {
		t.Error("prompt should describe final summary audit requirements")
	}
	if !strings.Contains(prompt, "verification commands really run/passed") || !strings.Contains(prompt, "no tests found") || !strings.Contains(prompt, "0 tests") {
		t.Error("prompt should prevent unsupported verification claims and empty-success verification")
	}
	if !strings.Contains(prompt, "fresh after the final edit") || !strings.Contains(prompt, "real execution output") || !strings.Contains(prompt, "pass/fail outcome") {
		t.Error("prompt should require fresh verification with non-empty execution evidence and an outcome")
	}
	if !strings.Contains(prompt, "(无输出)") || !strings.Contains(prompt, "no tests collected") || !strings.Contains(prompt, "[no test files]") || !strings.Contains(prompt, "list/help/collect-only/dry-run") {
		t.Error("prompt should enumerate weak verification outputs that do not count")
	}
	if !strings.Contains(prompt, "Tool-call JSON reliability") || !strings.Contains(prompt, "split it into chunks") || !strings.Contains(prompt, `mode="append"`) {
		t.Error("prompt should instruct subagent to keep tool-call JSON valid and split large write_file content")
	}
	if !strings.Contains(prompt, "tool error text as authoritative recovery guidance") || !strings.Contains(prompt, "do not repeat an identical failed tool call or command") || !strings.Contains(prompt, "change the approach before retrying") {
		t.Error("prompt should instruct subagent to recover from tool errors instead of repeating failed calls")
	}
	if !strings.Contains(prompt, "unrelated pre-existing errors") || !strings.Contains(prompt, "exact blocker") {
		t.Error("prompt should instruct the agent to report unrelated verification blockers precisely")
	}
	if !strings.Contains(prompt, "禁止用 write_file 重写已有文件") {
		t.Error("prompt should contain write_file prohibition for existing files")
	}
	if !strings.Contains(prompt, "项目路径内") || !strings.Contains(prompt, "不要读取项目外文件") {
		t.Error("prompt should describe project read/search boundary")
	}
	if !strings.Contains(prompt, "git reset --hard") || !strings.Contains(prompt, "Remove-Item -Recurse") {
		t.Error("prompt should describe dangerous command guardrails")
	}
	if !strings.Contains(prompt, "Git commands that rewrite") || !strings.Contains(prompt, "merge") || !strings.Contains(prompt, "apply") || !strings.Contains(prompt, "cherry-pick") || !strings.Contains(prompt, "update-index") || !strings.Contains(prompt, "Remove-Item -Recurse/-r/-rf") {
		t.Error("prompt should describe expanded command guardrails")
	}
	if !strings.Contains(prompt, "shell helpers") || !strings.Contains(prompt, "Set-Content") || !strings.Contains(prompt, "Node fs") || !strings.Contains(prompt, "Python open") || !strings.Contains(prompt, "Path write/touch/rename/remove") {
		t.Error("prompt should describe shell file mutation guardrails")
	}
	if !strings.Contains(prompt, "working_dir 必须在项目路径内") {
		t.Error("prompt should describe bash working directory boundary")
	}
	if !strings.Contains(prompt, "timeout/gtimeout") || !strings.Contains(prompt, "cmd /c") || !strings.Contains(prompt, "powershell -Command") || !strings.Contains(prompt, "bash -lc") {
		t.Error("prompt should identify safe verification wrappers")
	}
	if !strings.Contains(prompt, "|| true") || !strings.Contains(prompt, "pipes") || !strings.Contains(prompt, "output redirection") || !strings.Contains(prompt, "--fix/--write") || !strings.Contains(prompt, "chained post-verification commands") {
		t.Error("prompt should identify shell forms that make verification non-auditable")
	}
	// Should NOT contain IM rules, memory, browser, SSH, etc.
	for _, noise := range []string{"飞书", "微信", "QQ", "Browser:", "SSH", "memory", "screenshot", "IM 通道"} {
		if strings.Contains(prompt, noise) {
			t.Errorf("prompt should NOT contain non-coding noise: %q", noise)
		}
	}
}

func TestBuildCodingSubAgentSystemPrompt_WithContext(t *testing.T) {
	task := &TaskItem{Index: 2, Title: "实现关卡加载"}

	longReq := strings.Repeat("需求内容。", 200) // ~1000 chars
	longDesign := strings.Repeat("设计内容。", 200)
	prevOutputs := []string{"src/player.h (已完成)", "src/player.cpp (已完成)"}

	prompt := buildCodingSubAgentSystemPrompt(task, "/project", longReq, longDesign, prevOutputs)

	// Context should be truncated.
	if len([]rune(prompt)) > 5000 {
		t.Errorf("prompt too long: %d runes, expected <5000", len([]rune(prompt)))
	}
	// Previous outputs are injected into the task user message, not duplicated in
	// the system prompt.
	if strings.Contains(prompt, "src/player.h") {
		t.Error("system prompt should not duplicate previous task outputs")
	}
}

func TestCodingSubAgentSystemPromptCachesDynamicSelections(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Index: 1, Title: "Polish UI", Description: "Improve frontend progress status"},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "impeccable", Description: "Audit and polish frontend UI", Score: 0.9, RequiredArgs: []string{"input"}},
		},
		matchedMCPTools: []codingSubAgentMCPToolMatch{
			{ServerID: "browser", ServerName: "browser", ToolName: "screenshot", Description: "Capture browser screenshot", Score: 0.8, RequiredArgs: []string{"url"}},
		},
	}

	first := cb.BuildSystemPrompt("first turn", true)
	if !cb.matchedSkillsSelected || !cb.matchedMCPToolsSelected {
		t.Fatalf("dynamic selections should be marked selected after first prompt build")
	}
	if !strings.Contains(first, "impeccable") || !strings.Contains(first, "screenshot") || !strings.Contains(first, "server: browser") {
		t.Fatalf("prompt should include preselected skill and MCP sections, got %q", first)
	}

	cb.matchedSkills = nil
	cb.matchedMCPTools = nil
	second := cb.BuildSystemPrompt("later turn", false)
	if second != first {
		t.Fatalf("system prompt should be cached across turns even if match fields change")
	}
}

func TestCodingSubAgentBuildToolsCachesDynamicToolDefinitions(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		task:     &TaskItem{Index: 1, Title: "Use dynamic helpers"},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "impeccable", Description: "Audit and polish frontend UI", Score: 0.9, RequiredArgs: []string{"input"}},
		},
		matchedMCPTools: []codingSubAgentMCPToolMatch{
			{ServerID: "browser", ServerName: "browser", ToolName: "screenshot", Description: "Capture browser screenshot", Score: 0.8, RequiredArgs: []string{"url"}},
		},
	}

	first := cb.BuildTools("first turn")
	firstNames := codingSubAgentToolDefinitionNamesForTest(first)
	if !containsStringForTest(firstNames, "manage_skill") || !containsStringForTest(firstNames, "call_mcp_tool") {
		t.Fatalf("tools should include dynamic manage_skill and call_mcp_tool definitions, got %#v", firstNames)
	}
	if len(first) == 0 {
		t.Fatal("expected at least one tool definition")
	}
	if fn, _ := first[0]["function"].(map[string]interface{}); fn != nil {
		fn["name"] = "mutated_external_tool_name"
	}

	cb.matchedSkills = nil
	cb.matchedMCPTools = nil
	second := cb.BuildTools("later turn")
	if len(second) != len(first) {
		t.Fatalf("cached tool list length changed after matched tools were cleared: first=%d second=%d", len(first), len(second))
	}
	secondNames := codingSubAgentToolDefinitionNamesForTest(second)
	if strings.Join(secondNames, "\n") != strings.Join(firstNames, "\n") {
		t.Fatalf("cached tool names changed after matched tools were cleared: first=%#v second=%#v", firstNames, secondNames)
	}
	if containsStringForTest(secondNames, "mutated_external_tool_name") {
		t.Fatalf("BuildTools should return a defensive copy of cached tool definitions, got %#v", secondNames)
	}
}

func TestCodingSubAgentDynamicSelectionTextCachesTaskSnapshot(t *testing.T) {
	task := &TaskItem{
		Title:              "Polish progress UI",
		Description:        "Use screenshots to verify failed status rendering",
		Files:              []string{"gui/frontend/src/components/ai/CodingAgentProgressStatus.tsx"},
		AcceptanceCriteria: []string{"Quality failed state is visible"},
	}
	cb := &codingSubAgentCallbacks{
		task:        task,
		reqCtx:      "User requires browser screenshot evidence for the progress UI.",
		designCtx:   "Design says failed status uses a red outline and audit detail row.",
		prevOutputs: []string{"Previous task touched gui/frontend/src/components/ai/CodingAgentTimeline.tsx"},
	}

	first := cb.dynamicSelectionText()
	task.Title = "Changed title"
	task.Description = "Changed description"
	task.Files = []string{"other.go"}
	task.AcceptanceCriteria = []string{"Different criterion"}
	cb.reqCtx = "Changed requirement"
	cb.designCtx = "Changed design"
	cb.prevOutputs = []string{"Changed previous output"}
	second := cb.dynamicSelectionText()

	if first == "" {
		t.Fatal("expected non-empty dynamic selection text")
	}
	for _, want := range []string{"browser screenshot evidence", "red outline", "CodingAgentTimeline.tsx"} {
		if !strings.Contains(first, want) {
			t.Fatalf("dynamic selection text should include task context %q: %q", want, first)
		}
	}
	if second != first {
		t.Fatalf("dynamic selection text should be cached per task: first=%q second=%q", first, second)
	}
	if !cb.dynamicSelectionTextBuilt {
		t.Fatal("dynamic selection text cache should be marked built")
	}
}

func TestCloneCodingSubAgentToolDefinitionsDeepCopiesTypedSchemaContainers(t *testing.T) {
	original := []map[string]interface{}{
		{
			"function": map[string]interface{}{
				"name": "typed_schema_tool",
				"parameters": map[string]interface{}{
					"oneOf": []map[string]interface{}{
						{"required": []string{"path"}},
					},
					"dependentRequired": map[string][]string{
						"action": {"path"},
					},
				},
			},
		},
	}

	cloned := cloneCodingSubAgentToolDefinitions(original)
	params := cloned[0]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	params["oneOf"].([]map[string]interface{})[0]["required"].([]string)[0] = "mutated"
	params["dependentRequired"].(map[string][]string)["action"][0] = "also_mutated"

	originalParams := original[0]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if got := originalParams["oneOf"].([]map[string]interface{})[0]["required"].([]string)[0]; got != "path" {
		t.Fatalf("typed []map schema was not deep-copied, original required = %q", got)
	}
	if got := originalParams["dependentRequired"].(map[string][]string)["action"][0]; got != "path" {
		t.Fatalf("typed map[string][]string schema was not deep-copied, original dependentRequired = %q", got)
	}
}

func codingSubAgentToolDefinitionNamesForTest(tools []map[string]interface{}) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func containsStringForTest(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestCodingSubAgentPromptCapsTaskLists(t *testing.T) {
	var files []string
	for i := 0; i < codingSubAgentTaskFilesMax+2; i++ {
		files = append(files, fmt.Sprintf("src/file_%02d.go", i))
	}
	var criteria []string
	for i := 0; i < codingSubAgentAcceptanceCriteriaMax+3; i++ {
		criteria = append(criteria, fmt.Sprintf("criteria %02d", i))
	}
	task := &TaskItem{Index: 3, Title: "Large task", Files: files, AcceptanceCriteria: criteria}
	cb := &codingSubAgentCallbacks{task: task}

	userMsg := cb.buildTaskUserMessage()
	if strings.Contains(userMsg, "src/file_31.go") {
		t.Fatalf("task files should be capped, got %q", userMsg)
	}
	if !strings.Contains(userMsg, "还有 2 项未展开") {
		t.Fatalf("task files should report remaining count, got %q", userMsg)
	}
	if strings.Contains(userMsg, "criteria 22") {
		t.Fatalf("acceptance criteria should be capped, got %q", userMsg)
	}
	if !strings.Contains(userMsg, "AC1/标准1: criteria 00") || !strings.Contains(userMsg, "还有 3 项未展开") {
		t.Fatalf("acceptance criteria should be numbered and report remaining count, got %q", userMsg)
	}
}

func TestCodingSubAgentTaskUserMessageIncludesPreflightChecklist(t *testing.T) {
	task := &TaskItem{Index: 1, Title: "Fix handler", Description: "Resolve request failure"}
	cb := &codingSubAgentCallbacks{task: task}

	userMsg := cb.buildTaskUserMessage()
	for _, want := range []string{
		"Before editing",
		"Before finalizing",
		".codegraph/",
		"codegraph explore",
		"codegraph node",
		"Glob/ripgrep/read_file",
		"risk/impact",
		"minimal edit",
		"retry context",
		"After the last edit",
		"Do not present pre-edit verification as final verification",
		"actual modified/created file paths",
		"verification commands you really ran after editing",
		"acceptance criteria",
		"scope expansion",
		"remaining risk",
	} {
		if !strings.Contains(userMsg, want) {
			t.Fatalf("task user message missing %q: %q", want, userMsg)
		}
	}
	if strings.Contains(userMsg, "验收验证要求") {
		t.Fatalf("task without acceptance criteria should not add acceptance verification noise: %q", userMsg)
	}
	beforeEditingStart := strings.Index(userMsg, "Before editing")
	beforeFinalizingStart := strings.Index(userMsg, "Before finalizing")
	if beforeEditingStart < 0 || beforeFinalizingStart < 0 || beforeFinalizingStart <= beforeEditingStart {
		t.Fatalf("task user message should separate pre-edit and finalization checklists: %q", userMsg)
	}
	beforeEditingSection := userMsg[beforeEditingStart:beforeFinalizingStart]
	if strings.Contains(beforeEditingSection, "Run matching verification") || strings.Contains(beforeEditingSection, "test/build/lint/typecheck") {
		t.Fatalf("pre-edit checklist should not ask for final verification before edits: %q", beforeEditingSection)
	}
}

func TestCodingSubAgentTaskUserMessageIncludesPreviousOutputs(t *testing.T) {
	task := &TaskItem{Index: 2, Title: "Wire follow-up"}
	cb := &codingSubAgentCallbacks{
		task: task,
		prevOutputs: []string{
			"Previous passed task summary: implemented quality audit result propagation",
			"Previous task file output: gui/coding_subagent_orchestrator.go",
		},
	}

	userMsg := cb.buildTaskUserMessage()
	for _, want := range []string{
		"前置任务上下文",
		"Previous passed task summary",
		"quality audit result propagation",
		"gui/coding_subagent_orchestrator.go",
	} {
		if !strings.Contains(userMsg, want) {
			t.Fatalf("task user message missing previous output %q: %q", want, userMsg)
		}
	}
	if strings.Contains(userMsg, "验收验证要求") {
		t.Fatalf("task without acceptance criteria should not add acceptance verification noise: %q", userMsg)
	}
}

func TestCodingSubAgentTaskUserMessageRequiresAcceptanceCriteriaVerification(t *testing.T) {
	task := &TaskItem{
		Index:       2,
		Title:       "Fix handler",
		Description: "Resolve request failure",
		AcceptanceCriteria: []string{
			"returns 200 for valid payload",
			"returns 400 for invalid payload",
		},
	}
	cb := &codingSubAgentCallbacks{task: task}

	userMsg := cb.buildTaskUserMessage()
	for _, want := range []string{"**验收标准**", "AC1/标准1", "returns 200", "AC2/标准2", "returns 400", "验收验证要求", "对应到上述验收标准", "无法自动验证"} {
		if !strings.Contains(userMsg, want) {
			t.Fatalf("task user message missing acceptance verification hint %q: %q", want, userMsg)
		}
	}
}

func TestCodingSubAgentPromptCapsTitleAndDescription(t *testing.T) {
	task := &TaskItem{
		Index:       5,
		Title:       "Task " + strings.Repeat("very long title ", 40),
		Description: strings.Repeat("description line\n", codingSubAgentTaskDescriptionMaxRunes),
	}
	cb := &codingSubAgentCallbacks{task: task}

	userMsg := cb.buildTaskUserMessage()
	if !strings.Contains(userMsg, "截断") {
		t.Fatalf("expected long title/description to be truncated, got %q", userMsg)
	}
	if strings.Contains(userMsg, strings.Repeat("very long title ", 20)) {
		t.Fatalf("expected task title to be compacted, got %q", userMsg)
	}
	if len([]rune(userMsg)) > codingSubAgentTaskDescriptionMaxRunes+codingSubAgentTaskTitleMaxRunes+1000 {
		t.Fatalf("task user message too long: %d", len([]rune(userMsg)))
	}
}

func TestCodingSubAgentTaskUserMessageCapsPreviousOutputs(t *testing.T) {
	var prevOutputs []string
	for i := 0; i < codingSubAgentPrevOutputsMax+4; i++ {
		prevOutputs = append(prevOutputs, fmt.Sprintf("src/previous_%02d.go (modified)", i))
	}
	cb := &codingSubAgentCallbacks{
		task:        &TaskItem{Index: 4, Title: "Next"},
		prevOutputs: prevOutputs,
	}

	userMsg := cb.buildTaskUserMessage()
	if strings.Contains(userMsg, "src/previous_23.go") {
		t.Fatalf("previous outputs should be capped, got %q", userMsg)
	}
	if !strings.Contains(userMsg, "前置任务上下文") || !strings.Contains(userMsg, "还有 4 项未展开") {
		t.Fatalf("previous outputs should report remaining count in task message, got %q", userMsg)
	}
}

func TestBuildCodingToolDefinitions_OnlyCodingTools(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()

	if len(tools) != 9 {
		t.Fatalf("expected 9 coding tools, got %d", len(tools))
	}

	expectedNames := map[string]bool{
		"Glob":           true,
		"ripgrep":        true,
		"read_file":      true,
		"write_file":     true,
		"edit_file":      true,
		"edit_lines":     true,
		"bash":           true,
		"list_directory": true,
		"git_diff":       true,
	}

	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !expectedNames[name] {
			t.Errorf("unexpected tool: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		t.Errorf("missing tools: %v", expectedNames)
	}
}

func TestBuildCodingToolDefinitions_WriteFileKeepsChunkingHints(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()
	assertCodingWriteFileKeepsChunkingHints(t, tools)
	assertCodingEditToolsKeepChunkingHints(t, tools)
}
func TestBuildCodingToolDefinitions_BashDescribesSubAgentGuardrails(t *testing.T) {
	for name, tools := range map[string][]map[string]interface{}{
		"fallback": buildCodingToolDefinitionsFallback(),
		"registry": buildCodingToolDefinitionsFromRegistry(&IMMessageHandler{}),
	} {
		t.Run(name, func(t *testing.T) {
			bash := codingToolDefinitionForTest(t, tools, "bash")
			desc, _ := bash["description"].(string)
			for _, want := range []string{"read-only diagnostics", "test/build/lint/typecheck", "Do not use bash to edit files", "stage/commit/apply patches", "git_diff"} {
				if !strings.Contains(desc, want) {
					t.Fatalf("bash description missing %q: %q", want, desc)
				}
			}
		})
	}
}
func TestBuildCodingToolDefinitions_ReadFileExposesLineRangeHints(t *testing.T) {
	for name, tools := range map[string][]map[string]interface{}{
		"fallback": buildCodingToolDefinitionsFallback(),
		"registry": buildCodingToolDefinitionsFromRegistry(&IMMessageHandler{}),
	} {
		t.Run(name, func(t *testing.T) {
			readFile := codingToolDefinitionForTest(t, tools, "read_file")
			params, _ := readFile["parameters"].(map[string]interface{})
			props, _ := params["properties"].(map[string]interface{})
			offset, ok := props["offset"]
			if !ok {
				t.Fatal("read_file should expose offset for tail reads")
			}
			if got := codingToolPropTypeForTest(offset); got != "integer" {
				t.Fatalf("read_file offset type = %q, want integer", got)
			}
			desc := codingToolPropDescriptionForTest(offset)
			if !strings.Contains(desc, "last N lines") {
				t.Fatalf("read_file offset description should explain tail reads, got %q", desc)
			}
			linesDesc := codingToolPropDescriptionForTest(props["lines"])
			if !strings.Contains(linesDesc, "limit/num_lines/line_count") || !strings.Contains(linesDesc, "maximum 2000") {
				t.Fatalf("read_file lines description should expose aliases, got %q", linesDesc)
			}
			startDesc := codingToolPropDescriptionForTest(props["start_line"])
			if !strings.Contains(startDesc, "start/startLine") {
				t.Fatalf("read_file start_line description should expose aliases, got %q", startDesc)
			}
		})
	}
}

func TestBuildCodingToolDefinitionsFromRegistry_WriteFileKeepsChunkingHints(t *testing.T) {
	tools := buildCodingToolDefinitionsFromRegistry(&IMMessageHandler{})
	assertCodingWriteFileKeepsChunkingHints(t, tools)
}

func TestLoopCycleBuildTools_WriteFileKeepsChunkingHints(t *testing.T) {
	cb := &loopCycleCallbacks{parent: &guiLoopCommandCallbacks{}}
	tools := cb.BuildTools("fix")
	assertCodingWriteFileKeepsChunkingHints(t, tools)

	bash := codingToolDefinitionForTest(t, tools, "bash")
	desc, _ := bash["description"].(string)
	for _, want := range []string{"read-only diagnostics", "Do not use bash to edit files", "stage/commit/apply patches", "run the verify command"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("loop cycle bash description missing %q: %q", want, desc)
		}
	}

	prompt := cb.BuildSystemPrompt("fix", true)
	if !strings.Contains(prompt, "1800") || !strings.Contains(prompt, "overwrite + append") {
		t.Fatalf("loop cycle prompt should include write_file chunking hint, got %q", prompt)
	}
}

func codingToolDefinitionForTest(t *testing.T, tools []map[string]interface{}, toolName string) map[string]interface{} {
	t.Helper()
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if name, _ := fn["name"].(string); name == toolName {
			return fn
		}
	}
	t.Fatalf("missing %s tool", toolName)
	return nil
}

func assertCodingWriteFileKeepsChunkingHints(t *testing.T, tools []map[string]interface{}) {
	t.Helper()
	writeFile := codingToolDefinitionForTest(t, tools, "write_file")
	params, _ := writeFile["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	contentDesc := codingToolPropDescriptionForTest(props["content"])
	modeDesc := codingToolPropDescriptionForTest(props["mode"])
	if !strings.Contains(contentDesc, "No length limit") {
		t.Fatalf("write_file content description should state no length limit, got %q", contentDesc)
	}
	// write_file should NOT have maxLength — removed to prevent LLM from avoiding the tool.
	if got := codingToolPropMaxLengthForTest(props["content"]); got != 0 {
		t.Fatalf("write_file content should not have maxLength, got %d", got)
	}
	if !strings.Contains(modeDesc, "overwrite") || !strings.Contains(modeDesc, "append") {
		t.Fatalf("write_file mode description should keep overwrite/append hint, got %q", modeDesc)
	}
}

func assertCodingEditToolsKeepChunkingHints(t *testing.T, tools []map[string]interface{}) {
	t.Helper()
	byName := map[string]map[string]interface{}{}
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if name, _ := fn["name"].(string); name != "" {
			byName[name] = fn
		}
	}
	editFile := byName["edit_file"]
	if editFile == nil {
		t.Fatal("missing edit_file tool")
	}
	editFileParams, _ := editFile["parameters"].(map[string]interface{})
	editFileProps, _ := editFileParams["properties"].(map[string]interface{})
	for _, propName := range []string{"old_string", "new_string"} {
		desc := codingToolPropDescriptionForTest(editFileProps[propName])
		if !strings.Contains(desc, "1800") {
			t.Fatalf("edit_file %s description should keep chunking hint, got %q", propName, desc)
		}
		if got := codingToolPropMaxLengthForTest(editFileProps[propName]); got != codingSubAgentInlineContentLimit {
			t.Fatalf("edit_file %s maxLength = %d, want %d", propName, got, codingSubAgentInlineContentLimit)
		}
	}
	editLines := byName["edit_lines"]
	if editLines == nil {
		t.Fatal("missing edit_lines tool")
	}
	editLinesParams, _ := editLines["parameters"].(map[string]interface{})
	editLinesProps, _ := editLinesParams["properties"].(map[string]interface{})
	desc := codingToolPropDescriptionForTest(editLinesProps["content"])
	if !strings.Contains(desc, "1800") || !strings.Contains(desc, "split") {
		t.Fatalf("edit_lines content description should keep chunking hint, got %q", desc)
	}
	if got := codingToolPropMaxLengthForTest(editLinesProps["content"]); got != codingSubAgentInlineContentLimit {
		t.Fatalf("edit_lines content maxLength = %d, want %d", got, codingSubAgentInlineContentLimit)
	}
}

func codingToolPropTypeForTest(raw interface{}) string {
	switch prop := raw.(type) {
	case map[string]interface{}:
		typeName, _ := prop["type"].(string)
		return typeName
	case map[string]string:
		return prop["type"]
	default:
		return ""
	}
}
func codingToolPropDescriptionForTest(raw interface{}) string {
	switch prop := raw.(type) {
	case map[string]interface{}:
		desc, _ := prop["description"].(string)
		return desc
	case map[string]string:
		return prop["description"]
	default:
		return ""
	}
}

func codingToolPropMaxLengthForTest(raw interface{}) int {
	prop, ok := raw.(map[string]interface{})
	if !ok {
		return 0
	}
	switch v := prop["maxLength"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func TestRejectInvalidCodingSubAgentToolArgumentsGuidesChunkedRetry(t *testing.T) {
	result, rejected := rejectInvalidCodingSubAgentToolArguments("write_file", `{"path":"a.go","content":"unterminated`)
	if !rejected {
		t.Fatal("expected invalid JSON arguments to be rejected")
	}
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("outcome = %s, want failed", result.Outcome)
	}
	if strings.Contains(result.Text, "argument parse failed") {
		t.Fatalf("should not expose generic parse failure: %q", result.Text)
	}
	if !strings.Contains(result.Text, "invalid JSON object arguments") || !strings.Contains(result.Text, "unexpected end of JSON input") || !strings.Contains(result.Text, "split it into smaller chunks") {
		t.Fatalf("result should guide valid JSON and chunked retry: %q", result.Text)
	}
}

func TestRejectInvalidCodingSubAgentToolArgumentsDetectsTruncatedLargePayload(t *testing.T) {
	result, rejected := rejectInvalidCodingSubAgentToolArguments("write_file", `{"path":"a.go","content":"`+strings.Repeat("x", 9000))
	if !rejected {
		t.Fatal("expected truncated large JSON arguments to be rejected")
	}
	if !strings.Contains(result.Text, "appears truncated") || !strings.Contains(result.Text, "smaller chunks") {
		t.Fatalf("result should include truncated/chunk guidance: %q", result.Text)
	}
}

func TestRejectInvalidCodingSubAgentToolArgumentsDetectsContentPayloadTruncation(t *testing.T) {
	result, rejected := rejectInvalidCodingSubAgentToolArguments("edit_file", `{"new_string":"func TestX(t *testing.T) {`)
	if !rejected {
		t.Fatal("expected content-bearing truncated JSON arguments to be rejected")
	}
	if !strings.Contains(result.Text, "appears truncated") || !strings.Contains(result.Text, "edit_file/edit_lines") {
		t.Fatalf("result should include truncated edit guidance: %q", result.Text)
	}
}

func TestRejectInvalidCodingSubAgentToolArgumentsRejectsNonObjectJSON(t *testing.T) {
	for _, args := range []string{`[]`, `null`, `"text"`} {
		result, rejected := rejectInvalidCodingSubAgentToolArguments("read_file", args)
		if !rejected {
			t.Fatalf("expected non-object JSON arguments %s to be rejected", args)
		}
		if strings.Contains(result.Text, "argument parse failed") {
			t.Fatalf("should not expose generic parse failure for %s: %q", args, result.Text)
		}
		if !strings.Contains(result.Text, "invalid JSON object arguments") {
			t.Fatalf("result should explain JSON object arguments for %s: %q", args, result.Text)
		}
	}
}

func TestCodingSubAgentToolArgumentErrorsIncludeValidExamples(t *testing.T) {
	jsonResult := invalidCodingSubAgentToolArgumentsResult("write_file", `{"path":"a.go","content":"unterminated`, fmt.Errorf("unexpected end of JSON input"))
	if !strings.Contains(jsonResult.Text, "Example valid arguments:") || !strings.Contains(jsonResult.Text, `{"path":"src/new_file.go","content":"package main\n"}`) {
		t.Fatalf("invalid JSON error should include a write_file example, got %q", jsonResult.Text)
	}

	missing := missingCodingSubAgentRequiredArgumentResult("read_file", "path")
	if !strings.Contains(missing.Text, "Example valid arguments:") || !strings.Contains(missing.Text, `{"path":"src/main.go"}`) {
		t.Fatalf("missing argument error should include a read_file example, got %q", missing.Text)
	}

	typeErr := invalidCodingSubAgentArgumentTypeResult("list_directory", "path", "string", "number", "a JSON string")
	if !strings.Contains(typeErr.Text, "Example valid arguments:") || !strings.Contains(typeErr.Text, `{"path":"."}`) {
		t.Fatalf("type error should include a list_directory example, got %q", typeErr.Text)
	}
	if !strings.Contains(typeErr.Text, "dir/directory/root -> path") || !strings.Contains(typeErr.Text, "filename/target_path") {
		t.Fatalf("list_directory argument error should include path and directory alias recovery hints, got %q", typeErr.Text)
	}

	editMissing := missingCodingSubAgentRequiredArgumentResult("edit_file", "old_string")
	if !strings.Contains(editMissing.Text, "old_content/find/search -> old_string") ||
		!strings.Contains(editMissing.Text, "new_content/replace/replacement -> new_string") ||
		!strings.Contains(editMissing.Text, "filename/target_path") {
		t.Fatalf("edit_file argument error should include edit alias recovery hints, got %q", editMissing.Text)
	}

	allowed := invalidCodingSubAgentArgumentAllowedValuesResult("edit_lines", "operation", "move", []string{"replace", "insert", "delete"})
	if !strings.Contains(allowed.Text, "Example valid arguments:") || !strings.Contains(allowed.Text, `{"path":"src/main.go","operation":"replace"`) {
		t.Fatalf("allowed-values error should include an edit_lines example, got %q", allowed.Text)
	}
	if !strings.Contains(allowed.Text, "action/op -> operation") || !strings.Contains(allowed.Text, "start/startLine -> start_line") {
		t.Fatalf("edit_lines argument error should include alias recovery hints, got %q", allowed.Text)
	}

	skillTypeErr := invalidCodingSubAgentArgumentTypeResult("manage_skill", "args", "object", "string", "a JSON object")
	if !strings.Contains(skillTypeErr.Text, `{"action":"run","name":"skill-name","args":{"input":"task-specific instructions"}}`) {
		t.Fatalf("manage_skill type error should include args object example, got %q", skillTypeErr.Text)
	}

	mcpMissing := missingCodingSubAgentRequiredArgumentResult("call_mcp_tool", "server_id")
	if !strings.Contains(mcpMissing.Text, "server/server_name -> server_id") || !strings.Contains(mcpMissing.Text, "args/params/input -> arguments") {
		t.Fatalf("call_mcp_tool argument error should include alias recovery hints, got %q", mcpMissing.Text)
	}
}

func TestCodingSubAgentEmptyToolArgumentsNormalizeToObject(t *testing.T) {
	if got := normalizeCodingSubAgentToolArguments(""); got != "{}" {
		t.Fatalf("empty args normalize to %q, want {}", got)
	}
	if got := normalizeCodingSubAgentToolArguments(" \n\t "); got != "{}" {
		t.Fatalf("blank args normalize to %q, want {}", got)
	}

	cb := &codingSubAgentCallbacks{}
	result := cb.executeToolWithOutcome("read_file", "")
	if strings.Contains(result.Text, "argument parse failed") || strings.Contains(result.Text, "unexpected end of JSON input") {
		t.Fatalf("empty arguments should not surface JSON parse failure: %q", result.Text)
	}
}

func TestCodingSubAgentToolArgumentAliasesNormalizeCommonModelFields(t *testing.T) {
	normalized := normalizeCodingSubAgentToolArgumentsForTool("bash", `{"command":"go test ./gui","work_dir":"gui","cwd":"ignored"}`)
	var bashArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &bashArgs); err != nil {
		t.Fatalf("normalized bash args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := bashArgs["working_dir"].(string); got != "gui" {
		t.Fatalf("bash work_dir alias should populate working_dir, got %#v from %s", bashArgs, normalized)
	}
	if _, ok := bashArgs["work_dir"]; ok {
		t.Fatalf("bash work_dir alias should be removed after normalization: %#v", bashArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("edit_file", `{"path":"main.go","old_content":"old","new_content":"new"}`)
	var editArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &editArgs); err != nil {
		t.Fatalf("normalized edit_file args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := editArgs["old_string"].(string); got != "old" {
		t.Fatalf("edit_file old_content alias should populate old_string, got %#v from %s", editArgs, normalized)
	}
	if got, _ := editArgs["new_string"].(string); got != "new" {
		t.Fatalf("edit_file new_content alias should populate new_string, got %#v from %s", editArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("edit_file", `{"path":"main.go","find":"old","replace":"new"}`)
	editArgs = nil
	if err := json.Unmarshal([]byte(normalized), &editArgs); err != nil {
		t.Fatalf("normalized edit_file find/replace args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := editArgs["old_string"].(string); got != "old" {
		t.Fatalf("edit_file find alias should populate old_string, got %#v from %s", editArgs, normalized)
	}
	if got, _ := editArgs["new_string"].(string); got != "new" {
		t.Fatalf("edit_file replace alias should populate new_string, got %#v from %s", editArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("edit_file", `{"path":"main.go","search":"old","replacement":"new"}`)
	editArgs = nil
	if err := json.Unmarshal([]byte(normalized), &editArgs); err != nil {
		t.Fatalf("normalized edit_file search/replacement args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := editArgs["old_string"].(string); got != "old" {
		t.Fatalf("edit_file search alias should populate old_string, got %#v from %s", editArgs, normalized)
	}
	if got, _ := editArgs["new_string"].(string); got != "new" {
		t.Fatalf("edit_file replacement alias should populate new_string, got %#v from %s", editArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("edit_file", `{"path":"main.go","old_string":"canonical","old_content":"alias","new_string":"done"}`)
	editArgs = nil
	if err := json.Unmarshal([]byte(normalized), &editArgs); err != nil {
		t.Fatalf("normalized edit_file canonical args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := editArgs["old_string"].(string); got != "canonical" {
		t.Fatalf("canonical old_string should not be overwritten by alias, got %#v", editArgs)
	}
	if _, ok := editArgs["old_content"]; ok {
		t.Fatalf("edit_file old_content alias should be removed when canonical is present: %#v", editArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("grep_search", `{"query":"TODO","file_path":"gui"}`)
	var grepArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &grepArgs); err != nil {
		t.Fatalf("normalized grep_search args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := grepArgs["pattern"].(string); got != "TODO" {
		t.Fatalf("grep_search query alias should populate pattern, got %#v from %s", grepArgs, normalized)
	}
	if got, _ := grepArgs["path"].(string); got != "gui" {
		t.Fatalf("grep_search file_path alias should populate path, got %#v from %s", grepArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("read_file", `{"file":"main.go","path":"canonical.go"}`)
	var readArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &readArgs); err != nil {
		t.Fatalf("normalized read_file args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := readArgs["path"].(string); got != "canonical.go" {
		t.Fatalf("canonical path should not be overwritten by file alias, got %#v", readArgs)
	}
	if _, ok := readArgs["file"]; ok {
		t.Fatalf("read_file file alias should be removed when canonical is present: %#v", readArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("read_file", `{"path":"main.go","limit":40,"start":12}`)
	readArgs = nil
	if err := json.Unmarshal([]byte(normalized), &readArgs); err != nil {
		t.Fatalf("normalized read_file range args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := readArgs["lines"].(float64); got != 40 {
		t.Fatalf("read_file limit alias should populate lines, got %#v from %s", readArgs, normalized)
	}
	if got, _ := readArgs["start_line"].(float64); got != 12 {
		t.Fatalf("read_file start alias should populate start_line, got %#v from %s", readArgs, normalized)
	}
	if _, ok := readArgs["limit"]; ok {
		t.Fatalf("read_file limit alias should be removed after normalization: %#v", readArgs)
	}
	if _, ok := readArgs["start"]; ok {
		t.Fatalf("read_file start alias should be removed after normalization: %#v", readArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("read_file", `{"path":"main.go","lines":20,"limit":40,"start_line":3,"start":12}`)
	readArgs = nil
	if err := json.Unmarshal([]byte(normalized), &readArgs); err != nil {
		t.Fatalf("normalized canonical read_file range args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := readArgs["lines"].(float64); got != 20 {
		t.Fatalf("canonical read_file lines should not be overwritten by limit alias, got %#v", readArgs)
	}
	if got, _ := readArgs["start_line"].(float64); got != 3 {
		t.Fatalf("canonical read_file start_line should not be overwritten by start alias, got %#v", readArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("write_file", `{"target_path":"generated.go","content":"package main\n"}`)
	var writeArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &writeArgs); err != nil {
		t.Fatalf("normalized write_file args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := writeArgs["path"].(string); got != "generated.go" {
		t.Fatalf("write_file target_path alias should populate path, got %#v from %s", writeArgs, normalized)
	}
	if _, ok := writeArgs["target_path"]; ok {
		t.Fatalf("write_file target_path alias should be removed after normalization: %#v", writeArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("list_directory", `{"directory":"src"}`)
	var listArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &listArgs); err != nil {
		t.Fatalf("normalized list_directory args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := listArgs["path"].(string); got != "src" {
		t.Fatalf("list_directory directory alias should populate path, got %#v from %s", listArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("Glob", `{"glob":"**/*.go","root":"src"}`)
	var globArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &globArgs); err != nil {
		t.Fatalf("normalized Glob args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := globArgs["path"].(string); got != "src" {
		t.Fatalf("Glob root alias should populate path, got %#v from %s", globArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("read_file", `{"directory":"src"}`)
	readArgs = nil
	if err := json.Unmarshal([]byte(normalized), &readArgs); err != nil {
		t.Fatalf("normalized read_file directory args should be valid JSON: %v; %s", err, normalized)
	}
	if _, ok := readArgs["path"]; ok {
		t.Fatalf("read_file should not treat directory alias as file path, got %#v", readArgs)
	}
	if got, _ := readArgs["directory"].(string); got != "src" {
		t.Fatalf("read_file should preserve unsupported directory argument for validation, got %#v", readArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("edit_lines", `{"file":"main.go","action":"update","startLine":2,"endLine":3,"content":"replacement"}`)
	var editLinesArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &editLinesArgs); err != nil {
		t.Fatalf("normalized edit_lines args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := editLinesArgs["path"].(string); got != "main.go" {
		t.Fatalf("edit_lines file alias should populate path, got %#v from %s", editLinesArgs, normalized)
	}
	if got, _ := editLinesArgs["operation"].(string); got != "replace" {
		t.Fatalf("edit_lines action value should normalize to replace, got %#v from %s", editLinesArgs, normalized)
	}
	if got, _ := editLinesArgs["start_line"].(float64); got != 2 {
		t.Fatalf("edit_lines startLine alias should populate start_line, got %#v from %s", editLinesArgs, normalized)
	}
	if got, _ := editLinesArgs["end_line"].(float64); got != 3 {
		t.Fatalf("edit_lines endLine alias should populate end_line, got %#v from %s", editLinesArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("call_mcp_tool", `{"server":"browser","tool":"screenshot","params":{"fullPage":true}}`)
	var mcpArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &mcpArgs); err != nil {
		t.Fatalf("normalized call_mcp_tool args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := mcpArgs["server_id"].(string); got != "browser" {
		t.Fatalf("call_mcp_tool server alias should populate server_id, got %#v from %s", mcpArgs, normalized)
	}
	if got, _ := mcpArgs["tool_name"].(string); got != "screenshot" {
		t.Fatalf("call_mcp_tool tool alias should populate tool_name, got %#v from %s", mcpArgs, normalized)
	}
	if arguments, _ := mcpArgs["arguments"].(map[string]interface{}); arguments == nil || arguments["fullPage"] != true {
		t.Fatalf("call_mcp_tool params alias should populate arguments, got %#v from %s", mcpArgs, normalized)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("call_mcp_tool", `{"server":"alias","server_id":"canonical","tool":"alias_tool","tool_name":"canonical_tool","args":{"bad":true},"arguments":{"ok":true}}`)
	var canonicalMCPArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &canonicalMCPArgs); err != nil {
		t.Fatalf("normalized canonical call_mcp_tool args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := canonicalMCPArgs["server_id"].(string); got != "canonical" {
		t.Fatalf("canonical server_id should not be overwritten by alias, got %#v", canonicalMCPArgs)
	}
	if got, _ := canonicalMCPArgs["tool_name"].(string); got != "canonical_tool" {
		t.Fatalf("canonical tool_name should not be overwritten by alias, got %#v", canonicalMCPArgs)
	}
	if arguments, _ := canonicalMCPArgs["arguments"].(map[string]interface{}); arguments == nil || arguments["ok"] != true {
		t.Fatalf("canonical arguments should not be overwritten by alias, got %#v", canonicalMCPArgs)
	}
	if _, ok := canonicalMCPArgs["server"]; ok {
		t.Fatalf("call_mcp_tool server alias should be removed when canonical is present: %#v", canonicalMCPArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("coding_knowledge_search", `{"question":"how does task execution work?"}`)
	var knowledgeArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &knowledgeArgs); err != nil {
		t.Fatalf("normalized coding_knowledge_search args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := knowledgeArgs["query"].(string); got != "how does task execution work?" {
		t.Fatalf("coding_knowledge_search question alias should populate query, got %#v from %s", knowledgeArgs, normalized)
	}
	if _, ok := knowledgeArgs["question"]; ok {
		t.Fatalf("coding_knowledge_search question alias should be removed after normalization: %#v", knowledgeArgs)
	}

	normalized = normalizeCodingSubAgentToolArgumentsForTool("knowledge_search", `{"query":"canonical","keywords":"alias"}`)
	var projectKnowledgeArgs map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &projectKnowledgeArgs); err != nil {
		t.Fatalf("normalized knowledge_search args should be valid JSON: %v; %s", err, normalized)
	}
	if got, _ := projectKnowledgeArgs["query"].(string); got != "canonical" {
		t.Fatalf("canonical knowledge_search query should not be overwritten by alias, got %#v", projectKnowledgeArgs)
	}
	if _, ok := projectKnowledgeArgs["keywords"]; ok {
		t.Fatalf("knowledge_search keywords alias should be removed when canonical is present: %#v", projectKnowledgeArgs)
	}
}

func TestRemoteCodingSubAgentToolArgumentAliasesNormalizeCommonModelFields(t *testing.T) {
	args := map[string]interface{}{
		"file":        "app/main.go",
		"old_string":  "old text",
		"replacement": "new text",
	}
	if !applyRemoteCodingSubAgentToolArgumentAliases("ssh_edit_file", args) {
		t.Fatalf("ssh_edit_file aliases should report a change")
	}
	if got, _ := args["path"].(string); got != "app/main.go" {
		t.Fatalf("ssh_edit_file file alias should populate path, got %#v", args)
	}
	if got, _ := args["old_str"].(string); got != "old text" {
		t.Fatalf("ssh_edit_file old_string alias should populate old_str, got %#v", args)
	}
	if got, _ := args["new_str"].(string); got != "new text" {
		t.Fatalf("ssh_edit_file replacement alias should populate new_str, got %#v", args)
	}
	for _, alias := range []string{"file", "old_string", "replacement"} {
		if _, ok := args[alias]; ok {
			t.Fatalf("ssh_edit_file alias %q should be removed after normalization: %#v", alias, args)
		}
	}

	args = map[string]interface{}{
		"path":        "canonical.go",
		"target_path": "alias.go",
		"old_str":     "canonical old",
		"find":        "alias old",
		"new_str":     "canonical new",
		"replace":     "alias new",
	}
	if !applyRemoteCodingSubAgentToolArgumentAliases("ssh_edit_file", args) {
		t.Fatalf("ssh_edit_file canonical/alias mix should report a change")
	}
	if got, _ := args["path"].(string); got != "canonical.go" {
		t.Fatalf("canonical path should not be overwritten by alias, got %#v", args)
	}
	if got, _ := args["old_str"].(string); got != "canonical old" {
		t.Fatalf("canonical old_str should not be overwritten by alias, got %#v", args)
	}
	if got, _ := args["new_str"].(string); got != "canonical new" {
		t.Fatalf("canonical new_str should not be overwritten by alias, got %#v", args)
	}
	for _, alias := range []string{"target_path", "find", "replace"} {
		if _, ok := args[alias]; ok {
			t.Fatalf("ssh_edit_file alias %q should be removed when canonical is present: %#v", alias, args)
		}
	}

	args = map[string]interface{}{"directory": "src"}
	if !applyRemoteCodingSubAgentToolArgumentAliases("ssh_list_dir", args) {
		t.Fatalf("ssh_list_dir directory alias should report a change")
	}
	if got, _ := args["path"].(string); got != "src" {
		t.Fatalf("ssh_list_dir directory alias should populate path, got %#v", args)
	}

	args = map[string]interface{}{"command": "go test ./gui", "cwd": "gui"}
	if !applyRemoteCodingSubAgentToolArgumentAliases("ssh_bash", args) {
		t.Fatalf("ssh_bash cwd alias should report a change")
	}
	if got, _ := args["working_dir"].(string); got != "gui" {
		t.Fatalf("ssh_bash cwd alias should populate working_dir, got %#v", args)
	}

	args = map[string]interface{}{"id": "task-123"}
	if !applyRemoteCodingSubAgentToolArgumentAliases("ssh_check_task", args) {
		t.Fatalf("ssh_check_task id alias should report a change")
	}
	if got, _ := args["task_id"].(string); got != "task-123" {
		t.Fatalf("ssh_check_task id alias should populate task_id, got %#v", args)
	}

	args = map[string]interface{}{"question": "vector index"}
	if !applyRemoteCodingSubAgentToolArgumentAliases("knowledge_search", args) {
		t.Fatalf("knowledge_search question alias should report a change")
	}
	if got, _ := args["query"].(string); got != "vector index" {
		t.Fatalf("knowledge_search question alias should populate query, got %#v", args)
	}
}

func TestRemoteCodingSubAgentEmptyToolArgumentsNormalizeToObject(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	result := cb.executeRemoteTool("ssh_list_dir", "")
	if strings.Contains(result, "参数解析失败") || strings.Contains(result, "unexpected end of JSON input") {
		t.Fatalf("empty remote arguments should not surface JSON parse failure: %q", result)
	}
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("empty remote arguments should parse and reach handler validation, got %q", result)
	}
}

func TestRemoteCodingSubAgentResolvePathKeepsRelativePathWhenProjectDirMissing(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	if got := cb.resolvePath("src/main.go"); got != "src/main.go" {
		t.Fatalf("relative path without project dir should stay relative, got %q", got)
	}
	cb.agent.projectDir = "/repo/project/"
	if got := cb.resolvePath("src/main.go"); got != "/repo/project/src/main.go" {
		t.Fatalf("relative path should resolve under trimmed project dir, got %q", got)
	}
	if got := cb.resolvePath("/tmp/main.go"); got != "/tmp/main.go" {
		t.Fatalf("absolute path should stay absolute, got %q", got)
	}
}

func TestRemoteCodingSubAgentDefaultWorkingDirFallsBackToWorkDir(t *testing.T) {
	var nilCB *remoteCodingCallbacks
	if got := nilCB.defaultRemoteWorkingDir(); got != "." {
		t.Fatalf("nil callback default dir = %q, want .", got)
	}

	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{workDir: "/repo/work"}}
	if got := cb.defaultRemoteWorkingDir(); got != "/repo/work" {
		t.Fatalf("default dir should use workDir when projectDir is missing, got %q", got)
	}
	cb.agent.projectDir = "/repo/project"
	if got := cb.defaultRemoteWorkingDir(); got != "/repo/project" {
		t.Fatalf("default dir should prefer projectDir over workDir, got %q", got)
	}
}

func TestRemoteWriteFileResultRequiresExplicitOK(t *testing.T) {
	success := remoteWriteFileResult("/repo/main.py", 12, "OK\n", false)
	if !strings.Contains(success, "✅ 已写入 /repo/main.py") || !strings.Contains(success, "12 bytes") || !strings.Contains(success, "created=false") {
		t.Fatalf("write result should report success when command prints OK, got %q", success)
	}
	created := remoteWriteFileResult("/repo/new.py", 8, "OK created=true\n", false)
	if !strings.Contains(created, "created=true") {
		t.Fatalf("write result should preserve created=true from explicit OK line, got %q", created)
	}
	chunked := remoteWriteFileResult("/repo/main.py", 40000, "OK\n", true)
	if !strings.Contains(chunked, "chunked") {
		t.Fatalf("chunked write result should mention chunked success, got %q", chunked)
	}
	for _, result := range []string{
		"",
		"ERROR: permission denied",
		"remote coding subagent: handler unavailable",
		"python3: command not found",
		"Traceback (most recent call last):",
		"log: OK but write was skipped",
		"prefix OK created=true",
		"OK-ish",
		"OK created=maybe",
		"ok created=true",
	} {
		got := remoteWriteFileResult("/repo/main.py", 12, result, false)
		if !strings.HasPrefix(got, "写入失败:") {
			t.Fatalf("write result for %q should fail closed, got %q", result, got)
		}
	}
}

func TestRemoteEditFileResultRequiresExplicitOK(t *testing.T) {
	success := remoteEditFileResult("/repo/main.py", "OK: replaced 1 occurrence\n")
	if !strings.Contains(success, "✅ 已编辑 /repo/main.py") {
		t.Fatalf("edit result should report success when command prints OK, got %q", success)
	}
	for _, result := range []string{
		"",
		"ERROR: old_str not found in file",
		"remote coding subagent: handler unavailable",
		"python3: command not found",
		"Traceback (most recent call last):",
		"replaced 1 occurrence",
		"log OK: replaced 1 occurrence",
		"OK: skipped replacement",
		"OK: replaced 0 occurrences",
		"OK: replaced 2 occurrences",
	} {
		got := remoteEditFileResult("/repo/main.py", result)
		if !strings.HasPrefix(got, "编辑失败:") {
			t.Fatalf("edit result for %q should fail closed, got %q", result, got)
		}
	}
}

func TestRemoteCodingSubAgentResultFromLoopResultPreservesNonSuccessStates(t *testing.T) {
	result := remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{
		Error:      "cancelled during LLM retry",
		Text:       "partial",
		Iterations: 2,
		ToolCalls:  3,
	})
	if result.Status != "cancelled" || result.Error != "cancelled during LLM retry" || result.Summary != "partial" || result.Iterations != 2 || result.ToolCalls != 3 {
		t.Fatalf("cancelled loop result should preserve cancellation metadata, got %#v", result)
	}

	result = remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{
		Text:      "工具 bash 连续失败 12 次，已停止执行。",
		HardExit:  true,
		ToolCalls: 12,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "hard exit") || result.ToolCalls != 12 {
		t.Fatalf("hard-exit loop result should be failed, got %#v", result)
	}

	result = remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{
		Text:    "[ask_user] Need approval",
		AskUser: &agent.AskUserRequest{Question: "Proceed?"},
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "requires user input") {
		t.Fatalf("ask-user loop result should be failed for remote autonomous task, got %#v", result)
	}

	result = remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{Text: " \n\t", Iterations: 1})
	if result.Status != "failed" || !strings.Contains(result.Error, "empty summary") {
		t.Fatalf("empty remote loop summary should be failed, got %#v", result)
	}

	result = remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{Text: "done", Iterations: 1})
	if result.Status != "failed" || !strings.Contains(result.Error, "without using tools") {
		t.Fatalf("remote loop result without tool evidence should fail, got %#v", result)
	}

	result = remoteCodingSubAgentResultFromLoopResult(agent.LoopResult{Text: "done", ToolCalls: 1})
	if result.Status != "success" || result.Summary != "done" || result.Error != "" {
		t.Fatalf("normal loop result with tool evidence should remain success, got %#v", result)
	}
}

func TestRemoteCodingSubAgentVerificationGateRequiresPostEditVerification(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 2,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "verification command") || !strings.Contains(result.Summary, "## 验证状态") {
		t.Fatalf("remote file changes without verification should fail, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "success" || result.Error != "" || !strings.Contains(result.Summary, "PASS") {
		t.Fatalf("remote post-edit verification should pass, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 3,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "before the final edit") {
		t.Fatalf("remote verification before final edit should fail, got %#v", result)
	}
}

func TestRemoteCodingSubAgentNoChangeRequiresInspectionEvidence(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "nothing needed",
		ToolCalls: 1,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "no file changes and no inspection") || !strings.Contains(result.Summary, "## 无改动证据") {
		t.Fatalf("remote no-change result without evidence should fail, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/README.md")
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "inspected only",
		ToolCalls: 1,
	})
	if result.Status != "success" || strings.Contains(result.Summary, "## 无改动证据") {
		t.Fatalf("remote no-change result with read evidence should pass, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteSearch("ssh_list_dir", "ls -la /repo", "/repo", "total 8\n-rw-r--r-- README.md", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "listed only",
		ToolCalls: 1,
	})
	if result.Status != "success" || strings.Contains(result.Summary, "## 无改动证据") {
		t.Fatalf("remote no-change result with list/search evidence should pass, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteSearch("ssh_list_dir", "ls -la /repo", "/repo", "No results found", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "empty list only",
		ToolCalls: 1,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "no file changes") {
		t.Fatalf("remote no-change result with empty search evidence should fail, got %#v", result)
	}
}

func TestRemoteCodingSubAgentKeepsVerifiedExistingArtifactSuccessful(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/hello.cpp")
	cb.trackRemoteCommand("g++ -o hello hello.cpp", "/repo", "compiled successfully", true)
	cb.trackRemoteCommand("./hello", "/repo", "Hello, World!\nexit code = 0", true)

	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "failed",
		Error:     "max iterations reached after the final check",
		Summary:   "hello.cpp is already correct and ran successfully",
		ToolCalls: 2,
	})
	if result.Status != "success" || result.Error != "" {
		t.Fatalf("verified existing artifact should recover from a late loop failure, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/hello.cpp")
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "failed",
		Error:     "connection lost",
		Summary:   "inspection only",
		ToolCalls: 1,
	})
	if result.Status != "failed" {
		t.Fatalf("inspection alone must not recover a failed remote task, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileChanged("/repo/hello.cpp", true)
	cb.trackRemoteCommand("g++ -o hello hello.cpp", "/repo", "compiled successfully", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "failed",
		Error:     "max iterations reached after the final check",
		Summary:   "created artifact",
		ToolCalls: 2,
	})
	if result.Status != "failed" {
		t.Fatalf("a task with file writes must not recover from a failed remote task, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteCommand("g++ -o hello hello.cpp", "/repo", "compiled successfully", true)
	cb.trackRemoteCommand("cat missing.txt", "/repo", "cat: missing.txt: No such file or directory", false)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "failed",
		Error:     "max iterations reached after the final check",
		Summary:   "verification plus unresolved failure",
		ToolCalls: 2,
	})
	if result.Status != "failed" {
		t.Fatalf("unresolved command failures must not recover a failed remote task, got %#v", result)
	}
}

func TestRemoteCodingSubAgentQualityGateFailsUnresolvedPostEditCommands(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("cat missing.txt", "/repo", "cat: missing.txt: No such file or directory", false)
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 6,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "cat missing.txt") || !strings.Contains(result.Summary, "## 命令状态") {
		t.Fatalf("unresolved failed post-edit command should fail remote quality gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("cat missing.txt", "/repo", "cat: missing.txt: No such file or directory", false)
	cb.trackRemoteCommand("cat missing.txt", "/repo", "now exists", true)
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 7,
	})
	if result.Status != "success" || strings.Contains(result.Summary, "## 命令状态") {
		t.Fatalf("later successful equivalent command should resolve remote failure, got %#v", result)
	}
}

func TestRemoteCodingSubAgentFailedResultStillIncludesCommandEvidence(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteCommand("cat missing.txt", "/repo", "cat: missing.txt: No such file or directory", false)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "failed",
		Error:     "model stopped",
		Summary:   "partial work",
		ToolCalls: 1,
	})
	if result.Status != "failed" || result.Error != "model stopped" || !strings.Contains(result.Summary, "## 命令状态") || !strings.Contains(result.Summary, "cat missing.txt") {
		t.Fatalf("failed remote result should keep unresolved command evidence, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("npm run lint", "/repo", "lint failed", false)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "failed" || !strings.Contains(result.Summary, "## 验证状态") || strings.Contains(result.Summary, "## 命令状态") {
		t.Fatalf("verification failures should not duplicate command failure summary, got %#v", result)
	}
}

func TestRemoteCodingSubAgentExplorationGateRequiresPreEditReadForExistingFiles(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 3,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "首次修改前") || !strings.Contains(result.Summary, "## 探索状态") {
		t.Fatalf("remote existing-file edit without pre-read should fail exploration gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteCommand("rg 'func target' src", "/repo", "src/main.py:12:def target():", true)
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "success" || result.Error != "" || !strings.Contains(result.Summary, "EXPLORED") {
		t.Fatalf("remote successful search before edit should satisfy exploration gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteSearch("ssh_list_dir", "ls -la /repo", "/repo", "main.py\nREADME.md", true)
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "首次修改前") {
		t.Fatalf("remote directory listing alone should not satisfy exploration gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteCommand("rg 'missing' src", "/repo", "no matches", true)
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "首次修改前") {
		t.Fatalf("remote empty search result should not satisfy exploration gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileChanged("/repo/new.py", true)
	cb.trackRemoteFileRead("/repo/new.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " new.py | 1 +", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "created file",
		ToolCalls: 4,
	})
	if result.Status != "success" || result.Error != "" || !strings.Contains(result.Summary, "NOT_NEEDED") {
		t.Fatalf("remote created file should not require pre-edit read, got %#v", result)
	}
}

func TestRemoteCodingSubAgentConfirmationGateRequiresPostEditRead(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 3,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "ssh_read_file") || !strings.Contains(result.Summary, "## 确认状态") {
		t.Fatalf("remote file changes without post-edit read should fail confirmation gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/a.py")
	cb.trackRemoteFileChanged("/repo/a.py", false)
	cb.trackRemoteFileRead("/repo/a.py")
	cb.trackRemoteFileChanged("/repo/b.py", true)
	cb.trackRemoteFileRead("/repo/a.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed files",
		ToolCalls: 6,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "/repo/b.py") {
		t.Fatalf("each modified remote file should need post-edit read confirmation, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	if remoteReadFileResultHasUsefulEvidence("[remote read_file EOF: offset 999 is beyond scanned file length 20]") {
		cb.trackRemoteFileRead("/repo/main.py")
	}
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "/repo/main.py") {
		t.Fatalf("wrong-offset EOF read should not satisfy post-edit confirmation, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/./main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "success" {
		t.Fatalf("cleaned equivalent remote paths should satisfy post-edit confirmation, got %#v", result)
	}
}

func TestRemoteCodingSubAgentLogsFailedShellCommandDetails(t *testing.T) {
	var buf bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}()

	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		sessionID:  "ssh_root@example.com:22_1",
		projectDir: "/repo",
	}, task: "build remote project with token=task-secret"}
	cb.trackRemoteCommand("cmake --build build --token secret-token", "/repo", "fatal error LNK1120: unresolved externals\npassword=hunter2\nEXIT: 1", false)

	got := buf.String()
	for _, want := range []string{
		"[remote-subagent] shell command failed",
		"tool=ssh_bash",
		"outcome=failed",
		`session="ssh_root@example.com:22_1"`,
		`project="/repo"`,
		`task="build remote project with token=[redacted]"`,
		`workdir="/repo"`,
		`command="cmake --build build --token [redacted]"`,
		"cmake --build build --token [redacted]",
		`result="fatal error LNK1120: unresolved externals password=[redacted] EXIT: 1"`,
		"LNK1120",
		"password=[redacted]",
		"EXIT: 1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("failed remote shell command log missing %q in %q", want, got)
		}
	}
	for _, leaked := range []string{"secret-token", "hunter2", "task-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("failed remote shell command log leaked secret %q in %q", leaked, got)
		}
	}

	buf.Reset()
	cb.trackRemoteCommand("g++ -o hello hello.cpp", "/repo", "ok\nEXIT: 0", true)
	if got := buf.String(); got != "" {
		t.Fatalf("successful remote shell command should not log failure details, got %q", got)
	}

	cb.trackRemoteCommand("g++ --version 2>&1", "/repo", "bash: g++: command not found\nEXIT: 127", false)
	if got := buf.String(); got != "" {
		t.Fatalf("diagnostic remote shell probe should not log failure details, got %q", got)
	}
	if len(cb.commandsRun) != 3 || cb.commandsRun[2].Command != "g++ --version 2>&1" || cb.commandsRun[2].Succeeded {
		t.Fatalf("diagnostic remote shell probe should remain in command audit, got %#v", cb.commandsRun)
	}
	unresolved := unresolvedFailedSubAgentCommands(cb.commandsRun[2:])
	if len(unresolved) != 1 || unresolved[0].Command != "g++ --version 2>&1" {
		t.Fatalf("diagnostic probe without later verification should remain an unresolved command, got %#v", unresolved)
	}

	cb.trackRemoteCommand("gcc -v", "/repo", "bash: gcc: command not found\nEXIT: 127", false)
	if got := buf.String(); got != "" {
		t.Fatalf("compiler diagnostic remote shell probe should not log failure details, got %q", got)
	}

	cb.trackRemoteCommand("python3 --version && pytest tests", "/repo", "FAILED tests/test_app.py::TestApp\npassword=hunter2\nEXIT: 1", false)
	got = buf.String()
	if !strings.Contains(got, "[remote-subagent] shell command failed") || !strings.Contains(got, "python3 --version && pytest tests") || !strings.Contains(got, "FAILED tests/test_app.py::TestApp") {
		t.Fatalf("mixed diagnostic plus verification failure should be logged, got %q", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("mixed failure log leaked secret in %q", got)
	}
}

func TestRemoteDiffSelfCheckSummary(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "success" || !strings.Contains(result.Summary, "## 远程 Diff 自检") || !strings.Contains(result.Summary, "CHECKED") {
		t.Fatalf("remote diff self-check should be reported as checked, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "git diff/status") || !strings.Contains(result.Summary, "MISSING") {
		t.Fatalf("missing post-edit remote diff self-check should fail the final quality gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("git diff --stat", "/repo", "no changes", true)
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "git diff/status") || !strings.Contains(result.Summary, "MISSING") {
		t.Fatalf("pre-edit remote diff self-check should fail the final quality gate, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git status --short", "/repo", "fatal: not a git repository", false)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "success" || !strings.Contains(result.Summary, "NOT_NEEDED") || strings.Contains(result.Summary, "## 命令状态") {
		t.Fatalf("non-git remote diff/status failure should not leave command failure, got %#v", result)
	}

	status, summary := summarizeRemoteDiffSelfCheck([]string{"/repo/main.py"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: false, Summary: "fatal: not a git repository", seq: 2},
	}, 1)
	if status != codingSubAgentQualityNotNeeded || !strings.Contains(summary, "不是 Git 仓库") {
		t.Fatalf("non-git remote diff self-check should be skipped softly, got (%q, %q)", status, summary)
	}

	status, summary = summarizeRemoteDiffSelfCheck([]string{"/repo/main.py"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: false, Summary: "fatal: not a git repository", seq: 2},
		{Command: "git diff --stat", Succeeded: false, Summary: "fatal: bad revision 'HEAD'", seq: 3},
	}, 1)
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "bad revision") {
		t.Fatalf("mixed remote diff failures should still surface real git errors, got (%q, %q)", status, summary)
	}

	for _, tc := range []struct {
		name    string
		command string
		summary string
	}{
		{name: "empty diff", command: "git diff", summary: ""},
		{name: "exit marker only", command: "git diff --stat", summary: "EXIT: 0"},
		{name: "status clean", command: "git status", summary: "On branch main\nnothing to commit, working tree clean\nEXIT: 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, summary := summarizeRemoteDiffSelfCheck([]string{"/repo/main.py"}, []CodingSubAgentCommandResult{
				{Command: tc.command, Succeeded: true, Summary: tc.summary, seq: 2},
			}, 1)
			if status != codingSubAgentQualityFailed || !strings.Contains(summary, "工作区干净") {
				t.Fatalf("clean remote diff self-check should be suspicious, got (%q, %q)", status, summary)
			}
		})
	}

	status, summary = summarizeRemoteDiffSelfCheck([]string{"/repo/main.py"}, []CodingSubAgentCommandResult{
		{Command: "git diff --stat", Succeeded: true, Summary: "EXIT: 0", seq: 2},
		{Command: "git diff", Succeeded: true, Summary: "diff --git a/main.py b/main.py\n+print('ok')\nEXIT: 0", seq: 3},
	}, 1)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "已运行 2 条") {
		t.Fatalf("non-empty remote diff self-check should still pass, got (%q, %q)", status, summary)
	}

	status, summary = summarizeRemoteDiffSelfCheck([]string{"/repo/main.py"}, []CodingSubAgentCommandResult{
		{Command: "git status", Succeeded: true, Summary: "Changes not staged for commit:\n\tmodified: main.py\n\nno changes added to commit\nEXIT: 0", seq: 2},
	}, 1)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("dirty git status should not be treated as clean, got (%q, %q)", status, summary)
	}
}

func TestRemoteDiffSelfCheckCommandClassifier(t *testing.T) {
	positives := []string{
		"git diff",
		"git diff --stat",
		"git status --short",
		"git status --short; git diff --stat",
		"bash -lc \"git diff -- src/main.py\"",
	}
	for _, command := range positives {
		if !isRemoteDiffSelfCheckCommand(command) {
			t.Fatalf("isRemoteDiffSelfCheckCommand(%q) = false, want true", command)
		}
	}
	negatives := []string{
		"git grep target",
		"git log --oneline",
		"pytest tests",
		"rg target src",
	}
	for _, command := range negatives {
		if isRemoteDiffSelfCheckCommand(command) {
			t.Fatalf("isRemoteDiffSelfCheckCommand(%q) = true, want false", command)
		}
	}
}

func TestRemoteDiffSelfCheckCommandAllowedByWindowsShellGuard(t *testing.T) {
	previous := remotePlatformGOOS
	remotePlatformGOOS = func() string { return "windows" }
	t.Cleanup(func() { remotePlatformGOOS = previous })

	if msg := rejectDisallowedCodingBashCommand("git status --short; git diff --stat"); msg != "" {
		t.Fatalf("semicolon remote diff self-check should pass Windows shell guard, got %q", msg)
	}
	if msg := rejectDisallowedCodingBashCommand("git status --short && git diff --stat"); msg != "" {
		t.Fatalf("ampersand remote diff self-check should be normalized by the executor, got %q", msg)
	}
	if msg := rejectDisallowedCodingBashCommand("git reset --hard HEAD"); msg == "" {
		t.Fatal("destructive git command should remain rejected")
	}
}

func TestSubAgentCommandSummarySoftensNonGitDiffSelfCheckFailure(t *testing.T) {
	status, summary := summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "git diff --stat", Succeeded: false, Summary: "fatal: not a git repository"},
	})
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "skipped diff self-check") || strings.Contains(summary, "failed") {
		t.Fatalf("non-git diff self-check command should not be summarized as failed, got (%q, %q)", status, summary)
	}

	failed := unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "git diff --stat", Succeeded: false, Summary: "fatal: not a git repository"},
		{Command: "go test ./...", Succeeded: false, Summary: "compile failed"},
	})
	if len(failed) != 1 || failed[0].Command != "go test ./..." {
		t.Fatalf("only real command failures should remain unresolved, got %#v", failed)
	}

	commandSummary := appendSubAgentCommandSummary("done", []CodingSubAgentCommandResult{
		{Command: "git diff --stat", Succeeded: false, Summary: "fatal: not a git repository"},
	})
	if !strings.Contains(commandSummary, "SKIP: `git diff --stat`") || strings.Contains(commandSummary, "FAIL: `git diff --stat`") {
		t.Fatalf("non-git diff self-check should be shown as skipped, got %q", commandSummary)
	}
}

func TestRemoteReadFileResultUsefulEvidence(t *testing.T) {
	useful := []string{
		"1\tpackage main\n2\tfunc main() {}\n",
		"1\t\n",
		"2\t   \n",
		"3\t\r\n",
		"25\tline\n\n[remote read_file truncated: showing lines 25-25; call again with offset=26]",
		"[remote read_file EOF: offset 1 is beyond scanned file length 0]",
	}
	for _, result := range useful {
		if !remoteReadFileResultHasUsefulEvidence(result) {
			t.Fatalf("remoteReadFileResultHasUsefulEvidence(%q) = false, want true", result)
		}
	}

	notUseful := []string{
		"",
		"[remote read_file binary/non-UTF8: 42 bytes; text line range unavailable for offset=1 limit=200]",
		"[remote read_file EOF: offset 999 is beyond scanned file length 20]",
		"[remote read_file truncated: showing lines 1-200; call again with offset=201]",
	}
	for _, result := range notUseful {
		if remoteReadFileResultHasUsefulEvidence(result) {
			t.Fatalf("remoteReadFileResultHasUsefulEvidence(%q) = true, want false", result)
		}
	}
}

func TestRemoteCodingExplorationCommandClassifier(t *testing.T) {
	positives := []string{
		"rg 'func target' src",
		"ripgrep target src",
		"grep -R target src",
		"git grep target -- '*.go'",
		"codegraph explore target flow",
		"codegraph node gui/remote_coding_subagent.go",
		"bash -lc \"rg target src\"",
	}
	for _, command := range positives {
		if !isRemoteCodingExplorationCommand(command) {
			t.Fatalf("isRemoteCodingExplorationCommand(%q) = false, want true", command)
		}
	}

	negatives := []string{
		"pytest tests",
		"go test ./...",
		"ls -la src",
		"git status --short",
		"codegraph init",
	}
	for _, command := range negatives {
		if isRemoteCodingExplorationCommand(command) {
			t.Fatalf("isRemoteCodingExplorationCommand(%q) = true, want false", command)
		}
	}
}

func TestRemoteCodingTaskStatusCommandAndWorkingDir(t *testing.T) {
	command, workingDir := remoteCodingTaskStatusCommandAndWorkingDir("[completed] task bg_1\ncommand: cd /repo && pytest tests\nstatus: completed\nexit_code: 0\n")
	if command != "pytest tests" || workingDir != "/repo" {
		t.Fatalf("expected command and working dir split, got command=%q workingDir=%q", command, workingDir)
	}

	command, workingDir = remoteCodingTaskStatusCommandAndWorkingDir("[completed] task bg_1\ncommand: cd '/repo with spaces' && go test ./gui\nstatus: completed\nexit_code: 0\n")
	if command != "go test ./gui" || workingDir != "/repo with spaces" {
		t.Fatalf("expected quoted working dir split, got command=%q workingDir=%q", command, workingDir)
	}

	command, workingDir = remoteCodingTaskStatusCommandAndWorkingDir("[completed] task bg_1\ncommand: cd -- /repo && npm test\nstatus: completed\nexit_code: 0\n")
	if command != "npm test" || workingDir != "/repo" {
		t.Fatalf("expected cd -- working dir split, got command=%q workingDir=%q", command, workingDir)
	}

	command, workingDir = remoteCodingTaskStatusCommandAndWorkingDir("[completed] task bg_1\ncommand: cd /repo; pytest tests\nstatus: completed\nexit_code: 0\n")
	if command != "cd /repo; pytest tests" || workingDir != "" {
		t.Fatalf("non-&& cd command should be preserved, got command=%q workingDir=%q", command, workingDir)
	}
}

func TestRemoteCodingTaskCheckResultCanSatisfyVerification(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[completed] task bg_1\ncommand: cd /repo && pytest tests\nstatus: completed\nexit_code: 0\nprocess_alive: false\n\n--- latest log ---\n1 passed")
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "success" || !strings.Contains(result.Summary, "PASS") {
		t.Fatalf("completed check_task with verifier command should satisfy verification, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[running] task bg_1\ncommand: cd /repo && pytest tests\nstatus: running\nexit_code: unknown\nprocess_alive: true\n\n--- latest log ---\ncollecting")
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "verification command") {
		t.Fatalf("running check_task should not satisfy verification, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[running] task bg_1\ncommand: cd /repo && pytest tests\nstatus: running\nexit_code: unknown\nprocess_alive: true\n\n--- latest log ---\nstatus: completed\nexit_code: 0\n1 passed")
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 4,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "verification command") {
		t.Fatalf("log-only completed status should not satisfy verification, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[completed] task bg_1\ncommand: cd /repo && pytest tests\nstatus: completed\nexit_code: unknown\nprocess_alive: false\n\n--- latest log ---\nexit_code: 0\n1 passed")
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 5,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "pytest tests") {
		t.Fatalf("completed check_task with unknown header exit should not use log exit code, got %#v", result)
	}
}

func TestRemoteCodingTaskCheckFailureResolvedBySameWorkingDirCommand(t *testing.T) {
	cb := &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[completed] task bg_1\ncommand: cd /repo && pytest tests\nstatus: failed\nexit_code: 1\nprocess_alive: false\n\n--- latest log ---\nFAILED tests/test_main.py")
	cb.trackRemoteCommand("pytest tests", "/repo", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 6,
	})
	if result.Status != "success" || strings.Contains(result.Summary, "## 命令状态") {
		t.Fatalf("same working dir rerun should resolve failed check_task command, got %#v", result)
	}

	cb = &remoteCodingCallbacks{}
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteFileChanged("/repo/main.py", false)
	cb.trackRemoteFileRead("/repo/main.py")
	cb.trackRemoteTaskCheckResult("[completed] task bg_1\ncommand: cd /repo && pytest tests\nstatus: failed\nexit_code: 1\nprocess_alive: false\n\n--- latest log ---\nFAILED tests/test_main.py")
	cb.trackRemoteCommand("pytest tests", "/other", "1 passed in 0.1s", true)
	cb.trackRemoteCommand("git diff --stat", "/repo", " main.py | 2 +-", true)
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status:    "success",
		Summary:   "changed file",
		ToolCalls: 6,
	})
	if result.Status != "failed" || !strings.Contains(result.Summary, "## 验证状态") || !strings.Contains(result.Error, "pytest tests") {
		t.Fatalf("different working dir rerun should not resolve failed check_task command, got %#v", result)
	}
}

func TestRemoteCodingSubAgentCallbacksAreNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("remote coding callbacks should be nil-safe, panicked: %v", r)
		}
	}()

	var nilCB *remoteCodingCallbacks
	if cfg := nilCB.GetLLMConfig(); cfg.URL != "" || cfg.Key != "" {
		t.Fatalf("nil callback config should be empty, got %#v", cfg)
	}
	if nilCB.ShouldStop() {
		t.Fatalf("nil callback should not request stop")
	}
	if ctx, release, err := nilCB.LLMRequestContext(1); err != nil || ctx == nil || release == nil {
		t.Fatalf("nil callback should return background request context, ctx=%v release_nil=%v err=%v", ctx, release == nil, err)
	}
	if prompt := nilCB.BuildSystemPrompt("task", true); !strings.Contains(prompt, "Remote Coding SubAgent") {
		t.Fatalf("nil callback should still build base remote prompt, got %q", prompt)
	}
	tools := nilCB.BuildTools("task")
	if len(tools) == 0 {
		t.Fatalf("nil callback should still expose base remote tools")
	}
	nilCB.OnToken("delta")
	nilCB.OnProgress("progress")
	nilCB.OnToolCall("ssh_list_dir")

	if result := nilCB.ExecuteTool("ssh_list_dir", "{}"); !strings.Contains(result, "agent unavailable") {
		t.Fatalf("nil callback execute should report unavailable agent, got %q", result)
	}

	cb := &remoteCodingCallbacks{}
	cb.OnToken("delta")
	cb.OnProgress("progress")
	cb.OnToolCall("ssh_list_dir")
	if cb.ShouldStop() {
		t.Fatalf("callback with nil agent should not request stop")
	}
	if result := cb.ExecuteTool("ssh_list_dir", "{}"); !strings.Contains(result, "agent unavailable") {
		t.Fatalf("callback with nil agent should report unavailable agent, got %q", result)
	}

	cb = &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	if result := cb.ExecuteTool("ssh_check_task", `{"task_id":"task-123"}`); !strings.Contains(result, "handler unavailable") {
		t.Fatalf("ssh_check_task should report unavailable handler instead of panicking, got %q", result)
	}
}

func TestRemoteCodingSubAgentPromptAndToolDefinitionsExposeAliases(t *testing.T) {
	prompt := buildRemoteCodingSystemPrompt("/repo/project", "/repo", "")
	for _, want := range []string{
		"file/file_path/filename/target_path",
		"offset/limit",
		"start/start_line/startLine",
		"lines/num_lines/line_count",
		"ssh_check_task(task_id, tail_lines?)",
		"默认读取前 200 行",
		"old_string/old_content/find/search -> old_str",
		"new_string/new_content/replace/replacement -> new_str",
		"cwd/work_dir -> working_dir",
		"id/task -> task_id",
		"再次 ssh_read_file",
		"运行匹配任务的验证命令",
		"git diff --stat",
		"git status --short",
		"diff/status 自检结果",
		"实际运行的验证命令及结果",
		"剩余风险或未验证项",
		"文件改写必须使用 ssh_edit_file/ssh_write_file",
		"必须用 ssh_check_task 跟进直到得到明确状态/exit_code",
		"git reset/checkout/restore/switch/merge/rebase/stash/add/commit/apply/clean -f",
		"不要用 ssh_bash 执行 rm -r/rm -rf",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("remote prompt should expose alias hint %q, got %q", want, prompt)
		}
	}

	defs := remoteCodingToolDefinitions()
	descriptions := make(map[string]string)
	for _, def := range defs {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		descriptions[name], _ = fn["description"].(string)
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		for propName, propDef := range props {
			prop, _ := propDef.(map[string]interface{})
			desc, _ := prop["description"].(string)
			descriptions[name+"."+propName] = desc
		}
	}

	expectDescriptionContains := func(key string, wants ...string) {
		t.Helper()
		got := descriptions[key]
		if got == "" {
			t.Fatalf("missing remote tool description for %s in %#v", key, descriptions)
		}
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Fatalf("remote tool description %s should contain %q, got %q", key, want, got)
			}
		}
	}

	expectDescriptionContains("ssh_read_file.path", "file/file_path/filename/target_path")
	expectDescriptionContains("ssh_read_file.offset", "start/start_line/startLine")
	expectDescriptionContains("ssh_read_file.limit", "默认 200", "lines/num_lines/line_count", "最大 2000")
	expectDescriptionContains("ssh_edit_file", "old_string/old_content/find/search", "new_string/new_content/replace/replacement")
	expectDescriptionContains("ssh_edit_file.old_str", "old_string/old_content/find/search")
	expectDescriptionContains("ssh_edit_file.new_str", "new_string/new_content/replace/replacement")
	expectDescriptionContains("ssh_bash", "探索、诊断、格式化或验证", "拒绝 git 工作区改写", "递归删除")
	expectDescriptionContains("ssh_bash.command", "探索/诊断/格式化/验证", "不要用它改写文件或 Git 工作区")
	expectDescriptionContains("ssh_bash.working_dir", "cwd/work_dir")
	expectDescriptionContains("ssh_list_dir.path", "dir/directory/root", "file/file_path/filename/target_path")
	expectDescriptionContains("ssh_check_task.task_id", "id/task")
	expectDescriptionContains("ssh_check_task.tail_lines", "默认 50", "tail/lines/limit", "1-1000")
}

func TestRemoteCodingSubAgentRejectsHighRiskBashBeforeSSH(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: "/repo/project"}}
	for _, command := range []string{
		"git reset --hard HEAD",
		"git checkout -- .",
		"rm -rf build",
		"sed -i 's/a/b/' src/main.go",
		"python -c \"open('src/main.go','w').write('x')\"",
	} {
		result := cb.sshBash(map[string]interface{}{"command": command})
		if !strings.Contains(result, "拒绝执行高风险命令") || !strings.Contains(result, "远程编码 SubAgent") {
			t.Fatalf("remote ssh_bash should reject high-risk command %q before SSH, got %q", command, result)
		}
		if strings.Contains(result, "handler unavailable") {
			t.Fatalf("remote ssh_bash should reject %q before checking SSH handler, got %q", command, result)
		}
	}
}

func TestDirectoryCreationShellMutationGuard(t *testing.T) {
	for _, command := range []string{
		"mkdir -p build",
		"mkdir --mode 755 generated && mkdir -- cache",
	} {
		if !hasOnlyDirectoryCreationShellMutation(command) {
			t.Fatalf("directory-only command should be eligible for scoped approval: %q", command)
		}
	}
	for _, command := range []string{
		"mkdir build; touch build/marker",
		"mkdir $TARGET",
		"mkdir build && go test ./...",
		"mkdir -p",
	} {
		if hasOnlyDirectoryCreationShellMutation(command) {
			t.Fatalf("non-static or mixed command should not be eligible for scoped approval: %q", command)
		}
	}
}

func TestRemoteShellDirectoryCreationTargets(t *testing.T) {
	targets, ok := remoteShellDirectoryCreationTargets("mkdir -p generated && mkdir --mode 755 ../shared", "/repo/project")
	if !ok {
		t.Fatal("static mkdir targets should be parsed")
	}
	want := []string{"/repo/project/generated", "/repo/shared"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("directory targets = %#v, want %#v", targets, want)
	}
	if _, ok := remoteShellDirectoryCreationTargets("mkdir $TARGET", "/repo/project"); ok {
		t.Fatal("dynamic directory target should not be parsed for scoped approval")
	}
}
func TestRemoteCodingSubAgentDirectoryCreationRequestsScopedApproval(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: "/repo/project"}}
	var gotReq ScopeApprovalRequest
	cb.agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		gotReq = req
		return ScopeApprovalAllowOnce
	}, false)

	result := cb.sshBash(map[string]interface{}{"command": "mkdir -p generated"})
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("approved directory creation should proceed to SSH handler, got %q", result)
	}
	if gotReq.ToolName != remoteSSHBashToolName || gotReq.Kind != remoteDirectoryWriteKind || gotReq.Path != "/repo/project" || gotReq.ProjectPath != "/repo/project" {
		t.Fatalf("directory approval request = %#v", gotReq)
	}
}
func TestRemoteCodingSubAgentDirectoryApprovalPersistsForDirectory(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: "/repo/project"}}
	calls := 0
	cb.agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		if req.Kind != remoteDirectoryWriteKind {
			t.Fatalf("approval kind = %q, want %q", req.Kind, remoteDirectoryWriteKind)
		}
		return ScopeApprovalAllowDir
	}, false)

	for _, command := range []string{"mkdir generated", "mkdir artifacts"} {
		result := cb.sshBash(map[string]interface{}{"command": command})
		if !strings.Contains(result, "handler unavailable") {
			t.Fatalf("approved directory command should proceed to SSH handler, command=%q result=%q", command, result)
		}
	}
	if calls != 1 {
		t.Fatalf("directory approval should be reused within the approved directory, calls=%d", calls)
	}
}
func TestRemoteCodingSubAgentHighRiskBashCanBeUserApproved(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{projectDir: "/repo/project"},
	}
	var gotReq ScopeApprovalRequest
	cb.agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		gotReq = req
		return ScopeApprovalAllowOnce
	}, false)

	result := cb.sshBash(map[string]interface{}{
		"command":     "git reset --hard HEAD",
		"working_dir": "sub",
	})
	if strings.Contains(result, "拒绝执行高风险命令") {
		t.Fatalf("allow_once should bypass high-risk guardrail, got %q", result)
	}
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("approved command should proceed to SSH handler, got %q", result)
	}
	if gotReq.ToolName != remoteSSHBashToolName || gotReq.Path != "git reset --hard HEAD" || gotReq.ProjectPath != "/repo/project/sub" {
		t.Fatalf("approval request = %#v", gotReq)
	}
	if gotReq.Kind != remoteHighRiskApprovalKind || gotReq.AutoAllow || !strings.Contains(gotReq.Message, "拒绝执行高风险命令") {
		t.Fatalf("high-risk approval metadata = %#v", gotReq)
	}
}

func TestLocalCodingHighRiskApprovalCanBeUserApproved(t *testing.T) {
	var got ScopeApprovalRequest
	state := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		got = req
		return ScopeApprovalAllowOnce
	}, false)

	if msg := state.checkHighRisk("bash", "git reset --hard HEAD", `D:\testdriver`, `D:\testdriver`, "拒绝执行高风险命令"); msg != "" {
		t.Fatalf("allow_once should allow the guarded command, got %q", msg)
	}
	if got.ToolName != "bash" || got.Kind != localHighRiskApprovalKind || got.Path != "git reset --hard HEAD" || got.AutoAllow {
		t.Fatalf("local high-risk approval request = %#v", got)
	}
}

func TestLocalCodingHighRiskApprovalRejectsAllowDirectory(t *testing.T) {
	state := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalAllowDir
	}, false)
	if msg := state.checkHighRisk("bash", "git reset --hard HEAD", `D:\testdriver`, `D:\testdriver`, "拒绝执行高风险命令"); msg == "" {
		t.Fatal("allow_dir must not approve a high-risk command")
	}
}

func TestScopeApprovalTimeoutAlwaysDenies(t *testing.T) {
	for _, kind := range []string{"", remoteDirectoryWriteKind, remotePathAccessKind, localHighRiskApprovalKind, remoteHighRiskApprovalKind} {
		if got := remoteScopeApprovalTimeoutDecision(ScopeApprovalRequest{Kind: kind, AutoAllow: true}); got != ScopeApprovalDeny {
			t.Fatalf("timeout decision for kind %q = %q, want deny", kind, got)
		}
	}
}

func TestScopeApprovalAuditRecordsAllowedDecision(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	handler := &IMMessageHandler{app: app}
	t.Cleanup(func() {
		if app.auditLog != nil {
			_ = app.auditLog.Close()
		}
	})
	recordScopeApprovalAudit(handler, "scope_test", ScopeApprovalRequest{
		ToolName:    "bash",
		Path:        "git reset --hard HEAD",
		ProjectPath: `D:\testdriver`,
		Kind:        localHighRiskApprovalKind,
	}, ScopeApprovalAllowOnce, "user")

	app.ensureAuditLog()
	entries, err := app.auditLog.Query(security.AuditFilter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit entries = %d, err = %v", len(entries), err)
	}
	if entries[0].PolicyAction != security.PolicyUserOverride || !strings.Contains(entries[0].Result, "allow_once") {
		t.Fatalf("allowed approval audit = %#v", entries[0])
	}
	if entries[0].Arguments["decision"] != string(ScopeApprovalAllowOnce) || entries[0].Arguments["source"] != "user" {
		t.Fatalf("structured approval decision = %#v", entries[0].Arguments)
	}
}

func TestRemoteCodingSubAgentHighRiskBashFullAccessPersistsForTask(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{projectDir: "/repo/project"},
	}
	calls := 0
	cb.agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalFullAccess
	}, false)

	first := cb.sshBash(map[string]interface{}{"command": "git reset --hard HEAD"})
	second := cb.sshBash(map[string]interface{}{"command": "rm -rf build"})
	if strings.Contains(first, "拒绝执行高风险命令") || strings.Contains(second, "拒绝执行高风险命令") {
		t.Fatalf("full_access should bypass high-risk guardrail, first=%q second=%q", first, second)
	}
	if calls != 1 {
		t.Fatalf("full_access should avoid prompting again, calls=%d", calls)
	}
}

func TestRemoteCodingSubAgentHighRiskBashRejectsDirectoryApprovalDecision(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{projectDir: "/repo/project"},
	}
	cb.agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalAllowDir
	}, false)

	result := cb.sshBash(map[string]interface{}{"command": "git reset --hard HEAD"})
	if !strings.Contains(result, "拒绝执行高风险命令") {
		t.Fatalf("allow_dir should not bypass high-risk command guardrail, got %q", result)
	}
}

func TestRemoteCodingSubAgentSetCallbacksPreservesExplicitHighRiskApproval(t *testing.T) {
	agent := &RemoteCodingSubAgent{projectDir: "/repo/project"}
	calls := 0
	agent.SetHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalAllowOnce
	}, false)
	agent.SetCallbacks(nil, nil)

	cb := &remoteCodingCallbacks{agent: agent}
	result := cb.sshBash(map[string]interface{}{"command": "git reset --hard HEAD"})
	if strings.Contains(result, "拒绝执行高风险命令") {
		t.Fatalf("explicit approval callback should survive SetCallbacks, got %q", result)
	}
	if calls != 1 {
		t.Fatalf("explicit approval callback calls = %d, want 1", calls)
	}
}

func TestRemoteCodingSubAgentSetCallbacksPreservesAutoHighRiskFullAccess(t *testing.T) {
	agent := &RemoteCodingSubAgent{
		handler:    &IMMessageHandler{app: &App{}},
		projectDir: "/repo/project",
	}
	agent.setHighRiskApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		t.Fatalf("auto callback with full access should not be called")
		return ScopeApprovalDeny
	}, true, false, false)
	state := agent.highRiskApproval
	agent.SetCallbacks(nil, nil)
	if agent.highRiskApproval != state {
		t.Fatalf("auto callback refresh should preserve high-risk approval state")
	}

	cb := &remoteCodingCallbacks{agent: agent}
	result := cb.sshBash(map[string]interface{}{"command": "git reset --hard HEAD"})
	if strings.Contains(result, "拒绝执行高风险命令") {
		t.Fatalf("auto full_access should survive callback refresh, got %q", result)
	}
}

func TestRemoteCodingSubAgentAutoHighRiskRefreshDoesNotRevokeFullAccess(t *testing.T) {
	state := newRemoteHighRiskApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, true)
	state.configure(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false, true)

	result := state.check("git reset --hard HEAD", "/repo/project", "拒绝执行高风险命令")
	if result != "" {
		t.Fatalf("preserveFullAccess configure should not revoke full access, got %q", result)
	}
}

func TestRemoteShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := remoteShellQuote("/repo/O'Reilly/app")
	want := "'/repo/O'\\''Reilly/app'"
	if got != want {
		t.Fatalf("remoteShellQuote single quote escaping = %q, want %q", got, want)
	}
	command := fmt.Sprintf("cd %s && ls -la %s", got, remoteShellQuote("src's"))
	if strings.Contains(command, "O'Reilly") || !strings.Contains(command, "'\\''") {
		t.Fatalf("remote shell command should not contain unescaped single quote payload, got %q", command)
	}
}

func TestRemoteBashCommandWithExitMarker(t *testing.T) {
	command := remoteBashCommandWithExitMarker("/repo/O'Reilly/app", "pytest tests")
	for _, want := range []string{
		"cd '/repo/O'\\''Reilly/app'",
		"sh -lc 'pytest tests'",
		"EXIT: %s",
		"__maclaw_cmd_status=$?",
		"__maclaw_cd_status=$?",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("remote bash wrapper should contain %q, got %q", want, command)
		}
	}
	exitCommand := remoteBashCommandWithExitMarker("/repo", "echo before; exit 7")
	if !strings.Contains(exitCommand, "sh -lc 'echo before; exit 7'") || !strings.Contains(exitCommand, "printf '\\nEXIT: %s\\n'") {
		t.Fatalf("remote bash wrapper should run user command in child shell and still print EXIT, got %q", exitCommand)
	}
	quotedCommand := remoteBashCommandWithExitMarker("/repo", "python -c 'print(1)'")
	if !strings.Contains(quotedCommand, "python -c '\\''print(1)'\\'''") {
		t.Fatalf("remote bash wrapper should shell-quote nested single quotes, got %q", quotedCommand)
	}
	if got := remoteCodingToolOutcome("pytest failed\nEXIT: 1"); got != "failed" {
		t.Fatalf("remote bash EXIT marker should drive failed outcome, got %q", got)
	}
	if got := remoteCodingToolOutcome("1 passed\nEXIT: 0"); got != "success" {
		t.Fatalf("remote bash EXIT 0 marker should remain success, got %q", got)
	}
}

func TestRemoteCodingSubAgentKnowledgeSearchDoesNotRequireSSHHandler(t *testing.T) {
	store, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.SaveExperience(context.Background(), knowledge.CodingExperience{
		Title:            "Remote alias normalization",
		Category:         "pattern",
		Scope:            "language",
		Language:         "python",
		TriggerCondition: "remote alias normalization",
		Content:          "Remote coding tools should normalize old_string and replacement aliases before dispatch.",
		Status:           knowledge.CodingStatusActive,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}

	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			codingKB: store,
			// handler intentionally nil: coding knowledge search does not need SSH.
		},
	}
	result := cb.executeRemoteTool("coding_knowledge_search", `{"question":"remote alias normalization"}`)
	if strings.Contains(result, "handler unavailable") {
		t.Fatalf("coding knowledge search should not require SSH handler, got %q", result)
	}
	if !strings.Contains(result, "Remote alias normalization") || !strings.Contains(result, "old_string") {
		t.Fatalf("coding knowledge search should return matching experience, got %q", result)
	}

	sshResult := cb.executeRemoteTool("ssh_list_dir", "{}")
	if !strings.Contains(sshResult, "handler unavailable") {
		t.Fatalf("SSH tools should still require handler, got %q", sshResult)
	}

	unknownResult := cb.executeRemoteTool("not_a_remote_tool", "{}")
	if !strings.Contains(unknownResult, "unknown tool") || strings.Contains(unknownResult, "handler unavailable") {
		t.Fatalf("unknown tools should report unknown without requiring handler, got %q", unknownResult)
	}
}

func TestRemoteCodingToolOutcomeDetectsCommonFailureText(t *testing.T) {
	failures := []string{
		"错误: 需要 path 参数",
		"写入失败: disk full",
		"参数解析失败: invalid character",
		"error: permission denied",
		"Traceback (most recent call last):",
		"panic: runtime error",
		"exit status 1",
		"command exited with code 2",
		"process exited with code 137",
		"exit=1 accuracy=0.0",
		"ls: cannot access 'missing': No such file or directory",
		"bash: pytest: command not found",
		"ERROR: file not found: /repo/missing.py",
		"remote coding subagent: handler unavailable",
		"unknown tool: ssh_delete_all",
		"[failed] task task-123\nstatus: failed\nexit_code: 1\n--- latest log ---\npytest failed",
		"task task-123\nstatus: completed\nexit_code: 2\n--- latest log ---\ncommand failed",
		"task task-123\nstatus: killed\nexit_code: unknown",
		"EXIT: 1\ntraining failed",
		`{"status":"failed","exit_code":1}`,
		`{"state": "cancelled", "returncode": null}`,
		"exit code: 137\nkilled by oom",
		"return_code = 2\npytest failed",
	}
	for _, result := range failures {
		if got := remoteCodingToolOutcome(result); got != "failed" {
			t.Fatalf("remoteCodingToolOutcome(%q) = %q, want failed", result, got)
		}
	}

	successes := []string{
		"",
		"OK: replaced 1 occurrence",
		"✅ 已写入 /tmp/file.py (42 bytes, created=false)",
		"0 errors and 0 warnings",
		"[completed] task task-123\nstatus: completed\nexit_code: 0\n--- latest log ---\n1 passed",
		"EXIT: 0\nall checks passed",
		`{"status":"completed","exit_code":0}`,
		"returncode = 0\nall checks passed",
		"state: running\nexit_code: unknown",
		"command exited with code 0\nall checks passed",
		"exit status 0\nall checks passed",
		"exit=0 accuracy=91.5",
	}
	for _, result := range successes {
		if got := remoteCodingToolOutcome(result); got != "success" {
			t.Fatalf("remoteCodingToolOutcome(%q) = %q, want success", result, got)
		}
	}
}

func TestRemoteCodingToolOutcomeClassifiesGuardrailBlocks(t *testing.T) {
	for _, result := range []string{
		"\u62d2\u7edd\u6267\u884c\u9ad8\u98ce\u9669\u547d\u4ee4: rm -rf build",
		"refusing remote directory access outside the project: /srv/other",
		"refusing to modify remote path outside the project: /srv/other",
		"refusing to read remote path outside the project: /srv/other",
	} {
		if got := remoteCodingToolOutcome(result); got != "blocked" {
			t.Fatalf("remoteCodingToolOutcome(%q) = %q, want blocked", result, got)
		}
	}
	if got := remoteCodingToolOutcome("ninja: build stopped: subcommand failed"); got != "failed" {
		t.Fatalf("real command failure should remain failed, got %q", got)
	}
}
func TestRemoteCodingSubAgentToolFinishedEventUsesFailureOutcomeClassifier(t *testing.T) {
	var progress []string
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		onProgress: func(text string) {
			progress = append(progress, text)
		},
	}}

	result := cb.executeRemoteTool("ssh_list_dir", "{}")
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("expected handler unavailable result, got %q", result)
	}

	var finished CodingAgentEvent
	found := false
	for _, line := range progress {
		event, ok := parseCodingAgentEventText(line)
		if !ok || event.Event != codingAgentEventKindToolFinished.String() {
			continue
		}
		finished = event
		found = true
	}
	if !found {
		t.Fatalf("expected tool_finished event, progress=%#v", progress)
	}
	if finished.Outcome != "failed" {
		t.Fatalf("tool_finished outcome should use failure classifier, got event=%#v progress=%#v", finished, progress)
	}
	if !strings.Contains(finished.Summary, "handler unavailable") {
		t.Fatalf("failed tool event should include diagnostic summary, got %#v", finished)
	}
}

func TestRemoteCodingSubAgentToolFinishedEventIncludesRedactedBashCommand(t *testing.T) {
	var progress []string
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		onProgress: func(text string) {
			progress = append(progress, text)
		},
	}}

	result := cb.executeRemoteTool("ssh_bash", `{"command":"curl -H \"Authorization: Bearer secret-token\" https://example.test"}`)
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("expected handler unavailable result, got %q", result)
	}

	for _, line := range progress {
		event, ok := parseCodingAgentEventText(line)
		if !ok || event.Event != codingAgentEventKindToolFinished.String() {
			continue
		}
		want := `curl -H "Authorization: Bearer [redacted]" https://example.test`
		if event.Command != want {
			t.Fatalf("remote bash command = %q, want %q", event.Command, want)
		}
		return
	}
	t.Fatalf("expected remote tool_finished event, progress=%#v", progress)
}
func TestRemoteCodingToolFailureDiagnosticSeverityClassifier(t *testing.T) {
	if !remoteCodingToolFailureIsDiagnostic("ssh_bash", `{"command":"g++ --version 2>&1; cmake --version 2>&1"}`, "g++: command not found", "failed") {
		t.Fatal("remote diagnostic probe failure should be marked diagnostic")
	}
	if remoteCodingToolFailureIsDiagnostic("ssh_bash", `{"command":"cmake --build build"}`, "fatal error LNK1120: unresolved externals", "failed") {
		t.Fatal("remote build failure should not be marked diagnostic")
	}
	if remoteCodingToolFailureIsDiagnostic("ssh_bash", `{"command":"g++ --version 2>&1"}`, "permission denied while opening /srv/app/config.yml", "failed") {
		t.Fatal("remote hard failure result should override diagnostic probe command")
	}
	if !remoteCodingToolFailureIsDiagnostic("ssh_read_file", `{"path":"missing.go"}`, "No such file or directory", "failed") {
		t.Fatal("remote exploratory file probe miss should be marked diagnostic")
	}
	if !remoteCodingToolFailureIsDiagnostic("ssh_list_dir", `{"path":"does-not-exist"}`, "ls: cannot access 'does-not-exist': No such file or directory", "failed") {
		t.Fatal("remote exploratory directory probe miss should be marked diagnostic")
	}
	if remoteCodingToolFailureIsDiagnostic("ssh_read_file", `{"path":"main.go"}`, "permission denied while opening /srv/app/main.go", "failed") {
		t.Fatal("remote permission failure on read should not be marked diagnostic")
	}
	if remoteCodingToolFailureIsDiagnostic("ssh_write_file", `{"path":"main.go"}`, "写入失败: disk full", "failed") {
		t.Fatal("remote write failure should not be marked diagnostic")
	}
}

func TestRemoteCodingSubAgentLongCommandClassifierAvoidsShortCommandFalsePositives(t *testing.T) {
	longCommands := []string{
		"python train.py --epochs 10",
		"bash scripts/train_model.sh",
		"nohup python train.py > train.log 2>&1 &",
		"pip install -r requirements.txt",
		"conda install pytorch",
		"apt-get install -y build-essential",
		"git clone https://example.com/repo.git",
		"git pull origin main",
		"wget https://example.com/model.bin",
		"curl -L https://example.com/model.bin -o model.bin",
		"docker build -t app .",
		"make build",
		"make train",
		"cmake --build build",
	}
	for _, command := range longCommands {
		if !isLongRemoteCommand(command) {
			t.Fatalf("isLongRemoteCommand(%q) = false, want true", command)
		}
	}

	shortCommands := []string{
		"",
		"python3 -c \"print('train')\"",
		"python - <<'PY'\nprint('fit')\nPY",
		"python scripts/check_fit.py",
		"python scripts/constraint.py",
		"python scripts/validate_epoch_metrics.py",
		"bash scripts/check_train.sh",
		"bash scripts/run_checks.sh",
		"pytest tests/test_train.py",
		"make test",
		"make check",
		"cmake -S . -B build",
		"go test ./...",
	}
	for _, command := range shortCommands {
		if isLongRemoteCommand(command) {
			t.Fatalf("isLongRemoteCommand(%q) = true, want false", command)
		}
	}
}

func TestRemoteCodingSubAgentRawTextArgumentsPreserveWhitespace(t *testing.T) {
	args := map[string]interface{}{
		"path":    "  src/main.py  ",
		"content": "\n  print('hello')\n",
		"old_str": "  old text\n",
		"new_str": "\nnew text  ",
	}

	if got := remoteArgStr(args, "path"); got != "src/main.py" {
		t.Fatalf("remoteArgStr should trim control/path args, got %q", got)
	}
	content, ok := remoteArgRawStr(args, "content")
	if !ok || content != "\n  print('hello')\n" {
		t.Fatalf("remoteArgRawStr should preserve content whitespace, got ok=%v value=%q", ok, content)
	}
	oldStr, ok := remoteArgRawStr(args, "old_str")
	if !ok || oldStr != "  old text\n" {
		t.Fatalf("remoteArgRawStr should preserve old_str whitespace, got ok=%v value=%q", ok, oldStr)
	}
	newStr, ok := remoteArgRawStr(args, "new_str")
	if !ok || newStr != "\nnew text  " {
		t.Fatalf("remoteArgRawStr should preserve new_str whitespace, got ok=%v value=%q", ok, newStr)
	}

	emptyContent, ok := remoteArgRawStr(map[string]interface{}{"content": ""}, "content")
	if !ok || emptyContent != "" {
		t.Fatalf("empty content should be present and preserved, got ok=%v value=%q", ok, emptyContent)
	}
	if _, ok := remoteArgRawStr(map[string]interface{}{}, "content"); ok {
		t.Fatalf("missing content should not be reported as present")
	}
}

func TestRemoteCodingSubAgentEditFileRequiresExplicitNewStringButAllowsEmptyReplacement(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: "/repo"}}

	missingNew := cb.sshEditFile(map[string]interface{}{
		"path":    "main.py",
		"old_str": "old",
	})
	if !strings.Contains(missingNew, "new_str") {
		t.Fatalf("missing new_str should be rejected before execution, got %q", missingNew)
	}

	result := cb.sshEditFile(map[string]interface{}{
		"path":    "main.py",
		"old_str": "old",
		"new_str": "",
	})
	if !strings.Contains(result, "handler unavailable") {
		t.Fatalf("empty new_str should pass validation and reach execution, got %q", result)
	}
}

func TestRemoteCodingSubAgentPythonCommandsEncodePaths(t *testing.T) {
	path := "/repo/src/O'Reilly $HOME/main.py"
	pathB64 := base64.StdEncoding.EncodeToString([]byte(path))

	writeCmd := remoteWriteFilePythonCommand(path, "print('hello')\n")
	if !strings.Contains(writeCmd, pathB64) {
		t.Fatalf("write command should contain base64 path %q, got %q", pathB64, writeCmd)
	}
	if strings.Contains(writeCmd, path) || strings.Contains(writeCmd, "pathlib.Path('/repo") {
		t.Fatalf("write command should not embed raw path in Python string, got %q", writeCmd)
	}
	if strings.Contains(writeCmd, "\n\timport ") || strings.Contains(writeCmd, "\n\tp =") {
		t.Fatalf("write command should not embed Go indentation into Python source, got %q", writeCmd)
	}
	if !strings.Contains(writeCmd, "base64.b64decode") {
		t.Fatalf("write command should decode path/content via base64, got %q", writeCmd)
	}
	if !strings.Contains(writeCmd, "created = not p.exists()") || !strings.Contains(writeCmd, "created=") {
		t.Fatalf("write command should report whether it created or overwrote the file, got %q", writeCmd)
	}

	largeCmd := remoteWriteFileLargeDecodeCommand(path, "/tmp/maclaw_write_123")
	if !strings.Contains(largeCmd, pathB64) {
		t.Fatalf("large-write decode command should contain base64 path %q, got %q", pathB64, largeCmd)
	}
	if strings.Contains(largeCmd, path) || strings.Contains(largeCmd, "pathlib.Path('/repo") {
		t.Fatalf("large-write decode command should not embed raw path in Python string, got %q", largeCmd)
	}
	if strings.Contains(largeCmd, "\n\timport ") || strings.Contains(largeCmd, "\n\tp =") {
		t.Fatalf("large-write decode command should not embed Go indentation into Python source, got %q", largeCmd)
	}
	if !strings.Contains(largeCmd, "rm -f '/tmp/maclaw_write_123'") {
		t.Fatalf("large-write decode command should shell-quote tmp cleanup path, got %q", largeCmd)
	}
	if !strings.Contains(largeCmd, "created = not p.exists()") || !strings.Contains(largeCmd, "created=") {
		t.Fatalf("large-write command should report whether it created or overwrote the file, got %q", largeCmd)
	}

	chunkCmd := remoteWriteFileLargeChunkCommand("/tmp/maclaw write '123'", "YWJjZA==", false)
	for _, want := range []string{"printf %s 'YWJjZA==' > '/tmp/maclaw write '\\''123'\\'''", "printf %s"} {
		if !strings.Contains(chunkCmd, want) {
			t.Fatalf("large-write chunk command should contain %q, got %q", want, chunkCmd)
		}
	}
	if strings.Contains(chunkCmd, "echo -n") {
		t.Fatalf("large-write chunk command should use printf instead of echo -n, got %q", chunkCmd)
	}
	appendChunkCmd := remoteWriteFileLargeChunkCommand("/tmp/maclaw_write_123", "ZWY=", true)
	if !strings.Contains(appendChunkCmd, " >> '/tmp/maclaw_write_123'") {
		t.Fatalf("append chunk command should append to quoted tmp path, got %q", appendChunkCmd)
	}

	editCmd := remoteEditFilePythonCommand(path, "  old\n", "")
	if !strings.Contains(editCmd, pathB64) {
		t.Fatalf("edit command should contain base64 path %q, got %q", pathB64, editCmd)
	}
	if strings.Contains(editCmd, path) || strings.Contains(editCmd, "pathlib.Path('/repo") {
		t.Fatalf("edit command should not embed raw path in Python string, got %q", editCmd)
	}
	if strings.Contains(editCmd, "\n\timport ") || strings.Contains(editCmd, "\n\tif ") {
		t.Fatalf("edit command should not embed Go indentation into Python source, got %q", editCmd)
	}
	if !strings.Contains(editCmd, base64.StdEncoding.EncodeToString([]byte("  old\n"))) ||
		!strings.Contains(editCmd, base64.StdEncoding.EncodeToString([]byte(""))) {
		t.Fatalf("edit command should base64 encode old/new strings, got %q", editCmd)
	}
}

func TestRemoteCodingSubAgentReadFileRangeCommandAndArgs(t *testing.T) {
	path := "/repo/src/O'Reilly $HOME/main.py"
	pathB64 := base64.StdEncoding.EncodeToString([]byte(path))
	cmd := remoteReadFileRangePythonCommand(path, 25, 40)
	if !strings.Contains(cmd, pathB64) {
		t.Fatalf("read range command should contain base64 path %q, got %q", pathB64, cmd)
	}
	if strings.Contains(cmd, path) || strings.Contains(cmd, "pathlib.Path('/repo") {
		t.Fatalf("read range command should not embed raw path in Python string, got %q", cmd)
	}
	for _, want := range []string{"start = 25", "limit = 40", "with p.open", "errors=", "for lineno, line in enumerate(f, start=1)", "shown >= limit", "remote read_file EOF", "remote read_file truncated", "offset=%d"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("read range command should contain %q, got %q", want, cmd)
		}
	}
	if strings.Contains(cmd, "read_text(encoding='utf-8').splitlines") || strings.Contains(cmd, "lines[begin:end]") {
		t.Fatalf("read range command should stream line ranges instead of loading the full file, got %q", cmd)
	}
	if strings.Contains(cmd, "sys.stdout.buffer.write(p.read_bytes())") {
		t.Fatalf("read range command should not dump full binary/non-UTF8 files, got %q", cmd)
	}
	if strings.Contains(cmd, "%!d(MISSING)") {
		t.Fatalf("read range command should preserve Python percent-format placeholders, got %q", cmd)
	}
	if strings.Contains(cmd, "\n\timport ") || strings.Contains(cmd, "\n\tfor ") || strings.Contains(cmd, "\n\tif ") {
		t.Fatalf("read range command should not embed Go indentation into Python source, got %q", cmd)
	}
	if !strings.Contains(cmd, "remote read_file binary/non-UTF8") || !strings.Contains(cmd, "text line range unavailable") {
		t.Fatalf("read range command should report bounded binary/non-UTF8 diagnostics, got %q", cmd)
	}

	cmd = remoteReadFileRangePythonCommand(path, 0, 0)
	if !strings.Contains(cmd, "start = 1") || !strings.Contains(cmd, "limit = 200") {
		t.Fatalf("read range command should default to offset=1 limit=200, got %q", cmd)
	}

	args := map[string]interface{}{
		"startLine": "7",
		"lines":     float64(9999),
	}
	if got := remoteArgInt(args, 0, 0, 1000000, "offset", "start_line", "start", "startLine"); got != 7 {
		t.Fatalf("remoteArgInt should read string alias startLine, got %d", got)
	}
	if got := remoteArgInt(args, 0, 0, 2000, "limit", "lines", "num_lines", "line_count"); got != 2000 {
		t.Fatalf("remoteArgInt should clamp max value, got %d", got)
	}
	if got := remoteArgInt(map[string]interface{}{"offset": -5}, 10, 0, 2000, "offset"); got != 0 {
		t.Fatalf("remoteArgInt should clamp min value, got %d", got)
	}
}

func TestCodingSubAgentEditLinesAliasesPassExecution(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "main.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: project}}

	readResult := cb.executeToolWithOutcome("read_file", `{"path":"main.txt"}`)
	if readResult.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("read_file before edit_lines outcome=%q result=%s", readResult.Outcome, readResult.Text)
	}
	result := cb.executeToolWithOutcome("edit_lines", `{"file":"main.txt","action":"update","startLine":2,"endLine":3,"content":"TWO"}`)
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("edit_lines aliases should execute successfully, outcome=%q result=%s", result.Outcome, result.Text)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if got := string(data); got != "one\nTWO\n" {
		t.Fatalf("edit_lines aliases produced unexpected content %q", got)
	}
}

func TestCodingSubAgentEditFileAliasesPassArgumentValidation(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("edit_file", `{"path":"main.go","old_content":"old","new_content":"new"}`)

	if result.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("edit_file aliases should pass argument validation and reach host guard, outcome=%q result=%s", result.Outcome, result.Text)
	}
	if strings.Contains(result.Text, "missing required argument") || strings.Contains(result.Text, "old_string") || strings.Contains(result.Text, "new_string") {
		t.Fatalf("edit_file aliases should not fail required-argument validation, got %q", result.Text)
	}
}

func TestCodingSubAgentManageSkillRunMissingNameUsesStandardArgumentError(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "impeccable", Description: "Audit and polish frontend UI"},
		},
	}

	result := cb.executeManageSkill(map[string]interface{}{"action": "run"})
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("outcome = %s, want failed", result.Outcome)
	}
	if !strings.Contains(result.Text, `missing required argument "name"`) ||
		!strings.Contains(result.Text, `Example valid arguments:`) ||
		!strings.Contains(result.Text, `"args":{"input":"task-specific instructions"}`) {
		t.Fatalf("missing manage_skill name should use standard argument guidance, got %q", result.Text)
	}
}

func TestRejectedDynamicToolArgumentFailureIsTrackedForAudit(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		matchedSkills: []codingSubAgentSkillMatch{
			{Name: "impeccable", Description: "Audit and polish frontend UI"},
		},
	}

	result := cb.executeToolWithOutcome("manage_skill", `{"action":"run"}`)
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("outcome = %s, want failed", result.Outcome)
	}
	tools := cb.getDynamicToolsRun()
	if len(tools) != 1 {
		t.Fatalf("dynamic tool failures tracked = %d, want 1: %#v", len(tools), tools)
	}
	if tools[0].Tool != "manage_skill" || tools[0].Succeeded || !strings.Contains(tools[0].Summary, "missing required argument") {
		t.Fatalf("unexpected tracked dynamic failure: %#v", tools[0])
	}
}

func TestCanonicalCodingSubAgentToolNameAcceptsModelCasing(t *testing.T) {
	if got := canonicalCodingSubAgentToolName("glob"); got != "Glob" {
		t.Fatalf("canonical glob = %q, want Glob", got)
	}
	if got := canonicalCodingSubAgentToolName(" read_file "); got != "read_file" {
		t.Fatalf("canonical read_file = %q, want read_file", got)
	}
	if got := canonicalCodingSubAgentToolName("grep_search"); got != "ripgrep" {
		t.Fatalf("canonical grep_search = %q, want ripgrep", got)
	}
	if got := canonicalCodingSubAgentToolName("search_files"); got != "Glob" {
		t.Fatalf("canonical search_files = %q, want Glob", got)
	}
}

func TestCodingSubAgentToolNameListIsStable(t *testing.T) {
	names := codingSubAgentToolNameList()
	if len(names) == 0 {
		t.Fatal("tool name list should not be empty")
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("tool name list should be sorted for stable unknown-tool guidance, got %#v", names)
	}
}

func TestCodingSubAgentToolNameAliasesAvoidUnknownToolFailure(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("grep_search", `{}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("grep_search alias without pattern outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if strings.Contains(result.Text, "unknown tool") {
		t.Fatalf("grep_search alias should reach ripgrep argument validation, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "missing required argument") || !strings.Contains(result.Text, `"pattern"`) {
		t.Fatalf("grep_search alias should produce ripgrep pattern guidance, got %q", result.Text)
	}
}

func TestCodingSubAgentMaxIterationsUsesRuntimeBudget(t *testing.T) {
	loopCtx := NewLoopContext("subagent-budget", 180, nil)
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{loopCtx: loopCtx}}

	if got := cb.GetMaxIterations(); got != 180 {
		t.Fatalf("max iterations = %d, want runtime budget", got)
	}
}

func TestCodingSubAgentMaxIterationsFallsBackToDefaultBudget(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{}}

	got := cb.GetMaxIterations()
	if got != codingSubAgentPerTaskMaxIterations {
		t.Fatalf("max iterations = %d, want per-task cap %d", got, codingSubAgentPerTaskMaxIterations)
	}
}

func TestBuildCodingSubAgentSystemPromptIncludesWindowsShellContract(t *testing.T) {
	if normalizedRemotePlatform() != "windows" {
		t.Skip("Windows shell contract is platform-specific")
	}
	prompt := buildCodingSubAgentSystemPrompt(&TaskItem{Index: 0, Title: "Task"}, "D:\\workprj\\snake", "", "", nil)
	for _, want := range []string{"Windows shell contract", "mkdir -p", "&&", "working_dir", "CMake generators"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestCodingSubAgentBuildSystemPromptCachesPerTask(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: t.TempDir()},
		task:     &TaskItem{Index: 1, Title: "cache prompt", Description: "first description"},
		reqCtx:   "initial requirement",
	}

	first := cb.BuildSystemPrompt("fix", true)
	if first == "" || cb.cachedSystemPrompt == "" {
		t.Fatalf("BuildSystemPrompt should cache a non-empty prompt")
	}
	cb.reqCtx = "changed requirement after first build"
	cb.prevOutputs = []string{"late output should not rebuild prompt"}
	second := cb.BuildSystemPrompt("different user text", false)
	if second != first {
		t.Fatalf("BuildSystemPrompt should reuse per-task cached prompt")
	}
	if strings.Contains(second, "changed requirement") || strings.Contains(second, "late output") {
		t.Fatalf("cached prompt should not be rebuilt from mutated callback fields: %q", second)
	}
}

func TestCodingSubAgentDynamicSelectionTextIncludesTaskSignals(t *testing.T) {
	if got := codingSubAgentDynamicSelectionText(nil); got != "" {
		t.Fatalf("nil task should produce empty selection text, got %q", got)
	}

	text := codingSubAgentDynamicSelectionText(&TaskItem{
		Title:       "Update dashboard export",
		Description: "Wire the CSV export button to the reporting service.",
		Files:       []string{"hub/web/reporting/export.ts", "hub/web/reporting/export.ts", ""},
		AcceptanceCriteria: []string{
			"CSV export keeps filtered rows",
			"unit tests cover export failure handling",
		},
	})

	for _, want := range []string{
		"Update dashboard export",
		"Wire the CSV export button",
		"hub/web/reporting/export.ts",
		"CSV export keeps filtered rows",
		"unit tests cover export failure handling",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dynamic selection text missing %q: %q", want, text)
		}
	}
	if strings.Count(text, "hub/web/reporting/export.ts") != 1 {
		t.Fatalf("dynamic selection text should deduplicate files, got %q", text)
	}
}

func TestCodingSubAgentDynamicSelectionTextCapsTaskSignals(t *testing.T) {
	files := make([]string, 0, codingSubAgentTaskFilesMax+2)
	for i := 0; i < codingSubAgentTaskFilesMax+2; i++ {
		files = append(files, fmt.Sprintf("src/feature_%02d_with_extra_detail.go", i))
	}
	criteria := make([]string, 0, codingSubAgentAcceptanceCriteriaMax+3)
	for i := 0; i < codingSubAgentAcceptanceCriteriaMax+3; i++ {
		criteria = append(criteria, fmt.Sprintf("criterion %02d requires focused verification", i))
	}

	text := codingSubAgentDynamicSelectionText(&TaskItem{
		Title:              "Large dynamic task",
		Description:        strings.Repeat("Long description sentence. ", 200),
		Files:              files,
		AcceptanceCriteria: criteria,
	})

	if len([]rune(text)) > codingSubAgentDynamicSelectionTextMaxRunes+len([]rune("…（已截断）")) {
		t.Fatalf("dynamic selection text exceeded cap: %d runes", len([]rune(text)))
	}
	if strings.Contains(text, "src/feature_31_with_extra_detail.go") {
		t.Fatalf("dynamic selection files should be capped, got %q", text)
	}
	if !strings.Contains(text, "还有 2 项未展开") {
		t.Fatalf("dynamic selection files should report remaining count, got %q", text)
	}
	if strings.Contains(text, "criterion 22") {
		t.Fatalf("dynamic selection criteria should be capped, got %q", text)
	}
	if !strings.Contains(text, "还有 3 项未展开") {
		t.Fatalf("dynamic selection criteria should report remaining count, got %q", text)
	}
}
func TestCodingSubAgentDynamicSelectionTextIncludesCappedContextSignals(t *testing.T) {
	var prevOutputs []string
	for i := 0; i < codingSubAgentDynamicSelectionPrevOutputsMax+2; i++ {
		prevOutputs = append(prevOutputs, fmt.Sprintf("Previous output %02d mentions browser MCP screenshot flow", i))
	}
	text := codingSubAgentDynamicSelectionTextWithContext(
		&TaskItem{Title: "Polish status panel"},
		"Requirement context mentions browser screenshot review and MCP automation.",
		"Design context says the status panel needs compact failed-state diagnostics.",
		prevOutputs,
	)

	for _, want := range []string{"browser screenshot review", "MCP automation", "failed-state diagnostics", "Previous output 00"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dynamic selection context text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Previous output 06") {
		t.Fatalf("dynamic selection previous outputs should be capped, got %q", text)
	}
	if !strings.Contains(text, "还有 2 项未展开") {
		t.Fatalf("dynamic selection previous outputs should report remaining count, got %q", text)
	}
}

func TestCodingSubAgentDynamicToolSelectionCachesEmptyResults(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: t.TempDir()},
		task:     &TaskItem{Index: 1, Title: "plain Go fix", Description: "rename local variable"},
	}

	_ = cb.BuildSystemPrompt("fix", true)
	if !cb.matchedSkillsSelected || !cb.matchedMCPToolsSelected {
		t.Fatalf("BuildSystemPrompt should mark dynamic selection attempted, skills=%v mcp=%v", cb.matchedSkillsSelected, cb.matchedMCPToolsSelected)
	}

	tools := cb.BuildTools("fix")
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == "manage_skill" || name == "call_mcp_tool" {
			t.Fatalf("BuildTools should not add dynamic tools for cached empty selection, unexpectedly added %s", name)
		}
	}
}

func TestBuildCodingToolDefinitions_TokenEstimate(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()

	// Estimate total token cost of tool definitions.
	// The compact tool belt should stay small enough for a clean coding context.
	totalChars := 0
	for _, tool := range tools {
		// Rough estimate: JSON marshal and count chars.
		data, _ := json.Marshal(tool)
		totalChars += len(data)
	}

	// At ~2.5 bytes/token, total should be < 3000 tokens.
	estimatedTokens := totalChars * 10 / 25
	if estimatedTokens > 3000 {
		t.Errorf("tool definitions too large: ~%d tokens (chars=%d), expected <3000", estimatedTokens, totalChars)
	}
	t.Logf("tool definitions: %d chars, ~%d tokens", totalChars, estimatedTokens)
}

func TestTruncateRunesForSubAgent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		max       int
		wantTrunc bool
	}{
		{"short", "hello", 10, false},
		{"exact", "hello", 5, false},
		{"truncate", "hello world this is a long string", 10, true},
		{"chinese", "这是一段中文测试文本，用于验证截断功能", 10, true},
		{"paragraph_boundary", "第一段\n\n第二段\n\n第三段很长很长", 15, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateRunesForSubAgent(tt.input, tt.max)
			if tt.wantTrunc {
				if !strings.Contains(result, "截断") {
					t.Errorf("expected truncation marker, got: %s", result)
				}
				if len([]rune(result)) > tt.max+20 { // allow some slack for the marker
					t.Errorf("result too long: %d runes", len([]rune(result)))
				}
			} else {
				if result != tt.input {
					t.Errorf("expected no truncation, got: %s", result)
				}
			}
		})
	}
}

func TestBuildCodingToolDefinitionsFromRegistry_NilHandler(t *testing.T) {
	// When handler is nil, should fall back to inline definitions.
	tools := buildCodingToolDefinitionsFromRegistry(nil)
	if len(tools) != 9 {
		t.Fatalf("expected 9 fallback tools, got %d", len(tools))
	}
}

func TestBuildCodingToolDefinitionsFallbackUsesCoreRegistryPlusExtras(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()
	names := make(map[string]bool)
	descs := make(map[string]string)
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
		descs[name], _ = fn["description"].(string)
	}
	for _, name := range []string{"Glob", "ripgrep", "bash", "read_file", "write_file", "edit_file", "list_directory", "edit_lines", "git_diff"} {
		if !names[name] {
			t.Fatalf("missing fallback tool %q", name)
		}
	}
	if !strings.Contains(descs["ripgrep"], "递归搜索") {
		t.Fatalf("expected ripgrep description from core registry, got %q", descs["ripgrep"])
	}
	if !strings.Contains(descs["edit_lines"], "按行号") {
		t.Fatalf("expected edit_lines extra definition, got %q", descs["edit_lines"])
	}
}

func TestBuildCodingToolDefinitionsFallbackReturnsDefensiveCopy(t *testing.T) {
	first := buildCodingToolDefinitionsFallback()
	writeFile := codingToolDefinitionForTest(t, first, "write_file")
	params, _ := writeFile["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	content, _ := props["content"].(map[string]interface{})
	content["description"] = "mutated fallback cache"

	second := buildCodingToolDefinitionsFallback()
	writeFile = codingToolDefinitionForTest(t, second, "write_file")
	params, _ = writeFile["parameters"].(map[string]interface{})
	props, _ = params["properties"].(map[string]interface{})
	contentDesc := codingToolPropDescriptionForTest(props["content"])
	if strings.Contains(contentDesc, "mutated fallback cache") {
		t.Fatalf("fallback tool cache should return defensive copies, got description %q", contentDesc)
	}
	if !strings.Contains(contentDesc, "No length limit") {
		t.Fatalf("fallback tool cache lost write_file hint, got description %q", contentDesc)
	}
}

func TestCodingSubAgentToolNames_SingleSource(t *testing.T) {
	// Verify that codingSubAgentToolOrder is the single source of truth:
	// every tool in the fallback definitions must be in the derived map.
	fallback := buildCodingToolDefinitionsFallback()
	for _, tool := range fallback {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !codingSubAgentToolNames[name] {
			t.Errorf("fallback tool %q not in codingSubAgentToolNames", name)
		}
	}
	// And the map should have exactly as many entries as the fallback.
	if len(codingSubAgentToolNames) != len(fallback) {
		t.Errorf("codingSubAgentToolNames has %d entries, fallback has %d tools; they should match",
			len(codingSubAgentToolNames), len(fallback))
	}
	if len(codingSubAgentToolOrder) != len(codingSubAgentToolNames) {
		t.Errorf("codingSubAgentToolOrder has %d entries, map has %d; they should match",
			len(codingSubAgentToolOrder), len(codingSubAgentToolNames))
	}
}

func TestCodingSubAgentExecuteTaskRejectsNilTask(t *testing.T) {
	result := (&CodingSubAgent{}).ExecuteTask(nil, "", "", nil)
	if result == nil || result.Status != TaskExecFailed || !strings.Contains(result.Error, "nil") {
		t.Fatalf("expected nil task failure, got %#v", result)
	}
}

func TestCodingSubAgentExecuteTaskRejectsNilReceiver(t *testing.T) {
	var sa *CodingSubAgent
	result := sa.ExecuteTask(&TaskItem{Index: 0, Title: "Task A"}, "", "", nil)
	if result == nil || result.Status != TaskExecFailed || !strings.Contains(result.Error, "nil") {
		t.Fatalf("expected nil receiver failure, got %#v", result)
	}
}

func TestCodingSubAgentExecuteToolStopsWhenCancelled(t *testing.T) {
	ctx := NewLoopContext("coding-test", 1, nil)
	ctx.Cancel()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{loopCtx: ctx, projectPath: t.TempDir()},
	}

	result := cb.ExecuteTool("ripgrep", `{"pattern":"package"}`)
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("expected cancelled tool result, got %q", result)
	}
}

func TestCodingSubAgentToolContextCancelDoesNotCancelLoop(t *testing.T) {
	ctx := NewLoopContext("coding-test", 1, nil)
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{loopCtx: ctx, projectPath: t.TempDir()},
	}

	toolCtx, cancel := cb.toolContext()
	cancel()

	if err := toolCtx.Err(); err != context.Canceled {
		t.Fatalf("tool context error = %v, want context.Canceled", err)
	}
	if ctx.IsCancelled() {
		t.Fatalf("tool context cleanup cancelled the parent loop")
	}
}

func TestCodingSubAgentExecuteToolHandlesMissingHostHandler(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
	}

	result := cb.ExecuteTool("read_file", `{"path":"main.go"}`)
	if !strings.Contains(result, "host tool handler is unavailable") {
		t.Fatalf("expected missing handler error, got %q", result)
	}
	violations := cb.getGuardrailViolations()
	if len(violations) != 1 || violations[0].Tool != "read_file" {
		t.Fatalf("expected missing handler guardrail, got %#v", violations)
	}
}

func TestCodingSubAgentExecuteToolEmitsStructuredToolEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 7, Title: "Inspect parser"},
		subagent: &CodingSubAgent{
			projectPath: t.TempDir(),
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	_ = cb.ExecuteTool("read_file", `{"path":"main.go"}`)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, "Coding Agent Event:") ||
		!strings.Contains(joined, `"event":"tool_started"`) ||
		!strings.Contains(joined, `"event":"tool_finished"`) ||
		!strings.Contains(joined, `"phase":"running"`) ||
		!strings.Contains(joined, `"task_id":"T7"`) ||
		!strings.Contains(joined, `"detail":"read_file"`) ||
		!strings.Contains(joined, `"outcome":"blocked"`) ||
		!strings.Contains(joined, `"summary":"coding subagent host tool handler is unavailable"`) ||
		!strings.Contains(joined, `"duration_ms":`) {
		t.Fatalf("expected structured tool progress, got %#v", progress)
	}
}

func TestCodingSubAgentToolFinishedEventPrefersStderrDiagnostic(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 7, Title: "Inspect parser"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitToolFinishedEvent("bash", "", "ordinary prelude\n[stderr] compiler: missing symbol\ncommand exited with code 2", codingToolOutcomeFailed, 25*time.Millisecond)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"tool_finished"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"summary":"compiler: missing symbol"`) {
		t.Fatalf("failed tool event should surface stderr diagnostic, got %#v", progress)
	}
}

func TestCodingSubAgentToolFinishedEventIncludesRedactedBashCommand(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 7, Title: "Run blocked command"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitToolFinishedEvent("bash", `{"command":"curl -H \"Authorization: Bearer secret-token\" https://example.test"}`, "blocked by guardrail", codingToolOutcomeBlocked, 25*time.Millisecond)
	cb.emitToolFinishedEvent("read_file", `{"path":"README.md"}`, "blocked by guardrail", codingToolOutcomeBlocked, 25*time.Millisecond)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"command":"curl -H \"Authorization: Bearer [redacted]\" https://example.test"`) {
		t.Fatalf("bash tool event should include redacted command, got %#v", progress)
	}
	if strings.Contains(joined, "secret-token") {
		t.Fatalf("bash tool event should not leak command secrets, got %#v", progress)
	}
	if strings.Contains(progress[len(progress)-1], `"command":`) {
		t.Fatalf("non-bash tool event should not include command, got %q", progress[len(progress)-1])
	}
}
func TestCodingSubAgentToolFinishedEventMarksDiagnosticProbeSeverity(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 7, Title: "Probe compiler"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitToolFinishedEvent("bash", `{"command":"g++ --version 2>&1; cmake --version 2>&1"}`, "PowerShell exception: 无法将“g++”项识别为 cmdlet", codingToolOutcomeFailed, 25*time.Millisecond)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"tool_finished"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"severity":"diagnostic"`) {
		t.Fatalf("diagnostic probe tool event should carry diagnostic severity, got %#v", progress)
	}

	progress = nil
	cb.emitToolFinishedEvent("bash", `{"command":"cmake --build build"}`, "fatal error LNK1120: unresolved externals", codingToolOutcomeFailed, 25*time.Millisecond)
	if joined := strings.Join(progress, "\n"); strings.Contains(joined, `"severity":"diagnostic"`) {
		t.Fatalf("real build failure should not carry diagnostic severity, got %#v", progress)
	}

	progress = nil
	cb.emitToolFinishedEvent("bash", `{"command":"g++ --version 2>&1"}`, "permission denied while opening /srv/app/config.yml", codingToolOutcomeFailed, 25*time.Millisecond)
	if joined := strings.Join(progress, "\n"); strings.Contains(joined, `"severity":"diagnostic"`) {
		t.Fatalf("hard failure result should override diagnostic probe command, got %#v", progress)
	}

	progress = nil
	cb.emitToolFinishedEvent("bash", `{"command":"g++ --version 2>&1"}`, `FAIL at D:\test\test_hello.cpp:11: CHECK (result == "Hello, World!")`, codingToolOutcomeFailed, 25*time.Millisecond)
	if joined := strings.Join(progress, "\n"); strings.Contains(joined, `"severity":"diagnostic"`) {
		t.Fatalf("test assertion failure should override diagnostic probe command, got %#v", progress)
	}
}

func TestCodingSubAgentToolFinishedEventPrefersLikelyStderrFailure(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 7, Title: "Inspect parser"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitToolFinishedEvent("bash", "", "[stderr] running package github.com/acme/app\n[stderr] src/main.go:12: error: missing symbol\ncommand exited with code 2", codingToolOutcomeFailed, 25*time.Millisecond)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"summary":"src/main.go:12: error: missing symbol"`) || strings.Contains(joined, `"summary":"running package`) {
		t.Fatalf("failed tool event should prefer actionable stderr failure line, got %#v", progress)
	}
}

func TestCodingSubAgentRejectsNonStringToolPathArgument(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "sentinel.txt"), []byte("do not list root"), 0644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	result := cb.executeToolWithOutcome("list_directory", `{"path":123}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("non-string path outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, `invalid argument type for "path"`) || !strings.Contains(result.Text, "expected string") {
		t.Fatalf("non-string path should produce a targeted type error, got %q", result.Text)
	}
	if strings.Contains(result.Text, "sentinel.txt") {
		t.Fatalf("non-string path should not silently list the project root, got %q", result.Text)
	}
}

func TestCodingSubAgentRejectsNonStringBashWorkingDir(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("bash", `{"command":"Write-Output should-not-run","working_dir":123}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("non-string working_dir outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, `invalid argument type for "working_dir"`) || !strings.Contains(result.Text, "expected string") {
		t.Fatalf("non-string working_dir should produce a targeted type error, got %q", result.Text)
	}
	if len(cb.getCommandsRun()) != 0 {
		t.Fatalf("bash command with invalid working_dir should not execute or be tracked, got %#v", cb.getCommandsRun())
	}
}
func TestCodingSubAgentRejectsMissingRequiredToolArguments(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}
	cases := []struct {
		name  string
		tool  string
		args  string
		field string
	}{
		{name: "read path", tool: "read_file", args: `{}`, field: "path"},
		{name: "blank read path", tool: "read_file", args: `{"path":"  "}`, field: "path"},
		{name: "bash command", tool: "bash", args: `{}`, field: "command"},
		{name: "blank bash command", tool: "bash", args: `{"command":"  "}`, field: "command"},
		{name: "edit_lines start", tool: "edit_lines", args: `{"path":"main.go","operation":"replace"}`, field: "start_line"},
		{name: "edit_lines replace end", tool: "edit_lines", args: `{"path":"main.go","operation":"replace","start_line":1,"content":"x"}`, field: "end_line"},
		{name: "edit_lines replace content", tool: "edit_lines", args: `{"path":"main.go","operation":"replace","start_line":1,"end_line":1}`, field: "content"},
		{name: "edit_lines insert content", tool: "edit_lines", args: `{"path":"main.go","operation":"insert","start_line":1}`, field: "content"},
		{name: "edit_lines delete end", tool: "edit_lines", args: `{"path":"main.go","operation":"delete","start_line":1}`, field: "end_line"},
		{name: "knowledge query", tool: "knowledge_search", args: `{}`, field: "query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeCommands := len(cb.getCommandsRun())
			result := cb.executeToolWithOutcome(tc.tool, tc.args)
			if result.Outcome != codingToolOutcomeFailed {
				t.Fatalf("missing %s outcome = %q, want failed; result=%s", tc.field, result.Outcome, result.Text)
			}
			if !strings.Contains(result.Text, "missing required argument") || !strings.Contains(result.Text, fmt.Sprintf("%q", tc.field)) {
				t.Fatalf("missing %s should produce targeted required-argument error, got %q", tc.field, result.Text)
			}
			if len(cb.getCommandsRun()) != beforeCommands {
				t.Fatalf("tool with missing %s should not execute commands, commands=%#v", tc.field, cb.getCommandsRun())
			}
		})
	}

	if result, rejected := rejectInvalidCodingSubAgentToolArgumentTypes("write_file", map[string]interface{}{"path": "out.txt", "content": ""}); rejected {
		t.Fatalf("write_file empty content should be allowed by argument validation, got %#v", result)
	}
	if result, rejected := rejectInvalidCodingSubAgentToolArgumentTypes("edit_lines", map[string]interface{}{"path": "main.go", "operation": "replace", "start_line": 1, "end_line": 1, "content": ""}); rejected {
		t.Fatalf("edit_lines replace empty content should be allowed by argument validation, got %#v", result)
	}
}

func TestCodingSubAgentRejectsEmptyEditLinesInsertContent(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("edit_lines", `{"path":"main.go","operation":"insert","start_line":1,"content":"  "}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("empty insert content outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "missing required argument") || !strings.Contains(result.Text, `"content"`) {
		t.Fatalf("empty insert content should produce targeted required-argument error, got %q", result.Text)
	}
	if len(cb.getCommandsRun()) != 0 {
		t.Fatalf("edit_lines with empty insert content should not execute commands, commands=%#v", cb.getCommandsRun())
	}
}

func TestCodingSubAgentRejectsInvalidEditLinesOperation(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("edit_lines", `{"path":"main.go","operation":"move","start_line":1}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("invalid operation outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, `invalid argument value for "operation"`) || !strings.Contains(result.Text, "replace/insert/delete") {
		t.Fatalf("invalid operation should produce targeted allowed-values error, got %q", result.Text)
	}
}

func TestCodingSubAgentRejectsInvalidEditLinesRanges(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	cases := []struct {
		name     string
		args     string
		field    string
		expected string
	}{
		{name: "replace start zero", args: `{"path":"main.go","operation":"replace","start_line":0,"end_line":1,"content":"x"}`, field: "start_line", expected: "integer >= 1"},
		{name: "replace end zero", args: `{"path":"main.go","operation":"replace","start_line":1,"end_line":0,"content":"x"}`, field: "end_line", expected: "integer >= 1"},
		{name: "delete reversed range", args: `{"path":"main.go","operation":"delete","start_line":3,"end_line":2}`, field: "end_line", expected: `integer >= "start_line" (3)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeCommands := len(cb.getCommandsRun())
			result := cb.executeToolWithOutcome("edit_lines", tc.args)
			if result.Outcome != codingToolOutcomeFailed {
				t.Fatalf("invalid range outcome = %q, want failed; result=%s", result.Outcome, result.Text)
			}
			if !strings.Contains(result.Text, fmt.Sprintf(`invalid argument value for "%s"`, tc.field)) || !strings.Contains(result.Text, "expected "+tc.expected) {
				t.Fatalf("invalid %s should produce targeted range error, got %q", tc.field, result.Text)
			}
			if len(cb.getCommandsRun()) != beforeCommands {
				t.Fatalf("tool with invalid %s should not execute commands, commands=%#v", tc.field, cb.getCommandsRun())
			}
		})
	}

	if result, rejected := rejectInvalidCodingSubAgentToolArgumentTypes("edit_lines", map[string]interface{}{"path": "main.go", "operation": "insert", "start_line": 0, "content": "x"}); rejected {
		t.Fatalf("edit_lines insert at line 0 should be allowed by argument validation, got %#v", result)
	}
}
func TestCodingSubAgentRejectsInvalidWriteFileMode(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	result := cb.executeToolWithOutcome("write_file", `{"path":"out.txt","content":"x","mode":"merge"}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("invalid mode outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, `invalid argument value for "mode"`) || !strings.Contains(result.Text, "overwrite/append") {
		t.Fatalf("invalid write_file mode should produce targeted allowed-values error, got %q", result.Text)
	}
}

func TestCodingSubAgentRejectsMissingMCPRequiredArguments(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		matchedMCPTools: []codingSubAgentMCPToolMatch{{
			ServerID:     "wiki",
			ServerName:   "Wiki",
			ToolName:     "get_page_children",
			RequiredArgs: []string{"parent_id"},
		}},
	}

	result := cb.executeToolWithOutcome("call_mcp_tool", `{"server_id":"wiki","tool_name":"get_page_children","arguments":{"limit":25}}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("missing MCP required arg outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "missing required MCP argument") || !strings.Contains(result.Text, "parent_id") {
		t.Fatalf("missing MCP argument should produce targeted recovery error, got %q", result.Text)
	}
	if !result.SkipRejectedDynamicToolRecord {
		t.Fatalf("missing MCP argument rejection should skip rejected dynamic tool tracking")
	}
	if len(cb.getDynamicToolsRun()) != 0 {
		t.Fatalf("MCP call with missing required arguments should not execute or be tracked, got %#v", cb.getDynamicToolsRun())
	}
}

func TestCodingSubAgentMCPArgumentAliasesReachTargetValidation(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		matchedMCPTools: []codingSubAgentMCPToolMatch{{
			ServerID:     "wiki",
			ServerName:   "Wiki",
			ToolName:     "get_page_children",
			RequiredArgs: []string{"parent_id"},
		}},
	}

	result := cb.executeToolWithOutcome("call_mcp_tool", `{"server":"wiki","tool":"get_page_children","params":{"limit":25}}`)

	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("aliased MCP call missing target required arg outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "missing required MCP argument") || !strings.Contains(result.Text, "parent_id") {
		t.Fatalf("aliased MCP call should reach target-specific required-argument validation, got %q", result.Text)
	}
	if !result.SkipRejectedDynamicToolRecord {
		t.Fatalf("aliased MCP missing argument rejection should skip rejected dynamic tool tracking")
	}
	if len(cb.getDynamicToolsRun()) != 0 {
		t.Fatalf("aliased MCP call with missing required arguments should not execute or be tracked, got %#v", cb.getDynamicToolsRun())
	}
}

func TestCodingSubAgentRejectsInvalidNumericAndBooleanArgumentTypes(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}

	cases := []struct {
		name       string
		tool       string
		args       string
		field      string
		kind       string
		expected   string
		mustNotRun bool
	}{
		{name: "read lines type", tool: "read_file", args: `{"path":"main.go","lines":"20"}`, field: "lines", kind: "type", expected: "integer"},
		{name: "read lines fractional", tool: "read_file", args: `{"path":"main.go","lines":2.7}`, field: "lines", kind: "type", expected: "integer"},
		{name: "read lines zero", tool: "read_file", args: `{"path":"main.go","lines":0}`, field: "lines", kind: "value", expected: "integer >= 1"},
		{name: "read offset type", tool: "read_file", args: `{"path":"main.go","offset":"20"}`, field: "offset", kind: "type", expected: "integer"},
		{name: "read offset zero", tool: "read_file", args: `{"path":"main.go","offset":0}`, field: "offset", kind: "value", expected: "integer >= 1"},
		{name: "edit start fractional", tool: "edit_lines", args: `{"path":"main.go","operation":"replace","start_line":1.5,"end_line":2,"content":"x"}`, field: "start_line", kind: "type", expected: "integer"},
		{name: "edit start negative", tool: "edit_lines", args: `{"path":"main.go","operation":"replace","start_line":-1,"end_line":2,"content":"x"}`, field: "start_line", kind: "value", expected: "integer >= 0"},
		{name: "git staged", tool: "git_diff", args: `{"staged":"true"}`, field: "staged", kind: "type", expected: "boolean"},
		{name: "manage skill action type", tool: "manage_skill", args: `{"action":123,"name":"ui"}`, field: "action", kind: "type", expected: "string"},
		{name: "manage skill args object", tool: "manage_skill", args: `{"action":"run","name":"ui","args":"bad"}`, field: "args", kind: "type", expected: "object"},
		{name: "mcp server id type", tool: "call_mcp_tool", args: `{"server_id":7,"tool_name":"shot"}`, field: "server_id", kind: "type", expected: "string"},
		{name: "mcp arguments object", tool: "call_mcp_tool", args: `{"server_id":"browser","tool_name":"shot","arguments":[]}`, field: "arguments", kind: "type", expected: "object"},
		{name: "coding knowledge query type", tool: "coding_knowledge_search", args: `{"query":123}`, field: "query", kind: "type", expected: "string"},
		{name: "project knowledge query type", tool: "knowledge_search", args: `{"query":[]}`, field: "query", kind: "type", expected: "string"},
		{name: "bash timeout type", tool: "bash", args: `{"command":"Write-Output should-not-run","timeout":"30"}`, field: "timeout", kind: "type", expected: "integer", mustNotRun: true},
		{name: "bash timeout fractional", tool: "bash", args: `{"command":"Write-Output should-not-run","timeout":1.5}`, field: "timeout", kind: "type", expected: "integer", mustNotRun: true},
		{name: "bash timeout zero", tool: "bash", args: `{"command":"Write-Output should-not-run","timeout":0}`, field: "timeout", kind: "value", expected: "integer >= 1", mustNotRun: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeCommands := len(cb.getCommandsRun())
			result := cb.executeToolWithOutcome(tc.tool, tc.args)
			if result.Outcome != codingToolOutcomeFailed {
				t.Fatalf("%s outcome = %q, want failed; result=%s", tc.field, result.Outcome, result.Text)
			}
			if !strings.Contains(result.Text, fmt.Sprintf(`invalid argument %s for "%s"`, tc.kind, tc.field)) || !strings.Contains(result.Text, "expected "+tc.expected) {
				t.Fatalf("%s should produce targeted %s error, got %q", tc.field, tc.kind, result.Text)
			}
			if tc.mustNotRun && len(cb.getCommandsRun()) != beforeCommands {
				t.Fatalf("bash command with invalid %s should not execute, commands=%#v", tc.field, cb.getCommandsRun())
			}
		})
	}
}
func TestCodingSubAgentOnToolCallDoesNotEmitRawProgress(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.OnToolCall("read_file")

	if len(progress) != 0 {
		t.Fatalf("OnToolCall should not emit raw progress rows, got %#v", progress)
	}
}

func TestCodingToolExecutionUsesStructuredOutcome(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{}}
	blocked := cb.executeToolWithOutcome("read_file", `{"path":"main.go"}`)
	if blocked.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("blocked outcome = %q", blocked.Outcome)
	}
	failed := cb.ExecuteToolStructured("read_file", `{`)
	if failed.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("structured loop outcome = %q", failed.Outcome)
	}
	if got := compactCodingToolResultSummary("first line\nsecond line"); got != "first line" {
		t.Fatalf("tool result summary = %q", got)
	}
	if got := compactCodingToolResultSummary("ordinary prelude\n[stderr] compiler: missing symbol\ncommand exited with code 2"); got != "compiler: missing symbol" {
		t.Fatalf("stderr diagnostic summary = %q", got)
	}
	if got := compactCodingToolResultSummary("ordinary prelude\ncoverage: 12.3%\nFAIL: TestCheckout expected 200 got 500\ncommand exited with code 1"); got != "FAIL: TestCheckout expected 200 got 500" {
		t.Fatalf("stdout failure diagnostic summary = %q", got)
	}
	if got := compactCodingToolResultSummary("ordinary prelude\n✖ 10 errors and 0 warnings\ncommand exited with code 1"); got != "✖ 10 errors and 0 warnings" {
		t.Fatalf("double-digit error count should remain actionable diagnostic, got %q", got)
	}
	if got := compactCodingToolResultSummary("ordinary prelude\n0 errors and 0 warnings\nsrc/main.ts:12: TypeError: missing handler\ncommand exited with code 1"); got != "src/main.ts:12: TypeError: missing handler" {
		t.Fatalf("zero-error noise should not hide later diagnostic, got %q", got)
	}
}

func TestExecuteCodingBashReturnsStructuredOutcome(t *testing.T) {
	successCmd := "printf ok"
	failCmd := "exit 7"
	timeoutCmd := "sleep 2"
	if runtime.GOOS == "windows" {
		successCmd = "Write-Output ok"
		failCmd = "exit 7"
		timeoutCmd = "Start-Sleep -Seconds 2"
	}

	success := executeCodingBash(map[string]interface{}{"command": "  " + successCmd + "  "}, nil)
	if success.Kind != codingCommandResultOK || success.ExitCode != 0 {
		t.Fatalf("success result = %#v", success)
	}
	missing := executeCodingBash(map[string]interface{}{"command": "   "}, nil)
	if missing.Kind != codingCommandResultStartError || !strings.Contains(missing.Text, "missing command") {
		t.Fatalf("blank command should fail before shell execution, got %#v", missing)
	}
	failed := executeCodingBash(map[string]interface{}{"command": failCmd}, nil)
	if failed.Kind != codingCommandResultExitError || failed.ExitCode == 0 {
		t.Fatalf("failed result = %#v", failed)
	}
	if !strings.Contains(failed.Text, "no stdout/stderr") ||
		(!strings.Contains(failed.Text, "without output filters") && !strings.Contains(failed.Text, "without pipe filters")) {
		t.Fatalf("empty failure should include diagnostic hint, got %q", failed.Text)
	}
	timedOut := executeCodingBash(map[string]interface{}{
		"command": timeoutCmd,
		"timeout": float64(1),
	}, nil)
	if timedOut.Kind != codingCommandResultTimeout {
		t.Fatalf("timeout result = %#v", timedOut)
	}
	if timedOut.toolResult().Outcome != codingToolOutcomeTimeout {
		t.Fatalf("timeout tool outcome = %q", timedOut.toolResult().Outcome)
	}
}

func TestConvertUnquotedAndAndForPowerShellPreservesQuotedText(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{name: "plain separator", command: `Write-Output before && Write-Output after`, want: `Write-Output before ; Write-Output after`},
		{name: "compact separator", command: `go test ./...&&go vet ./...`, want: `go test ./...;go vet ./...`},
		{name: "double quoted data", command: `Write-Output "a && b"`, want: `Write-Output "a && b"`},
		{name: "single quoted data", command: `Write-Output 'a && b' && Write-Output done`, want: `Write-Output 'a && b' ; Write-Output done`},
		{name: "wrapped cmd command", command: `cmd /c "echo a && echo b"`, want: `cmd /c "echo a && echo b"`},
		{name: "powershell escaped quote", command: "Write-Output \"a `\" && still data\" && Write-Output done", want: "Write-Output \"a `\" && still data\" ; Write-Output done"},
		{name: "unicode text", command: `Write-Output "路径 && 数据" && Write-Output 完成`, want: `Write-Output "路径 && 数据" ; Write-Output 完成`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := convertUnquotedAndAndForPowerShell(tc.command); got != tc.want {
				t.Fatalf("convertUnquotedAndAndForPowerShell(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

func TestHasUnquotedWindowsCmdSyntax(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{command: `mkdir D:\\testdriver\\tests 2>nul || echo already exists`, want: true},
		{command: `Write-Output "a || b"`, want: false},
		{command: `Write-Output "a >nul"`, want: false},
		{command: `Write-Output ok`, want: false},
	}
	for _, tc := range cases {
		if got := hasUnquotedWindowsCmdSyntax(tc.command); got != tc.want {
			t.Fatalf("hasUnquotedWindowsCmdSyntax(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestExecuteCodingBashWindowsUsesCmdForNulAndOrSyntax(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe syntax is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `echo probe 2>nul || echo fallback`,
	}, nil)
	if result.Kind != codingCommandResultOK {
		t.Fatalf("cmd syntax probe should succeed, got %#v", result)
	}
	if !strings.Contains(result.Text, "probe") {
		t.Fatalf("cmd syntax probe output = %q, want probe", result.Text)
	}
}

func TestExecuteCodingBashWindowsPreservesQuotedAndAnd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell quoted && behavior is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `Write-Output "a && b"`,
	}, nil)
	if result.Kind != codingCommandResultOK {
		t.Fatalf("quoted && command should succeed, got %#v", result)
	}
	if !strings.Contains(result.Text, "a && b") || strings.Contains(result.Text, "a ; b") {
		t.Fatalf("quoted && should be preserved in command output, got %q", result.Text)
	}
}
func TestExecuteCodingBashWithContextReturnsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := executeCodingBashWithContext(ctx, map[string]interface{}{"command": "definitely-should-not-run"}, nil)
	if result.Kind != codingCommandResultCancelled {
		t.Fatalf("cancelled result kind = %q, want %q; result=%#v", result.Kind, codingCommandResultCancelled, result)
	}
	if result.toolResult().Outcome != codingToolOutcomeFailed {
		t.Fatalf("cancelled tool outcome = %q, want %q", result.toolResult().Outcome, codingToolOutcomeFailed)
	}
	if !strings.Contains(result.Text, "command cancelled before start") {
		t.Fatalf("cancelled result should explain cancellation, got %q", result.Text)
	}
}

func TestCodingCommandExecutionResultSucceededMatchesToolOutcome(t *testing.T) {
	cases := []codingCommandExecutionResult{
		{Text: "ok", Kind: codingCommandResultOK, ExitCode: 0},
		{Text: "matched output\ncommand exited with code 1", Kind: codingCommandResultExitError, ExitCode: 1},
		{Text: "[stderr] compiler failed\ncommand exited with code 1", Kind: codingCommandResultExitError, ExitCode: 1},
		{Text: "command timed out after 1 seconds", Kind: codingCommandResultTimeout, ExitCode: -1},
		{Text: "command cancelled", Kind: codingCommandResultCancelled, ExitCode: -1},
	}
	for _, tc := range cases {
		want := tc.toolResult().Outcome == codingToolOutcomeSuccess
		if got := tc.succeeded(); got != want {
			t.Fatalf("succeeded() = %v, want %v for %#v", got, want, tc)
		}
	}
}

func TestCodingCommandExecutionResultClassifiesExitOneByCommand(t *testing.T) {
	result := codingCommandExecutionResult{
		Text:     "test failed with assertion output\ncommand exited with code 1",
		Kind:     codingCommandResultExitError,
		ExitCode: 1,
	}
	if result.toolOutcome() != codingToolOutcomeFailed {
		t.Fatalf("plain exit-1 outcome should fail without command context, got %q", result.toolOutcome())
	}
	if result.succeededForCommand("go test ./...") {
		t.Fatal("verification command with non-zero exit must not be tracked as succeeded")
	}
	if got := result.toolResultForCommand("go test ./...").Outcome; got != codingToolOutcomeFailed {
		t.Fatalf("verification command tool outcome = %q, want failed", got)
	}
	if result.succeededForCommand("python script.py") {
		t.Fatal("generic non-verification exit-1 command should fail even with stdout")
	}
	if result.succeededForCommand("git -C . status") {
		t.Fatal("non-grep git exit-1 command should fail even with stdout")
	}

	noMatch := codingCommandExecutionResult{
		Text:     "command exited with code 1",
		Kind:     codingCommandResultExitError,
		ExitCode: 1,
	}
	for _, command := range []string{"rg missing-pattern .", "grep missing file.txt", "git grep missing", "git -C . grep missing", "findstr missing file.txt", "Select-String missing file.txt"} {
		if !noMatch.succeededForCommand(command) {
			t.Fatalf("search no-match exit-1 should be informational for %q", command)
		}
	}
	for _, command := range []string{"rg missing . ; go test ./...", "rg missing . && go test ./...", "rg missing . | cat"} {
		if noMatch.succeededForCommand(command) {
			t.Fatalf("mixed command exit-1 should not be treated as informational for %q", command)
		}
	}

	searchWithStderr := codingCommandExecutionResult{
		Text:     "[stderr] rg: regex parse error\ncommand exited with code 1",
		Kind:     codingCommandResultExitError,
		ExitCode: 1,
	}
	if searchWithStderr.succeededForCommand("rg '[' .") {
		t.Fatal("search exit-1 with stderr should fail")
	}

	searchWithStdoutThenStderr := codingCommandExecutionResult{
		Text:     "partial output\n[stderr] rg: IO error\ncommand exited with code 1",
		Kind:     codingCommandResultExitError,
		ExitCode: 1,
	}
	if searchWithStdoutThenStderr.succeededForCommand("rg missing .") {
		t.Fatal("search exit-1 with stderr after stdout should fail")
	}
}

func TestFormatCodingCommandOutputTruncatesAtUTF8Boundary(t *testing.T) {
	stdoutText := strings.Repeat("界", 3000)
	stderrText := strings.Repeat("错", 2000)

	output := formatCodingCommandOutput(stdoutText, stderrText)
	if !utf8.ValidString(output) {
		t.Fatalf("formatted command output should remain valid UTF-8 after truncation")
	}
	if !strings.Contains(output, "... (output truncated)") {
		t.Fatalf("stdout truncation marker missing from %q", lastNRunesForTest(output, 300))
	}
	if !strings.Contains(output, "... (stderr truncated)") {
		t.Fatalf("stderr truncation marker missing from %q", lastNRunesForTest(output, 300))
	}
	if !strings.Contains(output, "[stderr] ") {
		t.Fatalf("stderr prefix missing from truncated output")
	}
}

func TestFirstDiagnosticCodingToolResultLineRecognizesCommonCompilerFormats(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "typescript",
			output: strings.Join([]string{
				"> tsc --noEmit",
				"src/app.ts(12,7): error TS2304: Cannot find name 'missingValue'.",
				"command exited with code 2",
			}, "\n"),
			want: "src/app.ts(12,7): error TS2304: Cannot find name 'missingValue'.",
		},
		{
			name: "stdout diagnostic beats stderr warning",
			output: strings.Join([]string{
				"[stderr] npm WARN deprecated old-package@1.0.0",
				"src/app.ts(12,7): error TS2304: Cannot find name 'missingValue'.",
				"command exited with code 2",
			}, "\n"),
			want: "src/app.ts(12,7): error TS2304: Cannot find name 'missingValue'.",
		},
		{
			name: "rust",
			output: strings.Join([]string{
				"   Compiling app v0.1.0",
				"error[E0425]: cannot find value `missing_value` in this scope",
				"command exited with code 101",
			}, "\n"),
			want: "error[E0425]: cannot find value `missing_value` in this scope",
		},
		{
			name: "python traceback",
			output: strings.Join([]string{
				"running tests",
				"Traceback (most recent call last):",
				"  File \"test_app.py\", line 10, in <module>",
				"command exited with code 1",
			}, "\n"),
			want: "Traceback (most recent call last):",
		},
		{
			name: "python exception class",
			output: strings.Join([]string{
				"running tests",
				"ModuleNotFoundError: No module named 'generated_client'",
				"command exited with code 1",
			}, "\n"),
			want: "ModuleNotFoundError: No module named 'generated_client'",
		},
		{
			name: "javascript exception class",
			output: strings.Join([]string{
				"> node --test",
				"ReferenceError: missingValue is not defined",
				"command exited with code 1",
			}, "\n"),
			want: "ReferenceError: missingValue is not defined",
		},
		{
			name: "go assertion detail before package fail",
			output: strings.Join([]string{
				"--- FAIL: TestRender (0.00s)",
				"    render_test.go:42: got \"old\", want \"new\"",
				"FAIL",
				"FAIL\tgithub.com/example/app/gui\t0.123s",
				"command exited with code 1",
			}, "\n"),
			want: "render_test.go:42: got \"old\", want \"new\"",
		},
		{
			name: "pytest assert detail",
			output: strings.Join([]string{
				"tests/test_render.py::test_render FAILED",
				"E       assert 'old' == 'new'",
				"command exited with code 1",
			}, "\n"),
			want: "E       assert 'old' == 'new'",
		},
		{
			name: "jest file failure",
			output: strings.Join([]string{
				"Test Suites: 1 failed, 1 total",
				"Tests:       1 failed, 1 total",
				"FAIL  src/render.test.ts",
				"command exited with code 1",
			}, "\n"),
			want: "FAIL  src/render.test.ts",
		},
		{
			name: "process killed",
			output: strings.Join([]string{
				"running integration tests",
				"signal: killed",
				"command exited with code 137",
			}, "\n"),
			want: "signal: killed",
		},
		{
			name: "permission denied",
			output: strings.Join([]string{
				"running tests",
				"open ./tmp/cache: permission denied",
				"command exited with code 1",
			}, "\n"),
			want: "open ./tmp/cache: permission denied",
		},
		{
			name: "skip non failure error noise",
			output: strings.Join([]string{
				"summary: 0 errors reported before crash",
				"build failed: missing generated file",
				"command exited with code 1",
			}, "\n"),
			want: "build failed: missing generated file",
		},
		{
			name: "skip assertion count noise",
			output: strings.Join([]string{
				"summary: 0 assertions recorded",
				"running tests",
				"FAIL  src/render.test.ts",
			}, "\n"),
			want: "FAIL  src/render.test.ts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstDiagnosticCodingToolResultLine(tc.output); got != tc.want {
				t.Fatalf("diagnostic line = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodingSubAgentBashVerificationExitErrorFails(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: project},
		task:     &TaskItem{Index: 1, Title: "verification failure"},
	}

	args, err := json.Marshal(map[string]interface{}{
		"command":     "go test ./...",
		"working_dir": project,
		"timeout":     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := cb.executeToolWithOutcome("bash", string(args))
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("verification failure outcome = %q, want failed; text=%s", result.Outcome, result.Text)
	}
	commands := cb.getCommandsRun()
	if len(commands) != 1 {
		t.Fatalf("commands tracked = %d, want 1", len(commands))
	}
	if commands[0].Succeeded {
		t.Fatalf("verification failure should be tracked as failed: %#v", commands[0])
	}
}
func TestExecuteCodingBashWindowsNativeStderrWithZeroExitSucceeds(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell native stderr behavior is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `cmd /c "echo native stderr 1>&2 & exit /b 0" 2>&1`,
	}, nil)

	if result.Kind != codingCommandResultOK || result.ExitCode != 0 {
		t.Fatalf("native stderr with zero exit should succeed, got %#v", result)
	}
	if !strings.Contains(result.Text, "native stderr") {
		t.Fatalf("expected stderr text to be preserved, got %q", result.Text)
	}
}

func TestExecuteCodingBashWindowsPipedNativeNonZeroFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell native pipeline behavior is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `cmd /c "echo native failed 1>&2 & exit /b 7" 2>&1 | Select-Object -First 5`,
	}, nil)

	if result.Kind != codingCommandResultExitError || result.ExitCode != 7 {
		t.Fatalf("piped native non-zero should fail with native exit code, got %#v", result)
	}
}

func TestExecuteCodingBashWindowsCmdletErrorFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell cmdlet error behavior is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `Get-Item -LiteralPath "__codex_missing_file_for_test__"`,
	}, nil)

	if result.Kind != codingCommandResultExitError || result.ExitCode == 0 {
		t.Fatalf("cmdlet error should fail, got %#v", result)
	}
}

func TestExecuteCodingBashWindowsPipedCmdletErrorFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell cmdlet pipeline behavior is Windows-specific")
	}

	result := executeCodingBash(map[string]interface{}{
		"command": `Get-Item -LiteralPath "__codex_missing_file_for_test__" 2>&1 | Select-Object -First 5`,
	}, nil)

	if result.Kind != codingCommandResultExitError || result.ExitCode == 0 {
		t.Fatalf("piped cmdlet error should fail, got %#v", result)
	}
}

func TestCodingSubAgentEmitsVerificationSummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 8, Title: "Verify parser"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitVerificationSummaryEvent("failed", "go test ./gui failed\nfull output", 2)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"verification_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T8"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"summary":"go test ./gui failed"`) ||
		!strings.Contains(joined, `"count":2`) {
		t.Fatalf("expected structured verification progress, got %#v", progress)
	}
}

func TestSubAgentEventSummaryLinePrefersDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		want    string
	}{
		{
			name:    "markdown heading",
			summary: "## 质量审计\n- FAIL command: go test ./gui -> panic: nil parser",
			want:    "- FAIL command: go test ./gui -> panic: nil parser",
		},
		{
			name:    "english header",
			summary: "Verification summary:\npytest tests failed: AssertionError",
			want:    "pytest tests failed: AssertionError",
		},
		{
			name:    "chinese header",
			summary: "命令摘要：\nFAIL bash: npm test -> TypeError: missing config",
			want:    "FAIL bash: npm test -> TypeError: missing config",
		},
		{
			name:    "single line",
			summary: "verification not run",
			want:    "verification not run",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subAgentEventSummaryLine(tt.summary); got != tt.want {
				t.Fatalf("subAgentEventSummaryLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodingSubAgentEmitsExplorationSummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 9, Title: "Explore parser"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitExplorationSummaryEvent("explored", "searched before editing\nfull detail", 2)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"exploration_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T9"`) ||
		!strings.Contains(joined, `"outcome":"explored"`) ||
		!strings.Contains(joined, `"summary":"searched before editing"`) ||
		!strings.Contains(joined, `"count":2`) {
		t.Fatalf("expected structured exploration progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsGuardrailSummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 10, Title: "Guard edits"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitGuardrailSummaryEvent([]CodingSubAgentGuardrailViolation{
		{Tool: "bash", Category: "git", Summary: "refused dangerous command\nfull detail"},
		{Tool: "read_file", Category: "scope", Summary: "outside project"},
	})

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"guardrail_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T10"`) ||
		!strings.Contains(joined, `"outcome":"blocked"`) ||
		!strings.Contains(joined, `"summary":"blocked | bash | category:git | refused dangerous command"`) ||
		!strings.Contains(joined, `"count":2`) {
		t.Fatalf("expected structured guardrail progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsGuardrailSummaryEventCountsLateBlocks(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 10, Title: "Guard many edits"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	violations := make([]CodingSubAgentGuardrailViolation, 0, codingSubAgentResultAuditMax+1)
	for i := 0; i < codingSubAgentResultAuditMax; i++ {
		violations = append(violations, CodingSubAgentGuardrailViolation{
			Tool:     "read_file",
			Category: codingSubAgentGuardrailCategoryScope,
			Path:     fmt.Sprintf("../outside-%02d.go", i),
			Summary:  "outside project",
		})
	}
	violations = append(violations, CodingSubAgentGuardrailViolation{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryGit,
		Command:  "git reset --hard",
		Summary:  "late destructive git command",
	})
	cb.emitGuardrailSummaryEvent(violations)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"outcome":"blocked"`) ||
		!strings.Contains(joined, fmt.Sprintf(`"count":%d`, codingSubAgentResultAuditMax+1)) ||
		!strings.Contains(joined, `"summary":"blocked | bash | category:git | late destructive git command"`) {
		t.Fatalf("late high-risk guardrail block should remain visible in summary event, got %#v", progress)
	}
}
func TestCodingSubAgentEmitsCommandSummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 11, Title: "Run commands"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitCommandSummaryEvent([]CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
		{Command: "npm test", Succeeded: false},
	})

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"command_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T11"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"summary":"2 bash commands run, 1 failed: npm test"`) ||
		!strings.Contains(joined, `"count":2`) {
		t.Fatalf("expected structured command progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsCommandSummaryEventCountsLateFailures(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 11, Title: "Run many commands"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	commands := make([]CodingSubAgentCommandResult, 0, codingSubAgentResultAuditMax+1)
	for i := 0; i < codingSubAgentResultAuditMax; i++ {
		commands = append(commands, CodingSubAgentCommandResult{Command: fmt.Sprintf("echo ok-%02d", i), Succeeded: true})
	}
	commands = append(commands, CodingSubAgentCommandResult{Command: "npm test", Succeeded: false})
	cb.emitCommandSummaryEvent(commands)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, fmt.Sprintf(`"count":%d`, codingSubAgentResultAuditMax+1)) ||
		!strings.Contains(joined, "npm test") {
		t.Fatalf("late command failure should remain visible in summary event, got %#v", progress)
	}
}
func TestCodingSubAgentEmitsFileActivitySummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 12, Title: "Touch files"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitFileActivitySummaryEvent(
		[]string{"a.go", "b.go", "a.go"},
		[]string{"b.go"},
		[]string{"c.go"},
	)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"file_activity_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T12"`) ||
		!strings.Contains(joined, `"outcome":"changed"`) ||
		!strings.Contains(joined, `"detail":"read 2 / modified 1 / created 1"`) ||
		!strings.Contains(joined, `"summary":"read 2 / modified 1 / created 1; changed: b.go, c.go"`) ||
		!strings.Contains(joined, `"count":4`) ||
		!strings.Contains(joined, `"files":["`) {
		t.Fatalf("expected structured file activity progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsQualitySummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 13, Title: "Quality gate"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitQualitySummaryEvent("missing", "missing", false, []string{"main.go"}, nil, nil, 0, nil, nil)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"quality_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T13"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"summary":"no exploration before existing-file edits; verification not run; diff not checked"`) ||
		!strings.Contains(joined, `"count":3`) {
		t.Fatalf("expected structured quality progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsPrecomputedQualitySummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 14, Title: "Quality warning"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitQualitySummaryEventWithAudit(codingSubAgentQualityWarning, "1 dynamic tool failed: call_mcp_tool browser/screenshot -> browser closed", 1)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"quality_summary"`) ||
		!strings.Contains(joined, `"task_id":"T14"`) ||
		!strings.Contains(joined, `"outcome":"warning"`) ||
		!strings.Contains(joined, `"summary":"1 dynamic tool failed: call_mcp_tool browser/screenshot -\u003e browser closed"`) ||
		!strings.Contains(joined, `"count":1`) {
		t.Fatalf("expected precomputed structured quality progress, got %#v", progress)
	}
}

func TestCodingSubAgentSummaryEventsSkipGenericHeaders(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 15, Title: "Quality diagnostics"},
		subagent: &CodingSubAgent{
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitQualitySummaryEventWithAudit(codingSubAgentQualityFailed, "## 质量审计\nverification not run after last edit", 1)
	cb.emitVerificationSummaryEvent(codingSubAgentQualityFailed, "Verification summary:\ngo test ./gui failed: panic: nil parser", 1)
	cb.emitGuardrailSummaryEvent([]CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryGit,
		Summary:  "Guardrail summary:\nrefused destructive git command",
	}})

	joined := strings.Join(progress, "\n")
	for _, want := range []string{
		`"summary":"verification not run after last edit"`,
		`"summary":"go test ./gui failed: panic: nil parser"`,
		`"summary":"blocked | bash | category:git | refused destructive git command"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected diagnostic summary %s, got %#v", want, progress)
		}
	}
	if strings.Contains(joined, `"summary":"## 质量审计"`) || strings.Contains(joined, `"summary":"Verification summary:"`) {
		t.Fatalf("generic summary header should not be emitted as the visible event summary, got %#v", progress)
	}
}

func TestCodingSubAgentTrackFileEmitsDiffUpdatedEvent(t *testing.T) {
	var progress []string
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 4, Title: "Update parser"},
		subagent: &CodingSubAgent{
			projectPath: project,
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.trackFile(filepath.Join(project, "parser.go"))

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, "Coding Agent Event:") ||
		!strings.Contains(joined, `"event":"diff_updated"`) ||
		!strings.Contains(joined, `"phase":"running"`) ||
		!strings.Contains(joined, `"task_id":"T4"`) ||
		!strings.Contains(joined, `"detail":"parser.go (1)"`) {
		t.Fatalf("expected structured diff_updated progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsDiffSummaryEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 5, Title: "Summarize diff"},
		subagent: &CodingSubAgent{
			projectPath: t.TempDir(),
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitDiffSummaryEvent([]string{"b.go", "a.go"}, []string{"new.go"}, "diff --git a/a.go b/a.go\n+changed")

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, "Coding Agent Event:") ||
		!strings.Contains(joined, `"event":"diff_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T5"`) ||
		!strings.Contains(joined, `"count":3`) ||
		!strings.Contains(joined, `"files":["a.go","b.go","new.go"]`) ||
		!strings.Contains(joined, `"detail":"3 files | 1 created | diff --git a/a.go b/a.go"`) {
		t.Fatalf("expected structured diff_summary progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsDiffCheckEvent(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 6, Title: "Check diff"},
		subagent: &CodingSubAgent{
			projectPath: t.TempDir(),
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitDiffCheckEvent(true, "diff --git a/a.go b/a.go\n+changed", 1)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"diff_check"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T6"`) ||
		!strings.Contains(joined, `"outcome":"checked"`) ||
		!strings.Contains(joined, `"summary":"diff --git a/a.go b/a.go"`) ||
		!strings.Contains(joined, `"count":1`) {
		t.Fatalf("expected structured diff_check progress, got %#v", progress)
	}
}

func TestCodingSubAgentEmitsFailedDiffCheckEventForModifiedFiles(t *testing.T) {
	var progress []string
	cb := &codingSubAgentCallbacks{
		task: &TaskItem{Index: 6, Title: "Check diff failure"},
		subagent: &CodingSubAgent{
			projectPath: t.TempDir(),
			onProgress: func(text string) {
				progress = append(progress, text)
			},
		},
	}

	cb.emitDiffCheckEvent(false, "git diff failed: not a repository", 1)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"diff_check"`) ||
		!strings.Contains(joined, `"outcome":"failed"`) ||
		!strings.Contains(joined, `"summary":"git diff failed: not a repository"`) ||
		!strings.Contains(joined, `"count":1`) {
		t.Fatalf("expected failed diff_check progress for modified files, got %#v", progress)
	}
}
func TestCodingSubAgentRejectedBashTracksCommandAndGuardrail(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: project},
	}

	result := cb.ExecuteTool("bash", `{"command":"Set-Content src\\a.go x"}`)
	if !strings.Contains(result, "拒绝执行") && !strings.Contains(result, "鎷掔粷鎵ц") {
		t.Fatalf("expected rejected bash result, got %q", result)
	}
	commands := cb.getCommandsRun()
	if len(commands) != 1 || commands[0].Command != "Set-Content src\\a.go x" || commands[0].Succeeded {
		t.Fatalf("expected failed command audit record, got %#v", commands)
	}
	violations := cb.getGuardrailViolations()
	if len(violations) != 1 || violations[0].Tool != "bash" || violations[0].Category != "shell_write" {
		t.Fatalf("expected shell_write guardrail audit record, got %#v", violations)
	}
}

func TestCodingSubAgentRequiresReadBeforeModify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cb := &codingSubAgentCallbacks{}
	if msg := cb.requireReadBeforeModify(path, "edit_file"); msg == "" {
		t.Fatal("expected edit_file to require read_file first")
	}

	cb.trackReadFile(path)
	if msg := cb.requireReadBeforeModify(path, "edit_file"); msg != "" {
		t.Fatalf("expected edit_file to be allowed after read_file, got %q", msg)
	}
}

func TestCodingSubAgentRejectsModifyAfterExternalFileChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cb := &codingSubAgentCallbacks{}
	cb.trackReadFile(path)

	if err := os.WriteFile(path, []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if msg := cb.requireReadBeforeModify(path, "edit_file"); !strings.Contains(msg, "已变化") ||
		!strings.Contains(msg, "read_file 时 size=") ||
		!strings.Contains(msg, "当前 size=") ||
		!strings.Contains(msg, fmt.Sprintf("read_file(path=%q)", path)) ||
		!strings.Contains(msg, "重新应用最小编辑") {
		t.Fatalf("expected actionable external change warning, got %q", msg)
	}

	cb.refreshFileSnapshot(path)
	if msg := cb.requireReadBeforeModify(path, "edit_file"); msg != "" {
		t.Fatalf("expected modify to be allowed after snapshot refresh, got %q", msg)
	}
}

func TestCanonicalCodingPathKeyNormalizesWindowsCase(t *testing.T) {
	if got, want := canonicalCodingPathKey(`C:\Repo\App\MAIN.go`), "c:/repo/app/main.go"; got != want {
		t.Fatalf("Windows drive path key = %q, want %q", got, want)
	}
	if got, want := canonicalCodingPathKey(`\\Server\Share\Repo\MAIN.go`), "//server/share/repo/main.go"; got != want {
		t.Fatalf("Windows UNC path key = %q, want %q", got, want)
	}
	if got, want := canonicalCodingPathKey(`src/Main.go`), "src/Main.go"; got != want {
		t.Fatalf("relative path key should preserve case, got %q want %q", got, want)
	}
}

func TestCodingSubAgentSnapshotCanonicalizesSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(realDir, "target.go")
	if err := os.WriteFile(realPath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	linkPath := filepath.Join(linkDir, "target.go")

	cb := &codingSubAgentCallbacks{}
	cb.trackReadFile(linkPath)
	if msg := cb.requireReadBeforeModify(realPath, "edit_file"); msg != "" {
		t.Fatalf("expected real path write to match symlink read snapshot, got %q", msg)
	}

	if err := os.WriteFile(realPath, []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if msg := cb.requireReadBeforeModify(linkPath, "edit_file"); msg == "" {
		t.Fatal("expected symlink path to detect external change")
	}
}

func TestCodingSubAgentWriteFileRequiresReadOnlyForExistingFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cb := &codingSubAgentCallbacks{}
	if msg := cb.requireReadBeforeWriteExisting(existing, nil); msg == "" {
		t.Fatal("expected write_file on existing file to require read_file first")
	}

	cb.trackReadFile(existing)
	if msg := cb.requireReadBeforeWriteExisting(existing, nil); msg != "" {
		t.Fatalf("expected write_file on existing file to be allowed after read_file, got %q", msg)
	}

	newFile := filepath.Join(dir, "new.txt")
	if msg := cb.requireReadBeforeWriteExisting(newFile, nil); msg != "" {
		t.Fatalf("expected write_file on new file to be allowed, got %q", msg)
	}
}

func TestCodingSubAgentRejectsWriteFileAfterExternalFileChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cb := &codingSubAgentCallbacks{}
	cb.trackReadFile(path)
	if err := os.WriteFile(path, []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msg := cb.requireReadBeforeWriteExisting(path, map[string]interface{}{"content": "package overwrite\n"})
	if !strings.Contains(msg, "已变化") ||
		!strings.Contains(msg, "read_file 时 size=") ||
		!strings.Contains(msg, "当前 size=") ||
		!strings.Contains(msg, fmt.Sprintf("read_file(path=%q)", path)) {
		t.Fatalf("expected actionable stale write_file snapshot warning, got %q", msg)
	}
}
func TestCodingSubAgentWriteFileAllowsTestReportAppend(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "TEST_REPORT.md")
	if err := os.WriteFile(report, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{}
	if msg := cb.requireReadBeforeWriteExisting(report, map[string]interface{}{"mode": "append"}); msg != "" {
		t.Fatalf("expected TEST_REPORT.md append to be allowed without read_file, got %q", msg)
	}
	if msg := cb.requireReadBeforeWriteExisting(report, map[string]interface{}{"mode": "overwrite"}); msg == "" {
		t.Fatal("expected TEST_REPORT.md overwrite to still require read_file")
	}
}

func TestCodingFileToolExecutionReturnsStructuredOutcome(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	writeResult := executeCodingWriteFile(map[string]interface{}{
		"path":    file,
		"content": "package main\nfunc main() {}\n",
	})
	if writeResult.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("write outcome = %q, text=%q", writeResult.Outcome, writeResult.Text)
	}
	readResult := executeCodingReadFile(map[string]interface{}{"path": file})
	if readResult.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("read outcome = %q, text=%q", readResult.Outcome, readResult.Text)
	}
	editResult := executeCodingEditFile(map[string]interface{}{
		"path":       file,
		"old_string": "func main() {}",
		"new_string": "func main() { println(\"ok\") }",
	})
	if editResult.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("edit outcome = %q, text=%q", editResult.Outcome, editResult.Text)
	}
	missingPath := executeCodingWriteFile(map[string]interface{}{"content": "x"})
	if missingPath.Outcome != codingToolOutcomeFailed {
		t.Fatalf("missing path outcome = %q", missingPath.Outcome)
	}
	missingReplacement := executeCodingEditFile(map[string]interface{}{
		"path":       file,
		"old_string": "not present",
		"new_string": "replacement",
	})
	if missingReplacement.Outcome != codingToolOutcomeFailed {
		t.Fatalf("missing replacement outcome = %q", missingReplacement.Outcome)
	}
}

func TestExecuteCodingReadFileHonorsIntegerArgumentTypes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\nfour"), 0644); err != nil {
		t.Fatal(err)
	}

	fromStart := executeCodingReadFile(map[string]interface{}{
		"path":       file,
		"start_line": 1,
		"lines":      2,
	})
	if fromStart.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("start-line read outcome = %q, text=%q", fromStart.Outcome, fromStart.Text)
	}
	if !strings.Contains(fromStart.Text, "   1 | one") || !strings.Contains(fromStart.Text, "Next start_line=3") {
		t.Fatalf("expected explicit start_line=1 to return numbered chunk, got %q", fromStart.Text)
	}

	tail := executeCodingReadFile(map[string]interface{}{
		"path":   file,
		"offset": int64(2),
	})
	if tail.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("tail read outcome = %q, text=%q", tail.Outcome, tail.Text)
	}
	if !strings.Contains(tail.Text, "showing last 2 of 4 lines") || !strings.Contains(tail.Text, "three\nfour") {
		t.Fatalf("expected int64 offset to return tail content, got %q", tail.Text)
	}
}

func TestExecuteCodingReadFileCapsExplicitLineLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large.txt")
	var b strings.Builder
	for i := 1; i <= codingSubAgentReadFileExplicitMaxLines+105; i++ {
		fmt.Fprintf(&b, "line-%04d\n", i)
	}
	if err := os.WriteFile(file, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	result := executeCodingReadFile(map[string]interface{}{
		"path":       file,
		"start_line": 1,
		"lines":      codingSubAgentReadFileExplicitMaxLines + 10000,
	})
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("large explicit read outcome = %q, text=%q", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "line-2000") || strings.Contains(result.Text, "line-2001") {
		t.Fatalf("explicit read should be capped at %d lines, got tail %q", codingSubAgentReadFileExplicitMaxLines, lastNRunesForTest(result.Text, 500))
	}
	if !strings.Contains(result.Text, "Next start_line=2001") {
		t.Fatalf("capped explicit read should guide continuation, got %q", lastNRunesForTest(result.Text, 500))
	}
}

func lastNRunesForTest(text string, n int) string {
	runes := []rune(text)
	if n <= 0 || len(runes) <= n {
		return text
	}
	return string(runes[len(runes)-n:])
}

func TestExecuteCodingEditLinesHonorsIntegerArgumentTypes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := executeCodingEditLines(map[string]interface{}{
		"path":       file,
		"operation":  "replace",
		"start_line": json.Number("2"),
		"end_line":   int64(2),
		"content":    "TWO",
	})
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("edit_lines outcome = %q, text=%q", result.Outcome, result.Text)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "one\nTWO\nthree\n"; got != want {
		t.Fatalf("edited content = %q, want %q", got, want)
	}
}
func TestCodingSubAgentRejectsWritesOutsideProjectPath(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	prefixSibling := filepath.Join(root, "project-other", "file.txt")

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	if msg := cb.requireProjectWriteScope(filepath.Join(project, "main.go")); msg != "" {
		t.Fatalf("expected project file to be allowed, got %q", msg)
	}
	if msg := cb.requireProjectWriteScope(outside); !strings.Contains(msg, "项目目录外") {
		t.Fatalf("expected outside file to be rejected, got %q", msg)
	}
	if msg := cb.requireProjectWriteScope(prefixSibling); !strings.Contains(msg, "项目目录外") {
		t.Fatalf("expected prefix sibling to be rejected, got %q", msg)
	}
}

func TestCodingSubAgentRejectsWriteThroughSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "linked-outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	if msg := cb.requireProjectWriteScope(filepath.Join(link, "new.go")); msg == "" {
		t.Fatal("expected symlink escape write to be rejected")
	}
}

func TestCodingSubAgentRejectsReadsOutsideProjectPath(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	inside := filepath.Join(project, "sub", "main.go")
	outside := filepath.Join(root, "outside.go")
	prefixSibling := filepath.Join(root, "project-other", "main.go")
	for _, file := range []string{inside, outside, prefixSibling} {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	if msg := cb.requireProjectReadScope(inside, "read_file"); msg != "" {
		t.Fatalf("expected project read path to be allowed, got %q", msg)
	}
	if msg := cb.requireProjectReadScope(outside, "read_file"); !strings.Contains(msg, "项目目录外") {
		t.Fatalf("expected outside read path to be rejected, got %q", msg)
	}
	if msg := cb.requireProjectReadScope(prefixSibling, "ripgrep"); !strings.Contains(msg, "项目目录外") {
		t.Fatalf("expected prefix sibling read path to be rejected, got %q", msg)
	}
}

func TestCodingSubAgentRejectsCommandAndDiffOutsideProjectPath(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	inside := filepath.Join(project, "sub")
	outside := filepath.Join(root, "outside")
	prefixSibling := filepath.Join(root, "project-other")
	for _, dir := range []string{inside, outside, prefixSibling} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	if msg := cb.requireProjectWorkingDirScope(inside); msg != "" {
		t.Fatalf("expected project command working dir to be allowed, got %q", msg)
	}
	if msg := cb.requireProjectWorkingDirScope(outside); !strings.Contains(msg, "项目目录外执行命令") {
		t.Fatalf("expected outside command dir to be rejected, got %q", msg)
	}
	if msg := cb.requireProjectWorkingDirScope(prefixSibling); !strings.Contains(msg, "项目目录外执行命令") {
		t.Fatalf("expected prefix sibling command dir to be rejected, got %q", msg)
	}

	if msg := cb.requireProjectDiffScope(inside); msg != "" {
		t.Fatalf("expected project diff path to be allowed, got %q", msg)
	}
	if msg := cb.requireProjectDiffScope(outside); !strings.Contains(msg, "项目目录外的 diff") {
		t.Fatalf("expected outside diff path to be rejected, got %q", msg)
	}
}

func TestCodingSubAgentFilesModifiedAreRelativeAndSorted(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	cb.trackFile(filepath.Join(project, "z.go"))
	cb.trackFile(filepath.Join(project, "sub", "a.go"))
	files := cb.getFilesModified()
	want := []string{"sub/a.go", "z.go"}
	if len(files) != len(want) {
		t.Fatalf("files len = %d, want %d: %#v", len(files), len(want), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files[%d] = %q, want %q; all=%#v", i, files[i], want[i], files)
		}
	}
}

func TestCodingSubAgentFilesCreatedAreRelativeAndSorted(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	cb.trackCreatedFile(filepath.Join(project, "z.go"))
	cb.trackCreatedFile(filepath.Join(project, "sub", "a.go"))
	files := cb.getFilesCreated()
	want := []string{"sub/a.go", "z.go"}
	if len(files) != len(want) {
		t.Fatalf("files len = %d, want %d: %#v", len(files), len(want), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files[%d] = %q, want %q; all=%#v", i, files[i], want[i], files)
		}
	}
}

func TestCodingSubAgentFilesReadAreRelativeAndSorted(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(project, "sub", "a.go")
	z := filepath.Join(project, "z.go")
	if err := os.WriteFile(a, []byte("package sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(z, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackReadFile(z)
	cb.trackReadFile(a)
	files := cb.getFilesRead()
	want := []string{"sub/a.go", "z.go"}
	if len(files) != len(want) {
		t.Fatalf("files len = %d, want %d: %#v", len(files), len(want), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files[%d] = %q, want %q; all=%#v", i, files[i], want[i], files)
		}
	}
}

func TestCodingFileExistsOnlyReturnsTrueForFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if !codingFileExists(file) {
		t.Fatalf("expected codingFileExists(%q) to be true", file)
	}
	if codingFileExists(dir) {
		t.Fatalf("expected codingFileExists(%q) to be false for directory", dir)
	}
	if codingFileExists(filepath.Join(dir, "missing.txt")) {
		t.Fatal("expected missing file to return false")
	}
}

func TestIsPathWithinDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	ok, err := isPathWithinDir(filepath.Join(project, "sub", "file.go"), project)
	if err != nil || !ok {
		t.Fatalf("expected path inside dir, ok=%v err=%v", ok, err)
	}
	ok, err = isPathWithinDir(filepath.Join(root, "project2", "file.go"), project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected sibling prefix path to be outside dir")
	}
	ok, err = isPathWithinDir(filepath.Join(project, "sub", "new", "file.go"), project)
	if err != nil || !ok {
		t.Fatalf("expected missing child path inside dir, ok=%v err=%v", ok, err)
	}
}

func TestIsPathWithinDirRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "linked-outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	ok, err := isPathWithinDir(filepath.Join(link, "new.go"), project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected symlink escape path to be outside project")
	}
}

func TestCodingSubAgentDisplayProjectPathResolvesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(project, "sub", "main.go")
	if err := os.MkdirAll(filepath.Dir(inside), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "linked-outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	if got := cb.displayProjectPath(inside); got != "sub/main.go" {
		t.Fatalf("inside path display = %q, want sub/main.go", got)
	}

	escapePath := filepath.Join(link, "new.go")
	got := cb.displayProjectPath(escapePath)
	if strings.Contains(filepath.ToSlash(got), "linked-outside/new.go") {
		t.Fatalf("symlink escape should not display as project-relative path, got %q", got)
	}
	if !strings.Contains(filepath.ToSlash(got), "outside/new.go") {
		t.Fatalf("symlink escape should display resolved outside path, got %q", got)
	}
}
func TestCodingSubAgentDefaultsRelativePathsToProjectPath(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	fileArgs := cb.withProjectRelativePath(map[string]interface{}{"path": "src/main.go"}, false)
	wantFile := filepath.Join(project, "src", "main.go")
	if got, _ := fileArgs["path"].(string); got != wantFile {
		t.Fatalf("relative file path = %q, want %q", got, wantFile)
	}
	spacedFileArgs := cb.withProjectRelativePath(map[string]interface{}{"path": "  src/spaced.go  "}, false)
	wantSpacedFile := filepath.Join(project, "src", "spaced.go")
	if got, _ := spacedFileArgs["path"].(string); got != wantSpacedFile {
		t.Fatalf("spaced relative file path = %q, want %q", got, wantSpacedFile)
	}

	listArgs := cb.withProjectRelativePath(map[string]interface{}{}, true)
	if got, _ := listArgs["path"].(string); got != project {
		t.Fatalf("empty list_directory path = %q, want project path %q", got, project)
	}

	bashArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "working_dir": "gui"})
	wantDir := filepath.Join(project, "gui")
	if got, _ := bashArgs["working_dir"].(string); got != wantDir {
		t.Fatalf("relative working_dir = %q, want %q", got, wantDir)
	}
	spacedBashArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "  go test ./...  ", "working_dir": "  gui  "})
	if got, _ := spacedBashArgs["working_dir"].(string); got != wantDir {
		t.Fatalf("spaced relative working_dir = %q, want %q", got, wantDir)
	}
	if got, _ := spacedBashArgs["command"].(string); got != "go test ./..." {
		t.Fatalf("spaced bash command = %q, want trimmed command", got)
	}

	searchArgs := cb.withDefaultProjectPath(map[string]interface{}{"path": "gui", "pattern": "func main"})
	wantSearchDir := filepath.Join(project, "gui")
	if got, _ := searchArgs["path"].(string); got != wantSearchDir {
		t.Fatalf("relative search path = %q, want %q", got, wantSearchDir)
	}
	spacedSearchArgs := cb.withDefaultProjectPath(map[string]interface{}{"path": "  gui  ", "pattern": "func main"})
	if got, _ := spacedSearchArgs["path"].(string); got != wantSearchDir {
		t.Fatalf("spaced relative search path = %q, want %q", got, wantSearchDir)
	}

	defaultSearchArgs := cb.withDefaultProjectPath(map[string]interface{}{"pattern": "func main"})
	if got, _ := defaultSearchArgs["path"].(string); got != project {
		t.Fatalf("empty search path = %q, want project path %q", got, project)
	}

	abs := filepath.Join(project, "abs.go")
	absArgs := cb.withProjectRelativePath(map[string]interface{}{"path": abs}, false)
	if got, _ := absArgs["path"].(string); got != abs {
		t.Fatalf("absolute path changed: got %q, want %q", got, abs)
	}
}

func TestCodingSubAgentNormalizesBashTimeout(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	defaultArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./..."})
	if got, _ := defaultArgs["timeout"].(float64); got != float64(codingSubAgentDefaultBashTimeout) {
		t.Fatalf("default timeout = %v, want %d", got, codingSubAgentDefaultBashTimeout)
	}

	largeArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "timeout": float64(999)})
	if got, _ := largeArgs["timeout"].(float64); got != float64(codingSubAgentMaxBashTimeout) {
		t.Fatalf("large timeout = %v, want %d", got, codingSubAgentMaxBashTimeout)
	}

	invalidArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "timeout": float64(-1)})
	if got, _ := invalidArgs["timeout"].(float64); got != float64(codingSubAgentDefaultBashTimeout) {
		t.Fatalf("invalid timeout = %v, want %d", got, codingSubAgentDefaultBashTimeout)
	}

	normalArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "timeout": 90})
	if got, _ := normalArgs["timeout"].(float64); got != float64(corelib.MinAgentTimeoutSec) {
		t.Fatalf("normal timeout = %v, want %d", got, corelib.MinAgentTimeoutSec)
	}

	midArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "timeout": 300})
	if got, _ := midArgs["timeout"].(float64); got != 300 {
		t.Fatalf("mid timeout = %v, want 300", got)
	}
}

func TestRejectDisallowedCodingBashCommand(t *testing.T) {
	disallowed := []string{
		"git reset --hard HEAD",
		"CGO_ENABLED=0 git reset --hard HEAD",
		"env GIT_DIR=.git git reset --hard HEAD",
		"env -u GIT_CONFIG git reset --hard HEAD",
		"cross-env CI=1 git reset --hard HEAD",
		"git -C . reset --hard HEAD",
		"git -C=. reset --hard HEAD",
		"git.exe -C . reset --hard HEAD",
		"git.cmd reset --hard HEAD",
		"git.bat reset --hard HEAD",
		"powershell -Command git reset --hard HEAD",
		"powershell /Command git reset --hard HEAD",
		"pwsh -c git reset --hard HEAD",
		"cmd /c git reset --hard HEAD",
		`cmd /c "git reset --hard HEAD"`,
		"cmd /s /c git reset --hard HEAD",
		"go test ./...; git reset --hard HEAD",
		"go test ./... && git reset --hard HEAD",
		"go test ./... || git checkout -- .",
		"(git reset --hard HEAD)",
		"bash -c touch src/a.go",
		"bash -lc touch src/a.go",
		`bash -lc "touch src/a.go"`,
		"bash -lc git reset --hard HEAD",
		`bash -lc "git reset --hard HEAD"`,
		"git reset --soft HEAD~1",
		"git checkout -- .",
		"git -C repo checkout main",
		"git checkout HEAD -- gui/coding_subagent.go",
		"git checkout abc123 -- src/file.go",
		"git checkout .",
		"git checkout main",
		"git checkout -f HEAD",
		"git restore .",
		"git switch main",
		"git merge feature",
		"git rebase main",
		"git stash push",
		"git add src/a.go",
		"git -C . add src/a.go",
		"git commit -m update",
		"git apply fix.patch",
		"git am patch.mbox",
		"git cherry-pick abc123",
		"git revert abc123",
		"git rm src/a.go",
		"git mv src/a.go src/b.go",
		"git update-index --assume-unchanged src/a.go",
		"git read-tree HEAD",
		"git clean -fdx",
		"git clean --force -d",
		"git --work-tree . clean -fd",
		"rm -rf build",
		"DRY_RUN=0 rm -rf build",
		"env DRY_RUN=0 rm -rf build",
		"rm -r build",
		"Remove-Item -Recurse .\\build",
		"Remove-Item -LiteralPath .\\build -Recurse",
		"Remove-Item -r .\\build",
		"Remove-Item -rf .\\build",
		"rm -r -fo .\\build",
		"ri -r .\\build",
		"cmd /c rd /s build",
		"(rm -rf build)",
		"Set-Content src\\a.go 'package main'",
		"Set-Content.ps1 src\\a.go 'package main'",
		"FOO=bar Set-Content src\\a.go 'package main'",
		"powershell -Command Set-Content src\\a.go 'package main'",
		"powershell -NoProfile -Command Set-Content src\\a.go 'package main'",
		"powershell -EncodedCommand Z2l0IHJlc2V0IC0taGFyZCBIRUFE",
		"powershell -enc Z2l0IHJlc2V0IC0taGFyZCBIRUFE",
		"pwsh -EncodedCommand Z2l0IHJlc2V0IC0taGFyZCBIRUFE",
		"pwsh /EncodedArguments AAAA",
		`powershell -NoProfile -Command "Set-Content src\\a.go x"`,
		"pwsh -c Set-Content src\\a.go x",
		"cmd /c copy src\\a.go src\\b.go",
		"go test ./...; New-Item src\\a.go",
		"go test ./... && Set-Content src\\a.go x",
		"Add-Content src\\a.go 'line'",
		"Out-File src\\a.go",
		"New-Item src\\a.go",
		"Copy-Item src\\a.go src\\b.go",
		"Move-Item src\\a.go src\\b.go",
		"Rename-Item src\\a.go b.go",
		"sc src\\a.go 'package main'",
		"ac src\\a.go 'line'",
		"ni src\\a.go",
		"Tee-Object src\\a.log",
		"Export-Csv src\\out.csv",
		"Start-Transcript src\\trace.log",
		"touch src/a.go",
		"mkdir src/generated",
		"md src\\generated",
		"truncate -s 0 src/a.go",
		"xcopy src dst /s",
		"robocopy src dst /mir",
		"'hello' > src\\a.go",
		"'hello' >> src\\a.go",
		"go test ./... | tee test.log",
		"go test ./... | Tee-Object test.log",
		"sed -i 's/a/b/' src/a.go",
		"perl -pi -e 's/a/b/' src/a.go",
		"node -e \"require('fs').writeFileSync('src/a.go','x')\"",
		"node -e \"require('fs').promises.writeFile('src/a.go','x')\"",
		"node -e \"require('fs').promises.appendFile('src/a.go','x')\"",
		"node -e \"require('fs').promises.copyFile('src/a.go','src/b.go')\"",
		"node -e \"require('fs').promises.rename('src/a.go','src/b.go')\"",
		"node --input-type module --eval \"require('fs').writeFileSync('src/a.go','x')\"",
		"node --eval=require('fs').writeFileSync('src/a.go','x')",
		"node -e \"require('fs').renameSync('src/a.go','src/b.go')\"",
		"node -e \"require('fs').rmSync('src/a.go')\"",
		"node -e \"require('fs').mkdirSync('src/generated')\"",
		"node -e \"require('fs').promises.rm('src/a.go')\"",
		"node -e \"const fs = require('fs'); fs.rm('src/a.go', () => {})\"",
		"node -e \"require('fs').unlink('src/a.go', () => {})\"",
		"bun -e \"Bun.write('src/a.go','x')\"",
		"bun --eval \"require('fs').promises.appendFile('src/a.go','x')\"",
		"deno eval \"Deno.writeTextFileSync('src/a.go','x')\"",
		"deno eval --allow-write \"Deno.rename('src/a.go','src/b.go')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').write_text('x')\"",
		"python -I -c \"open('src/a.go','w').write('x')\"",
		"python -copen('src/a.go','w').write('x')",
		"python -c \"open('src/a.go','w').write('x')\"",
		"python -c \"open('src/a.go','wb').write(b'x')\"",
		"python -c \"p='src/a.go'; open(p,'w+b').write(b'x')\"",
		"python -c \"open('src/a.go','w+b').write(b'x')\"",
		"python -c \"open('src/a.go', mode='wt').write('x')\"",
		"python -c \"open('src/a.go', mode='x').write('x')\"",
		"python -c \"open('src/a.go', mode='r+').write('x')\"",
		"python -c \"open('src/a.go', mode='r+b').write(b'x')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').open(mode='ab').write(b'x')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').touch()\"",
		"python -c \"from pathlib import Path; Path('src/a.go').rename('src/b.go')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').replace('src/b.go')\"",
		"python -c \"from pathlib import Path; Path('src/generated').rmdir()\"",
		"python -c \"import os; os.remove('src/a.go')\"",
		"python -c \"import os; os.rename('src/a.go','src/b.go')\"",
		"python -c \"import shutil; shutil.copyfile('src/a.go','src/b.go')\"",
		"dd if=/dev/zero of=src/a.go bs=1 count=1",
		"rmdir /s build",
		"rd /s build",
		"del /s *.tmp",
		"erase /s *.tmp",
	}
	for _, command := range disallowed {
		if msg := rejectDisallowedCodingBashCommand(command); msg == "" {
			t.Fatalf("expected command to be rejected: %q", command)
		}
	}

	allowed := []string{
		"go test ./...",
		"go build ./...",
		"npm run build",
		"git diff -- .",
		"go -C gui env",
		"go fmt ./...",
		"cargo fmt --all",
		"npm run format",
		"npm run fmt",
		"pnpm run format",
		"yarn format",
		"yarn workspaces foreach -A run format",
		"yarn workspaces foreach -A exec vite --host 0.0.0.0",
		"make fmt",
		"make format",
		"just",
		"just --list",
		"just --list test",
		"just --summary test",
		"just fmt",
		"just dev",
		"task --list",
		"task --list test",
		"task -l test",
		"go-task --list-all test",
		"task --dry test",
		"task dev",
		"task format",
		"mage -l",
		"mage -l test",
		"mage -h test",
		"mage dev",
		"bazel run //app:server",
		"bazel clean",
		"bazel query //...",
		"pants run src/python/app.py",
		"pants tailor",
		"buck2 run //app:server",
		"buck2 clean",
		"prettier .",
		"prettier --write .",
		"npx prettier --write .",
		"npm run lint -- --fix",
		"pnpm run lint -- --fix",
		"yarn lint --fix",
		"eslint --fix .",
		"npx eslint --fix .",
		"ruff check --fix .",
		"python -m ruff check --fix .",
		"golangci-lint run --fix ./...",
		"rubocop -a",
		"bundle exec rubocop -A",
		"biome format .",
		"biome format --write .",
		"biome check --write .",
		"git -C . diff -- .",
		"git status --short",
		"git -C . status --short",
		"git log --oneline -5",
		"git clean -nd",
		"git clean --dry-run -d",
		"git clean docs/file.txt",
		"git clean -n; echo force",
		"Remove-Item .\\build\\old.log",
		"go test ./... 2>&1",
		`go test ./... -run "TestAPI|TestHandler"`,
		`bash -lc 'go test ./... -run "TestAPI|TestHandler"'`,
		`powershell -NoProfile -Command 'go test ./... -run "TestAPI|TestHandler"'`,
		"bash -c go test ./...",
		`bash -lc "go test ./..."`,
		`powershell -NoProfile -Command "go test ./..."`,
		"bash -lc go test ./...",
		"powershell -NoProfile -Command go test ./...",
		"pwsh -Command go test ./...",
		"python -m pytest",
		"node --test",
		"npm run lint",
		"npx tsc --noEmit",
		"echo mkdir",
		"echo rm -rf build",
		"echo git reset --hard HEAD",
		"Write-Output Remove-Item -Recurse",
		"Write-Output Set-Content",
		`echo "a > b"`,
		`Write-Output "a | tee"`,
		`printf 'x >> y'`,
		"go test ./... | Select-String FAIL",
		"python -c \"print('touch src/a.go')\"",
		"node -e \"console.log('writeFileSync')\"",
		"bun -e \"console.log('Bun.write')\"",
		"deno eval \"console.log('Deno.writeTextFile')\"",
		"python -c",
		"node -e",
		"node --input-type module --eval \"console.log('writeFileSync')\"",
		"node --eval=console.log('writeFileSync')",
		"node -e \"console.log('rmSync')\"",
		"node -e \"const obj = { rm() {} }; obj.rm()\"",
		"python -c \"print('open(\\\"src/a.go\\\",\\\"w\\\")')\"",
		"python -c \"open('src/a.go','rb').read()\"",
		"python -c \"open('src/a.go', mode='r').read()\"",
		"python -c \"open('src/a.go', mode='rt').read()\"",
		"python -c \"open('src/a.go','rb').read(); print('w')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').open(mode='rb').read(); print('a')\"",
		"python -c \"from pathlib import Path; Path('src/a.go').open(mode='rb').read()\"",
		"python -c \"print('Path.touch')\"",
		"python -c \"print('Path.rename')\"",
		"python -I -c \"print('open(\\\"src/a.go\\\",\\\"w\\\")')\"",
		"python -cprint('open(\\\"src/a.go\\\",\\\"w\\\")')",
	}
	for _, command := range allowed {
		if msg := rejectDisallowedCodingBashCommand(command); msg != "" {
			t.Fatalf("expected command to be allowed: %q got %q", command, msg)
		}
	}
}

func TestClassifyCodingGuardrail(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		path     string
		command  string
		result   string
		category string
	}{
		{name: "git", tool: "bash", command: "git reset --hard HEAD", category: "git"},
		{name: "delete", tool: "bash", command: "Remove-Item -Recurse .\\build", category: "delete"},
		{name: "shell_write", tool: "bash", command: "Set-Content src\\a.go x", category: "shell_write"},
		{name: "scope", tool: "read_file", path: "..\\outside.go", result: "outside project", category: "scope"},
		{name: "scope_result_marker", tool: "call_mcp_tool", result: "requested path is outside the project scope", category: "scope"},
		{name: "host", tool: "read_file", result: "coding subagent host tool handler is unavailable", category: "host"},
		{name: "policy_with_project_word", tool: "call_mcp_tool", result: "project approval policy rejected this dynamic tool", category: "policy"},
	}
	for _, tc := range cases {
		if got := classifyCodingGuardrailCategory(tc.tool, tc.path, tc.command, tc.result).String(); got != tc.category {
			t.Fatalf("%s category = %q, want %q", tc.name, got, tc.category)
		}
	}
}

func TestHasWindowsShellCompatibilitySyntax(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{command: "mkdir -p build && cmake -S . -B build", want: true},
		{command: "MKDIR -P build", want: true},
		{command: `bash -lc "mkdir -p build && cmake -S . -B build"`, want: true},
		{command: `BASH -LC "MKDIR -P build"`, want: true},
		{command: `PowerShell -Command "MKDIR -P build"`, want: true},
		{command: `Cross-Env CI=1 MKDIR -P build`, want: true},
		{command: `ENV FOO=1 MKDIR -P build`, want: true},
		{command: `node -e "console.log('a && b'); console.log('mkdir -p docs')"`, want: false},
		{command: `go test ./... -run "TestA&&TestB"`, want: false},
		{command: `Write-Output "mkdir -p is mentioned in docs"`, want: false},
	}
	for _, tc := range cases {
		if got := hasWindowsShellCompatibilitySyntax(tc.command); got != tc.want {
			t.Fatalf("hasWindowsShellCompatibilitySyntax(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestRejectDisallowedCodingBashCommandAllowsWindowsAndAnd(t *testing.T) {
	if normalizedRemotePlatform() != "windows" {
		t.Skip("PowerShell command separator guardrail is Windows-specific")
	}
	if msg := rejectDisallowedCodingBashCommand("cd build && cmake -S . -B build"); msg != "" {
		t.Fatalf("unquoted && should be normalized by the executor, got %q", msg)
	}
	msg := rejectDisallowedCodingBashCommand("mkdir -p build")
	if !strings.Contains(msg, "PowerShell") || !strings.Contains(msg, "mkdir -p") {
		t.Fatalf("expected mkdir compatibility rejection, got %q", msg)
	}
	if msg := rejectDisallowedCodingBashCommand(`node -e "console.log('a && b'); console.log('mkdir -p docs')"`); msg != "" {
		t.Fatalf("quoted bash-like text should not trigger Windows compatibility rejection, got %q", msg)
	}
}

func TestHasGitCleanForceFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "force short", args: []string{"-fdx"}, want: true},
		{name: "force long", args: []string{"--force", "-d"}, want: true},
		{name: "dry run short", args: []string{"-nd"}, want: false},
		{name: "dry run long", args: []string{"--dry-run", "-d"}, want: false},
		{name: "path contains f", args: []string{"docs/file.txt"}, want: false},
	}
	for _, tc := range cases {
		if got := hasGitCleanForceFlag(tc.args); got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCodingSubAgentEnsureFinalGitDiffSkipsWhenNoFilesModified(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}
	checked, summary := cb.ensureFinalGitDiff(nil, nil)
	if checked {
		t.Fatal("expected no git diff check when no files were modified")
	}
	if summary != "" {
		t.Fatalf("expected empty diff summary, got %q", summary)
	}
}

func TestCompactAndAppendSubAgentDiffSummary(t *testing.T) {
	empty := compactSubAgentDiff("(命令执行完成，无输出)")
	if empty != "git diff 无输出" {
		t.Fatalf("unexpected empty diff summary: %q", empty)
	}

	long := compactSubAgentDiff(strings.Repeat("diff --git a/x b/x\n", 300))
	if !strings.Contains(long, "截断") {
		t.Fatal("expected long diff to be truncated")
	}

	summary := appendSubAgentDiffSummary("完成了", "diff --git a/x b/x")
	if !strings.Contains(summary, "## Diff 自检") || !strings.Contains(summary, "diff --git") {
		t.Fatalf("expected diff section to be appended, got %q", summary)
	}
}

func TestCompactSubAgentModelSummary(t *testing.T) {
	if got := compactSubAgentModelSummary("  完成了  "); got != "完成了" {
		t.Fatalf("expected trimmed summary, got %q", got)
	}
	long := compactSubAgentModelSummary(strings.Repeat("模型说明\n", codingSubAgentModelSummaryMaxRunes))
	if !strings.Contains(long, "截断") {
		t.Fatalf("expected long model summary to be truncated, got %q", long)
	}
	if len([]rune(long)) > codingSubAgentModelSummaryMaxRunes+20 {
		t.Fatalf("model summary too long: %d", len([]rune(long)))
	}
}

func TestFallbackSubAgentTaskSummaryReflectsStatus(t *testing.T) {
	task := &TaskItem{Index: 2, Title: "Fix parser"}
	passed := fallbackSubAgentTaskSummary(TaskExecPassed, task, 3, 4)
	if !strings.Contains(passed, "任务执行完成 T2") || strings.Contains(passed, "失败") {
		t.Fatalf("passed fallback summary should report completion, got %q", passed)
	}
	failed := fallbackSubAgentTaskSummary(TaskExecFailed, task, 5, 6)
	if !strings.Contains(failed, "任务运行错误 T2") || strings.Contains(failed, "执行完成") {
		t.Fatalf("failed fallback summary should not claim completion, got %q", failed)
	}
	skipped := fallbackSubAgentTaskSummary(TaskExecSkipped, task, 1, 0)
	if !strings.Contains(skipped, "任务已跳过 T2") {
		t.Fatalf("skipped fallback summary should report skipped status, got %q", skipped)
	}

	rebased := rebaseFallbackSubAgentTaskSummary(passed+"\n\n## 验证状态\n\nmissing", TaskExecFailed, task, 3, 4)
	if !strings.Contains(rebased, "任务运行错误 T2") || strings.Contains(rebased, "任务执行完成") || !strings.Contains(rebased, "## 验证状态") {
		t.Fatalf("rebased fallback summary should reflect final failure and preserve sections, got %q", rebased)
	}
}

func TestSubAgentQualityReportSummaryAppendedToExecutionSummary(t *testing.T) {
	summary := fallbackSubAgentTaskSummary(TaskExecPassed, &TaskItem{Index: 1, Title: "Task"}, 2, 3)
	summary = appendSubAgentQualityReportSummary(summary, &CodingSubAgentResult{
		QualityStatus:  codingSubAgentQualityFailed,
		QualitySummary: "verification not run",
	})
	if !strings.Contains(summary, "任务执行完成 T1") || !strings.Contains(summary, "## 质量审计") || !strings.Contains(summary, "FAILED: verification not run") {
		t.Fatalf("execution summary should include quality audit section, got %q", summary)
	}
}

func TestApplySubAgentQualityOutcomeFailsPassedResult(t *testing.T) {
	status, errMsg := applySubAgentQualityOutcome(TaskExecPassed, "", codingSubAgentQualityFailed, "verification not run; diff not checked", 2)
	if status != TaskExecFailed || !strings.Contains(errMsg, "quality audit failed") || !strings.Contains(errMsg, "2 issue") {
		t.Fatalf("quality failure should fail passed task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentQualityOutcome(TaskExecFailed, "agent loop failed", codingSubAgentQualityFailed, "verification not run", 1)
	if status != TaskExecFailed || !strings.Contains(errMsg, "agent loop failed") || !strings.Contains(errMsg, "quality audit failed") || !strings.Contains(errMsg, "verification not run") {
		t.Fatalf("quality fallback should append to existing failure, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentQualityOutcome(TaskExecPassed, "", codingSubAgentQualityWarning, "dynamic tool warning", 1)
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("quality warning should not fail task, got status=%s err=%q", status, errMsg)
	}
}

func TestCompactSubAgentErrorSummary(t *testing.T) {
	if got := compactSubAgentErrorSummary("  failed  "); got != "failed" {
		t.Fatalf("expected trimmed error, got %q", got)
	}
	long := compactSubAgentErrorSummary(strings.Repeat("错误详情\n", codingSubAgentErrorSummaryMaxRunes))
	if !strings.Contains(long, "截断") {
		t.Fatalf("expected long error summary to be truncated, got %q", long)
	}
	if len([]rune(long)) > codingSubAgentErrorSummaryMaxRunes+20 {
		t.Fatalf("error summary too long: %d", len([]rune(long)))
	}
}

func TestAppendSubAgentFileChangeSummary(t *testing.T) {
	summary := appendSubAgentFileChangeSummary(
		"完成",
		[]string{"api/new.go", "api/existing.go"},
		[]string{"api/new.go"},
	)
	if !strings.Contains(summary, "## 文件变更") {
		t.Fatalf("expected file change section, got %q", summary)
	}
	if !strings.Contains(summary, "created: `api/new.go`") {
		t.Fatalf("expected created file entry, got %q", summary)
	}
	if !strings.Contains(summary, "modified: `api/existing.go`") {
		t.Fatalf("expected modified file entry, got %q", summary)
	}
	if strings.Contains(summary, "modified: `api/new.go`") {
		t.Fatalf("created file should not be duplicated as modified: %q", summary)
	}
}

func TestAppendSubAgentFileChangeSummaryCapsLongLists(t *testing.T) {
	var created []string
	var modified []string
	for i := 0; i < codingSubAgentFileChangeSummaryMax+2; i++ {
		created = append(created, fmt.Sprintf("api/new_%02d.go", i))
		modified = append(modified, fmt.Sprintf("api/existing_%02d.go", i))
	}
	filesModified := append([]string{}, created...)
	filesModified = append(filesModified, modified...)

	summary := appendSubAgentFileChangeSummary("完成", filesModified, created)
	if count := strings.Count(summary, "- created: `"); count != codingSubAgentFileChangeSummaryMax {
		t.Fatalf("created entry count = %d, want %d; summary=%q", count, codingSubAgentFileChangeSummaryMax, summary)
	}
	if count := strings.Count(summary, "- modified: `"); count != codingSubAgentFileChangeSummaryMax {
		t.Fatalf("modified entry count = %d, want %d; summary=%q", count, codingSubAgentFileChangeSummaryMax, summary)
	}
	if strings.Contains(summary, "api/new_21.go") || strings.Contains(summary, "api/existing_21.go") {
		t.Fatalf("file change summary should be capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 2 个新建文件未展开") || !strings.Contains(summary, "还有 2 个修改文件未展开") {
		t.Fatalf("expected remaining file counts, got %q", summary)
	}
}

func TestAppendSubAgentFileChangeSummaryDedupesAndSorts(t *testing.T) {
	summary := appendSubAgentFileChangeSummary(
		"完成",
		[]string{" api/z.go ", "api/a.go", "api/z.go", "api/new.go", ""},
		[]string{"api/new.go", "api/new.go", " "},
	)
	if strings.Count(summary, "created: `api/new.go`") != 1 {
		t.Fatalf("created file should be listed once, got %q", summary)
	}
	if strings.Count(summary, "modified: `api/z.go`") != 1 {
		t.Fatalf("modified file should be listed once, got %q", summary)
	}
	if strings.Contains(summary, "modified: `api/new.go`") {
		t.Fatalf("created file should not be duplicated as modified, got %q", summary)
	}
	if strings.Index(summary, "modified: `api/a.go`") > strings.Index(summary, "modified: `api/z.go`") {
		t.Fatalf("modified files should be sorted, got %q", summary)
	}
}

func TestAppendSubAgentFileChangeSummarySkipsEmpty(t *testing.T) {
	summary := appendSubAgentFileChangeSummary("完成", nil, nil)
	if summary != "完成" {
		t.Fatalf("empty file changes should not alter summary, got %q", summary)
	}
}

func TestCodingSubAgentGuardrailTracking(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	result := cb.rejectToolCall("bash", map[string]interface{}{
		"command":     "git reset --hard HEAD",
		"working_dir": project,
	}, "拒绝执行高风险命令：git reset --hard HEAD")
	if !strings.Contains(result, "拒绝执行") {
		t.Fatalf("unexpected reject result: %q", result)
	}

	violations := cb.getGuardrailViolations()
	if len(violations) != 1 {
		t.Fatalf("expected 1 guardrail violation, got %d", len(violations))
	}
	if violations[0].Tool != "bash" || violations[0].Command != "git reset --hard HEAD" {
		t.Fatalf("unexpected guardrail violation: %#v", violations[0])
	}
	if violations[0].Category != "git" {
		t.Fatalf("guardrail category = %q, want git", violations[0].Category)
	}
	if violations[0].Path != "." && violations[0].Path != project {
		t.Fatalf("expected project-relative or project path, got %q", violations[0].Path)
	}
}

func TestAppendSubAgentGuardrailSummary(t *testing.T) {
	summary := appendSubAgentGuardrailSummary("完成", []CodingSubAgentGuardrailViolation{
		{Tool: "bash", Category: "git", Path: ".", Command: "git reset --hard HEAD", Summary: "拒绝执行高风险命令"},
	})
	if !strings.Contains(summary, "## 安全边界") {
		t.Fatalf("expected guardrail section, got %q", summary)
	}
	if !strings.Contains(summary, "blocked `bash`") || !strings.Contains(summary, "git reset --hard HEAD") {
		t.Fatalf("expected blocked bash command entry, got %q", summary)
	}
	if !strings.Contains(summary, "category: `git`") {
		t.Fatalf("expected guardrail category, got %q", summary)
	}
}

func TestAppendSubAgentGuardrailSummaryCapsLongLists(t *testing.T) {
	var violations []CodingSubAgentGuardrailViolation
	for i := 0; i < codingSubAgentGuardrailSummaryMax+2; i++ {
		violations = append(violations, CodingSubAgentGuardrailViolation{
			Tool:    "read_file",
			Path:    fmt.Sprintf("../outside-%d.go", i),
			Summary: "拒绝读取项目目录外的路径",
		})
	}

	summary := appendSubAgentGuardrailSummary("完成", violations)
	if count := strings.Count(summary, "- blocked `"); count != codingSubAgentGuardrailSummaryMax {
		t.Fatalf("blocked entry count = %d, want %d; summary=%q", count, codingSubAgentGuardrailSummaryMax, summary)
	}
	if !strings.Contains(summary, "还有 2 条安全边界拦截未展开") {
		t.Fatalf("expected remaining guardrail count, got %q", summary)
	}
}

func TestAppendSubAgentGuardrailSummaryAggregatesDuplicates(t *testing.T) {
	violations := []CodingSubAgentGuardrailViolation{
		{Tool: "bash", Path: ".", Command: "git reset --hard HEAD", Summary: "拒绝执行高风险命令"},
		{Tool: "bash", Path: ".", Command: "git reset --hard HEAD", Summary: "拒绝执行高风险命令"},
		{Tool: "read_file", Path: "../outside.go", Summary: "拒绝读取项目目录外的路径"},
	}

	summary := appendSubAgentGuardrailSummary("完成", violations)
	if count := strings.Count(summary, "- blocked `"); count != 2 {
		t.Fatalf("blocked entry count = %d, want 2; summary=%q", count, summary)
	}
	if !strings.Contains(summary, "blocked `bash` x2") {
		t.Fatalf("expected duplicate bash guardrail to show x2, got %q", summary)
	}
}

func TestAppendSubAgentGuardrailSummaryCompactsLongEntries(t *testing.T) {
	longPath := "../" + strings.Repeat("very-long-folder/", 30) + "outside.go"
	longCommand := "git reset --hard HEAD" + strings.Repeat(" --very-long-flag", 30)
	longSummary := "拒绝执行高风险命令：" + strings.Repeat("detail ", 80)

	summary := appendSubAgentGuardrailSummary("完成", []CodingSubAgentGuardrailViolation{
		{Tool: "bash`tool", Path: longPath, Command: longCommand, Summary: longSummary},
	})
	if !strings.Contains(summary, "blocked `bash'tool`") {
		t.Fatalf("expected tool name to be escaped, got %q", summary)
	}
	if !strings.Contains(summary, "截断") {
		t.Fatalf("expected long guardrail entry to be truncated, got %q", summary)
	}
	if strings.Contains(summary, strings.Repeat("very-long-folder/", 20)) {
		t.Fatalf("expected long path to be compacted, got %q", summary)
	}
	if strings.Count(summary, "detail") > 45 {
		t.Fatalf("expected guardrail detail to be compacted, got %q", summary)
	}
}

func TestAppendSubAgentGuardrailSummarySkipsEmpty(t *testing.T) {
	summary := appendSubAgentGuardrailSummary("完成", nil)
	if summary != "完成" {
		t.Fatalf("empty guardrail list should not alter summary, got %q", summary)
	}
}

func TestCodingSubAgentCommandTracking(t *testing.T) {
	cb := &codingSubAgentCallbacks{}
	cb.trackCommandResult(map[string]interface{}{
		"command":     "go test ./...",
		"working_dir": "D:\\repo",
	}, "ok github.com/example/project", true)
	cb.trackCommandResult(map[string]interface{}{"command": ""}, "ignored", false)

	commands := cb.getCommandsRun()
	if len(commands) != 1 {
		t.Fatalf("expected 1 command record, got %d", len(commands))
	}
	if commands[0].Command != "go test ./..." || commands[0].WorkingDir != "D:\\repo" {
		t.Fatalf("unexpected command record: %#v", commands[0])
	}
	if !commands[0].Succeeded {
		t.Fatal("expected command to be marked successful")
	}
}

func TestCodingSubAgentCommandResultJSONOmitsInternalSequence(t *testing.T) {
	cmd := CodingSubAgentCommandResult{
		Command:    "go test ./...",
		WorkingDir: "D:\\repo",
		Succeeded:  true,
		Summary:    "ok",
		seq:        42,
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	if strings.Contains(encoded, "seq") || strings.Contains(encoded, "42") {
		t.Fatalf("internal sequence must not be serialized, got %s", encoded)
	}
	for _, want := range []string{"Command", "WorkingDir", "Succeeded", "Summary"} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("serialized command result missing %s: %s", want, encoded)
		}
	}
}
func TestCodingSubAgentRejectedCommandTracking(t *testing.T) {
	cb := &codingSubAgentCallbacks{}
	cb.trackCommandResult(map[string]interface{}{
		"command":     "git reset --hard HEAD",
		"working_dir": "D:\\repo",
	}, "拒绝执行高风险命令：git reset --hard HEAD", false)

	commands := cb.getCommandsRun()
	if len(commands) != 1 {
		t.Fatalf("expected 1 command record, got %d", len(commands))
	}
	if commands[0].Succeeded {
		t.Fatalf("expected rejected command to be marked failed: %#v", commands[0])
	}
	if !strings.Contains(commands[0].Summary, "拒绝执行") {
		t.Fatalf("expected rejection summary, got %q", commands[0].Summary)
	}
}

func TestCommandResultSummaryAndStatus(t *testing.T) {
	long := compactCommandResult(strings.Repeat("line\n", 400))
	if !strings.Contains(long, "截断") {
		t.Fatal("expected long command output to be truncated")
	}

	summary := appendSubAgentCommandSummary("完成", []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: "D:\\repo", Succeeded: true, Summary: "PASS\nok"},
		{Command: "go test ./bad", Succeeded: false, Summary: "[错误] 退出码: 1"},
	})
	if !strings.Contains(summary, "## 命令验证") ||
		!strings.Contains(summary, "PASS: `go test ./...`") ||
		!strings.Contains(summary, "FAIL: `go test ./bad`") {
		t.Fatalf("unexpected command summary: %s", summary)
	}

	diagnosticSummary := appendSubAgentCommandSummary("完成", []CodingSubAgentCommandResult{
		{Command: "go test ./bad", Succeeded: false, Summary: "ordinary prelude\n[stderr] compiler: missing symbol\ncommand exited with code 2"},
	})
	if !strings.Contains(diagnosticSummary, "compiler: missing symbol") || strings.Contains(diagnosticSummary, "ordinary prelude") {
		t.Fatalf("command summary should prefer stderr diagnostics, got %q", diagnosticSummary)
	}

	failedSummary := compactFailedVerificationCommandResults([]CodingSubAgentCommandResult{
		{Command: "go test ./bad", Succeeded: false, Summary: "ordinary prelude\n[stderr] compiler: missing symbol\ncommand exited with code 2"},
	})
	if !strings.Contains(failedSummary, "compiler: missing symbol") || strings.Contains(failedSummary, "ordinary prelude") {
		t.Fatalf("failed verification summary should prefer stderr diagnostics, got %q", failedSummary)
	}

	stdoutFailedSummary := compactFailedVerificationCommandResults([]CodingSubAgentCommandResult{
		{Command: "go test ./bad", Succeeded: false, Summary: "ordinary prelude\ncoverage: 12.3%\nFAIL: TestCheckout expected 200 got 500\ncommand exited with code 1"},
	})
	if !strings.Contains(stdoutFailedSummary, "FAIL: TestCheckout expected 200 got 500") || strings.Contains(stdoutFailedSummary, "ordinary prelude") {
		t.Fatalf("failed verification summary should prefer stdout failure diagnostics, got %q", stdoutFailedSummary)
	}
}

func TestAppendSubAgentCommandSummaryHidesResolvedFailures(t *testing.T) {
	summary := appendSubAgentCommandSummary("完成", []CodingSubAgentCommandResult{
		{Command: `cmake --build D:\test\build --config Debug`, Succeeded: false, Summary: "PowerShell compatibility failed", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 2},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 3},
	})
	if strings.Contains(summary, "FAIL:") || strings.Contains(summary, "PowerShell compatibility failed") {
		t.Fatalf("resolved failed command should be hidden from command summary, got %q", summary)
	}
	if !strings.Contains(summary, "PASS: `& \"C:\\Program Files\\CMake\\bin\\cmake.exe\" --build D:\\test\\build --config Debug`") ||
		!strings.Contains(summary, "PASS: `& \"C:\\Program Files\\CMake\\bin\\ctest.exe\" --test-dir D:\\test\\build -C Debug --output-on-failure`") {
		t.Fatalf("successful verification commands should remain visible, got %q", summary)
	}
}

func TestAppendSubAgentCommandSummaryCapsLongLists(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentCommandSummaryMax+3; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}

	summary := appendSubAgentCommandSummary("完成", commands)
	if count := strings.Count(summary, "- PASS: `"); count != codingSubAgentCommandSummaryMax {
		t.Fatalf("command entry count = %d, want %d; summary=%q", count, codingSubAgentCommandSummaryMax, summary)
	}
	if strings.Contains(summary, "go test ./pkg/00") {
		t.Fatalf("command summary should be capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 3 条命令记录未展开") {
		t.Fatalf("expected remaining command count, got %q", summary)
	}
}

func TestAppendSubAgentCommandSummaryKeepsLateFailures(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentCommandSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}
	commands[len(commands)-1].Succeeded = false
	commands[len(commands)-1].Summary = "late failure"

	summary := appendSubAgentCommandSummary("完成", commands)
	if !strings.Contains(summary, "FAIL: `go test ./pkg/11`") || !strings.Contains(summary, "late failure") {
		t.Fatalf("late failed command should remain visible, got %q", summary)
	}
	if strings.Count(summary, "- PASS: `")+strings.Count(summary, "- FAIL: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestAppendSubAgentCommandSummaryPrefersLatestProblemsWhenCapped(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentCommandSummaryMax+3; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: false,
			Summary:   fmt.Sprintf("failure %02d", i),
		})
	}

	summary := appendSubAgentCommandSummary("完成", commands)
	if strings.Contains(summary, "go test ./pkg/00") || strings.Contains(summary, "failure 00") {
		t.Fatalf("oldest failed commands should be omitted when capped, got %q", summary)
	}
	if !strings.Contains(summary, "FAIL: `go test ./pkg/12`") || !strings.Contains(summary, "failure 12") {
		t.Fatalf("latest failed command should remain visible, got %q", summary)
	}
	if strings.Count(summary, "- FAIL: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d failed entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestAppendSubAgentCommandSummaryKeepsSelectedEntriesChronological(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentCommandSummaryMax+3; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}
	commands[1].Succeeded = false
	commands[1].Summary = "early failure"
	commands[3].Succeeded = false
	commands[3].Summary = "middle failure"

	summary := appendSubAgentCommandSummary("完成", commands)
	earlyFailure := strings.Index(summary, "FAIL: `go test ./pkg/01`")
	middleFailure := strings.Index(summary, "FAIL: `go test ./pkg/03`")
	recentPass := strings.Index(summary, "PASS: `go test ./pkg/12`")
	if earlyFailure < 0 || middleFailure < 0 || recentPass < 0 {
		t.Fatalf("expected selected failures and recent commands to remain visible, got %q", summary)
	}
	if !(earlyFailure < middleFailure && middleFailure < recentPass) {
		t.Fatalf("selected command entries should stay chronological, got %q", summary)
	}
	if strings.Count(summary, "- PASS: `")+strings.Count(summary, "- FAIL: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestAppendSubAgentCommandSummaryMarksEmptySuccess(t *testing.T) {
	summary := appendSubAgentCommandSummary("完成", []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s"},
	})
	if !strings.Contains(summary, "EMPTY: `pytest tests`") || !strings.Contains(summary, "no tests collected") {
		t.Fatalf("empty verification success should be marked distinctly, got %q", summary)
	}
}

func TestAppendSubAgentCommandSummaryKeepsLateEmptySuccess(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentCommandSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}
	commands[len(commands)-1].Command = "pytest tests"
	commands[len(commands)-1].Summary = "no tests collected in 0.01s"

	summary := appendSubAgentCommandSummary("完成", commands)
	if !strings.Contains(summary, "EMPTY: `pytest tests`") || !strings.Contains(summary, "no tests collected") {
		t.Fatalf("late empty verification success should remain visible, got %q", summary)
	}
	if strings.Count(summary, "- PASS: `")+strings.Count(summary, "- EMPTY: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestAppendSubAgentCommandSummaryCompactsLongEntries(t *testing.T) {
	longCommand := "go test ./..." + strings.Repeat(" -run TestVeryLongCaseName", 30)
	longCwd := "D:\\repo\\" + strings.Repeat("very-long-folder\\", 30)
	longSummary := strings.Repeat("failure output ", 80)

	summary := appendSubAgentCommandSummary("完成", []CodingSubAgentCommandResult{
		{Command: longCommand, WorkingDir: longCwd, Succeeded: false, Summary: longSummary},
	})
	if !strings.Contains(summary, "截断") {
		t.Fatalf("expected long command summary to be truncated, got %q", summary)
	}
	if strings.Contains(summary, strings.Repeat("very-long-folder\\", 20)) {
		t.Fatalf("expected long cwd to be compacted, got %q", summary)
	}
	if strings.Count(summary, "failure output") > 25 {
		t.Fatalf("expected first output line to be compacted, got %q", summary)
	}
}

func TestAppendSubAgentDynamicToolSummary(t *testing.T) {
	summary := appendSubAgentDynamicToolSummary("完成", []CodingSubAgentDynamicToolResult{
		{Tool: "manage_skill", Name: "impeccable", Succeeded: true, Summary: "skill ok"},
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "ordinary prelude\n[stderr] MCP call failed: browser closed\nfull output"},
	})
	if !strings.Contains(summary, "## 动态工具") ||
		!strings.Contains(summary, "PASS: `manage_skill` `impeccable`") ||
		!strings.Contains(summary, "FAIL: `call_mcp_tool` `browser/screenshot`") ||
		!strings.Contains(summary, "MCP call failed: browser closed") ||
		strings.Contains(summary, "ordinary prelude") {
		t.Fatalf("unexpected dynamic tool summary: %s", summary)
	}
}

func TestAppendSubAgentDynamicToolSummaryCapsLongLists(t *testing.T) {
	var tools []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentCommandSummaryMax+2; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("server/tool-%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}

	summary := appendSubAgentDynamicToolSummary("完成", tools)
	if count := strings.Count(summary, "- PASS: `call_mcp_tool`"); count != codingSubAgentCommandSummaryMax {
		t.Fatalf("dynamic tool entry count = %d, want %d; summary=%q", count, codingSubAgentCommandSummaryMax, summary)
	}
	if strings.Contains(summary, "server/tool-00") || strings.Contains(summary, "server/tool-01") || !strings.Contains(summary, "server/tool-11") {
		t.Fatalf("dynamic tool summary should keep latest capped entries, got %q", summary)
	}
	if !strings.Contains(summary, "还有 2 条动态工具记录未展开") {
		t.Fatalf("expected remaining dynamic tool count, got %q", summary)
	}
}
func TestAppendSubAgentDynamicToolSummaryKeepsLateFailures(t *testing.T) {
	var tools []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentCommandSummaryMax+2; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("server/tool-%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}
	tools[len(tools)-1].Succeeded = false
	tools[len(tools)-1].Summary = "late MCP failure"

	summary := appendSubAgentDynamicToolSummary("完成", tools)
	if !strings.Contains(summary, "FAIL: `call_mcp_tool` `server/tool-11`") || !strings.Contains(summary, "late MCP failure") {
		t.Fatalf("late failed dynamic tool should remain visible, got %q", summary)
	}
	if strings.Count(summary, "- PASS: `")+strings.Count(summary, "- FAIL: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestAppendSubAgentDynamicToolSummaryPrefersLatestProblems(t *testing.T) {
	var tools []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentCommandSummaryMax+3; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("server/tool-%02d", i),
			Succeeded: false,
			Summary:   fmt.Sprintf("failure %02d", i),
		})
	}

	summary := appendSubAgentDynamicToolSummary("完成", tools)
	if strings.Contains(summary, "server/tool-00") || strings.Contains(summary, "server/tool-01") || strings.Contains(summary, "server/tool-02") {
		t.Fatalf("dynamic tool summary should omit oldest failures when capped, got %q", summary)
	}
	if !strings.Contains(summary, "server/tool-12") || !strings.Contains(summary, "failure 12") {
		t.Fatalf("dynamic tool summary should keep latest failures, got %q", summary)
	}
}

func TestAppendSubAgentDynamicToolSummaryKeepsEmptySuccess(t *testing.T) {
	var tools []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentCommandSummaryMax+2; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("server/tool-%02d", i),
			Succeeded: true,
			Summary:   "ok",
		})
	}
	tools[0].Summary = "(无输出)"

	summary := appendSubAgentDynamicToolSummary("完成", tools)
	if !strings.Contains(summary, "EMPTY: `call_mcp_tool` `server/tool-00`") {
		t.Fatalf("empty successful dynamic tool should remain visible, got %q", summary)
	}
	if strings.Count(summary, "- PASS: `")+strings.Count(summary, "- EMPTY: `") != codingSubAgentCommandSummaryMax {
		t.Fatalf("summary should still be capped at %d entries, got %q", codingSubAgentCommandSummaryMax, summary)
	}
}

func TestSummarizeSubAgentCommands(t *testing.T) {
	status, summary := summarizeSubAgentCommands(nil)
	if status != "none" || summary != "no bash commands run" {
		t.Fatalf("empty command summary = %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
		{Command: "npm test", Succeeded: true, Summary: "2 tests passed"},
	})
	if status != "passed" || !strings.Contains(summary, "2 bash commands run, no failures") {
		t.Fatalf("passed command summary = %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
		{Command: "npm test", Succeeded: false, Summary: "jest failed\nfull output"},
	})
	if status != "failed" || !strings.Contains(summary, "1 failed: npm test: jest failed") {
		t.Fatalf("failed command summary = %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: `cmake --build D:\test\build --config Debug`, Succeeded: false, Summary: "PowerShell compatibility failed", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 2},
	})
	if status != "passed" || !strings.Contains(summary, "2 bash commands run, no failures") || strings.Contains(summary, "PowerShell compatibility failed") {
		t.Fatalf("resolved failed command should not make command progress summary fail, got %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: `g++ --version 2>&1; echo "---"; cmake --version 2>&1`, Succeeded: false, Summary: "PowerShell exception: 无法将“g++”项识别为 cmdlet", seq: 1},
		{Command: `cl.exe 2>&1; where cl.exe 2>&1`, Succeeded: false, Summary: "PowerShell exception: 无法将“cl.exe”项识别为 cmdlet", seq: 2},
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 3},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 4},
	})
	if status != "passed" || strings.Contains(summary, "g++") || strings.Contains(summary, "cl.exe") {
		t.Fatalf("resolved environment probe failures should not make command progress summary fail, got %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s"},
	})
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "1 empty success") || !strings.Contains(summary, "pytest tests: no tests collected") {
		t.Fatalf("empty verification success should be surfaced in command summary, got %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "npm test", Succeeded: false, Summary: "jest failed"},
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s"},
	})
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "1 failed, 1 empty success") || !strings.Contains(summary, "npm test: jest failed") || !strings.Contains(summary, "pytest tests: no tests collected") {
		t.Fatalf("mixed failed and empty command summary = %q, %q", status, summary)
	}
}

func TestCompactFailedVerificationCommandResultsDedupesRepeatedFailures(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./gui", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "ordinary prelude\ncompile failed: missing symbol", seq: 1},
		{Command: "go   test ./gui", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "compile failed: missing symbol", seq: 2},
		{Command: "go test ./gui", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "compile failed: different package", seq: 3},
		{Command: "go test ./gui", WorkingDir: filepath.Join("repo", "api"), Succeeded: false, Summary: "compile failed: missing symbol", seq: 4},
	}

	summary := compactFailedVerificationCommandResults(commands)
	if strings.Count(summary, "go   test ./gui: compile failed: missing symbol") != 1 {
		t.Fatalf("repeated same-command same-dir same-diagnostic failure should appear once, got %q", summary)
	}
	if strings.Contains(summary, "ordinary prelude") || strings.Contains(summary, "go test ./gui: compile failed: missing symbol; go   test ./gui") {
		t.Fatalf("older duplicate failure should be omitted, got %q", summary)
	}
	if !strings.Contains(summary, "go test ./gui: compile failed: different package") {
		t.Fatalf("different diagnostic should remain visible, got %q", summary)
	}
	if strings.Count(summary, "compile failed: missing symbol") != 2 {
		t.Fatalf("same diagnostic from a different working dir should remain visible, got %q", summary)
	}
}

func TestCompactFailedVerificationCommandResultsPrefersDiagnosticsOverExitStatus(t *testing.T) {
	commands := make([]CodingSubAgentCommandResult, 0, codingSubAgentFailedVerificationSummaryMax+2)
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: false,
			Summary:   "exit status 1",
			seq:       uint64(i + 1),
		})
	}
	commands[0].Summary = "panic: nil pointer dereference\nexit status 2"

	summary := compactFailedVerificationCommandResults(commands)
	if !strings.Contains(summary, "go test ./pkg/00: panic: nil pointer dereference") {
		t.Fatalf("old actionable diagnostic should outrank newer exit-status-only failures, got %q", summary)
	}
	if strings.Contains(summary, "go test ./pkg/01") || strings.Contains(summary, "go test ./pkg/02") {
		t.Fatalf("summary should omit older exit-status-only failures when capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 2 条未通过命令未展开") {
		t.Fatalf("summary should report omitted command failures, got %q", summary)
	}
}

func TestCompactFailedSubAgentDynamicToolResultsPrefersActionableLateFailure(t *testing.T) {
	tools := make([]CodingSubAgentDynamicToolResult, 0, codingSubAgentFailedVerificationSummaryMax+2)
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("browser/tool-%02d", i),
			Succeeded: false,
			Summary:   "ordinary prelude only",
		})
	}
	tools[len(tools)-1].Summary = "ordinary prelude\n[stderr] MCP call failed: browser closed"

	summary := compactFailedSubAgentDynamicToolResults(tools)
	if !strings.Contains(summary, "browser/tool-06 -> MCP call failed: browser closed") {
		t.Fatalf("late actionable MCP failure should remain visible, got %q", summary)
	}
	if strings.Contains(summary, "browser/tool-00") || strings.Contains(summary, "browser/tool-01") {
		t.Fatalf("summary should omit oldest low-signal failures when capped, got %q", summary)
	}
	if !strings.Contains(summary, "... 2 more") {
		t.Fatalf("summary should report omitted dynamic failures, got %q", summary)
	}
}

func TestCompactFailedSubAgentDynamicToolResultsPrefersDiagnosticsOverExitStatus(t *testing.T) {
	tools := make([]CodingSubAgentDynamicToolResult, 0, codingSubAgentFailedVerificationSummaryMax+2)
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		tools = append(tools, CodingSubAgentDynamicToolResult{
			Tool:      "call_mcp_tool",
			Name:      fmt.Sprintf("browser/tool-%02d", i),
			Succeeded: false,
			Summary:   "Error: process exited with code 1.",
		})
	}
	tools[0].Summary = "ordinary prelude\nTypeError: cannot read property id"

	summary := compactFailedSubAgentDynamicToolResults(tools)
	if !strings.Contains(summary, "browser/tool-00 -> TypeError: cannot read property id") {
		t.Fatalf("old actionable dynamic tool diagnostic should outrank newer exit-status-only failures, got %q", summary)
	}
	if strings.Contains(summary, "browser/tool-01") || strings.Contains(summary, "browser/tool-02") {
		t.Fatalf("summary should omit older exit-status-only dynamic tool failures when capped, got %q", summary)
	}
	if !strings.Contains(summary, "... 2 more") {
		t.Fatalf("summary should report omitted dynamic failures, got %q", summary)
	}
}

func TestCompactFailedSubAgentDynamicToolResultsDedupesRepeatedFailures(t *testing.T) {
	tools := []CodingSubAgentDynamicToolResult{
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "ordinary prelude\n[stderr] MCP call failed: browser closed"},
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "MCP call failed: browser closed"},
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "MCP call failed: timeout"},
		{Tool: "call_mcp_tool", Name: "project/search", Succeeded: false, Summary: "MCP call failed: browser closed"},
	}

	summary := compactFailedSubAgentDynamicToolResults(tools)
	if strings.Count(summary, "call_mcp_tool browser/screenshot -> MCP call failed: browser closed") != 1 {
		t.Fatalf("repeated same-target same-diagnostic failure should appear once, got %q", summary)
	}
	if !strings.Contains(summary, "call_mcp_tool browser/screenshot -> MCP call failed: timeout") {
		t.Fatalf("different diagnostic for same target should remain visible, got %q", summary)
	}
	if !strings.Contains(summary, "call_mcp_tool project/search -> MCP call failed: browser closed") {
		t.Fatalf("same diagnostic for different target should remain visible, got %q", summary)
	}
}

func TestSummarizeSubAgentQuality(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality("not_needed", "not_needed", false, nil, nil, nil, 0, nil, nil)
	if status != "passed" || count != 0 || !strings.Contains(summary, "no file changes") {
		t.Fatalf("empty quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 0, nil, nil)
	if status != "passed" || count != 0 || !strings.Contains(summary, "passed") {
		t.Fatalf("passed quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("missing", "missing", false, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "npm test", Succeeded: false, Summary: "jest failed\\nfull output"}}, 0, nil, nil)
	if status != "failed" || count != 4 || !strings.Contains(summary, "1 command(s) failed: npm test -> jest failed") || !strings.Contains(summary, "verification not run") || !strings.Contains(summary, "no exploration before existing-file edits") {
		t.Fatalf("failed quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: false, Summary: "command exited with code 1"},
		{Command: "npm test", Succeeded: false, Summary: "ordinary prelude\n[stderr] jest failed: expected true"},
	}, 0, nil, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "2 command(s) failed: npm test -> jest failed: expected true") || strings.Contains(summary, "go test ./...") || strings.Contains(summary, "ordinary prelude") {
		t.Fatalf("unresolved failed commands should fail quality and prefer actionable diagnostics, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./old", Succeeded: false, Summary: "old compile error", seq: 2},
		{Command: "go test ./new", Succeeded: false, Summary: "new compile error", seq: 3},
	}, 1, nil, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "2 command(s) failed: go test ./new -> new compile error") || strings.Contains(summary, "go test ./old") {
		t.Fatalf("failed command summary should prefer latest actionable diagnostic, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./old", Succeeded: false, Summary: "command exited with code 1", seq: 2},
		{Command: "go test ./new", Succeeded: false, Summary: "command exited with code 1", seq: 3},
	}, 1, nil, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "2 command(s) failed: go test ./new -> command exited with code 1") || strings.Contains(summary, "go test ./old") {
		t.Fatalf("failed command summary should fall back to latest failure, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "npm test", Succeeded: false, Summary: "jest failed", seq: 2},
		{Command: "npm   test", Succeeded: true, Summary: "ok", seq: 3},
	}, 1, nil, nil)
	if status != "passed" || count != 0 || strings.Contains(summary, "command(s) failed") {
		t.Fatalf("failed command resolved by a later equivalent success should pass, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: false, Summary: "failed", seq: 2},
		{Command: "pytest   tests", Succeeded: true, Summary: "no tests collected in 0.01s", seq: 3},
	}, 1, nil, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "pytest   tests -> no tests collected") || strings.Contains(summary, "passed") {
		t.Fatalf("empty verification success should not resolve earlier command failure, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("missing", "passed", true, []string{"new.go"}, []string{"new.go"}, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 0, nil, nil)
	if status != "passed" || count != 0 || !strings.Contains(summary, "passed") {
		t.Fatalf("created-only quality summary should not require exploration, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: false, seq: 1},
		{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2, nil, nil)
	if status != "passed" || count != 0 || strings.Contains(summary, "command(s) failed") {
		t.Fatalf("stale pre-edit command failure should not warn after fresh pass, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "failed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: false, Summary: "compile failed"}}, 0, []CodingSubAgentGuardrailViolation{{Tool: "bash"}}, nil)
	if status != "failed" || count != 2 || !strings.Contains(summary, "guardrail") || !strings.Contains(summary, "verification failed: 1 command(s) failed: go test ./... -> compile failed") {
		t.Fatalf("failed quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "Set-Content src\\a.go x", Succeeded: false, Summary: "blocked"}}, 0, []CodingSubAgentGuardrailViolation{{Tool: "bash", Command: "Set-Content src\\a.go x"}}, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") || strings.Contains(summary, "command(s) failed") {
		t.Fatalf("guardrail-blocked command should not be duplicated as command failure, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, nil, nil, []CodingSubAgentCommandResult{
		{Command: `cd D:\test\build && cmake --build D:\test\build --config Debug`, Succeeded: false, Summary: "blocked", seq: 2},
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 4},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 5},
	}, 0, []CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryCommand,
		Command:  `cd D:\test\build && cmake --build D:\test\build --config Debug`,
		Summary:  "PowerShell command compatibility: uses bash-only syntax such as `&&`.",
		seq:      3,
	}}, nil)
	if status != "passed" || count != 0 || strings.Contains(summary, "guardrail") || strings.Contains(summary, "command(s) failed") {
		t.Fatalf("resolved PowerShell compatibility guardrail should not fail no-change quality, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, nil, nil, []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 4},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 5},
	}, 0, []CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryCommand,
		Command:  `& "C:\Program Files\Microsoft Visual Studio\18\Community\Common7\Tools\Launch-VsDevShell.ps1" -Arch amd64 -HostArch amd64 2>&1; Get-Command cl.exe`,
		Summary:  `PowerShell exception: 无法加载文件 C:\Program Files\Microsoft Visual Studio\18\Community\Common7\Tools\Launch-VsDevShell.ps1，因为在此系统上禁止运行脚本。about_Execution_Policies`,
		seq:      3,
	}}, nil)
	if status != "passed" || count != 0 || strings.Contains(summary, "guardrail") {
		t.Fatalf("PowerShell execution-policy setup guardrail should be resolved by later successful verification, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, nil, nil, []CodingSubAgentCommandResult{
		{Command: "Get-ChildItem .", Succeeded: true, Summary: "CMakeLists.txt", seq: 4},
	}, 0, []CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryCommand,
		Command:  `& "C:\Program Files\Microsoft Visual Studio\18\Community\Common7\Tools\Launch-VsDevShell.ps1" -Arch amd64 -HostArch amd64`,
		Summary:  `PowerShell exception: running scripts is disabled on this system. about_Execution_Policies`,
		seq:      3,
	}}, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") {
		t.Fatalf("PowerShell setup guardrail should require later verification success, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, nil, nil, []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 2},
	}, 0, []CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryCommand,
		Command:  `cmake --build D:\test\build --config Debug`,
		Summary:  "PowerShell command compatibility: uses bash-only syntax such as `&&`.",
		seq:      3,
	}}, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") {
		t.Fatalf("earlier success should not resolve later guardrail, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, nil, nil, []CodingSubAgentCommandResult{
		{Command: "git reset --hard HEAD", Succeeded: false, Summary: "blocked", seq: 2},
		{Command: "git status", Succeeded: true, Summary: "clean", seq: 3},
	}, 0, []CodingSubAgentGuardrailViolation{{
		Tool:     "bash",
		Category: codingSubAgentGuardrailCategoryGit,
		Command:  "git reset --hard HEAD",
		Summary:  "blocked destructive git command",
		seq:      2,
	}}, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") {
		t.Fatalf("destructive guardrail should remain blocking, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "Git   reset --hard HEAD", Succeeded: false, Summary: "blocked"}}, 0, []CodingSubAgentGuardrailViolation{{Tool: "bash", Command: "git reset --hard head"}}, nil)
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") || strings.Contains(summary, "command(s) failed") {
		t.Fatalf("equivalent guardrail-blocked command should not be duplicated as command failure, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "npm test", Succeeded: false, Summary: "ordinary prelude\n[stderr] jest failed"}}, 0, []CodingSubAgentGuardrailViolation{{Tool: "bash", Command: "Set-Content src\\a.go x"}}, nil)
	if status != "failed" || count != 2 || !strings.Contains(summary, "guardrail") || !strings.Contains(summary, "npm test -> jest failed") || strings.Contains(summary, "ordinary prelude") {
		t.Fatalf("unrelated failed command should still be reported with guardrail, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, nil, 0, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "ordinary prelude\n[stderr] MCP call failed: browser closed"}})
	if status != "failed" || count != 1 || !strings.Contains(summary, "1 dynamic tool failed") || !strings.Contains(summary, "call_mcp_tool browser/screenshot -> MCP call failed") || strings.Contains(summary, "ordinary prelude") {
		t.Fatalf("unresolved failed dynamic tool should fail quality summary, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, nil, 1, nil, []CodingSubAgentDynamicToolResult{
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "browser closed", seq: 2},
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: true, Summary: "ok", seq: 3},
	})
	if status != "passed" || count != 0 || strings.Contains(summary, "dynamic tool failed") {
		t.Fatalf("failed dynamic tool resolved by later same-target success should pass, got %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, nil, 2, nil, []CodingSubAgentDynamicToolResult{
		{Tool: "manage_skill", Name: "impeccable", Succeeded: false, Summary: "failed before edit", seq: 1},
		{Tool: "manage_skill", Name: "impeccable", Succeeded: true, Summary: "ok", seq: 3},
	})
	if status != "passed" || count != 0 || strings.Contains(summary, "dynamic tool failed") {
		t.Fatalf("stale pre-edit dynamic failure should not warn after fresh success, got %q, %q, %d", status, summary, count)
	}
	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, nil, 1, nil, []CodingSubAgentDynamicToolResult{
		{Tool: "manage_skill", Succeeded: false, Summary: "missing required argument name", seq: 2},
		{Tool: "manage_skill", Name: "impeccable", Succeeded: true, Summary: "ok", seq: 3},
	})
	if status != "failed" || count != 1 || !strings.Contains(summary, "dynamic tool failed") || !strings.Contains(summary, "missing required argument name") {
		t.Fatalf("target-less dynamic argument failure should not be resolved by later same-tool success, got %q, %q, %d", status, summary, count)
	}
}

func TestSummarizeSubAgentNoChangeEvidence(t *testing.T) {
	warning := summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, nil)
	if warning == "" || !strings.Contains(warning, "no file changes") {
		t.Fatalf("empty no-change task should warn about missing evidence, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, []string{"src/a.go"}, nil, nil, nil)
	if warning != "" {
		t.Fatalf("read evidence should satisfy no-change task, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, []CodingSubAgentSearchResult{{Tool: "codegraph", Query: "codegraph explore handler", Succeeded: true, Summary: "Found 3 symbols in handlers.go"}}, nil, nil)
	if warning != "" {
		t.Fatalf("successful search evidence should satisfy no-change task, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, []CodingSubAgentSearchResult{{Tool: "codegraph", Query: "missing handler", Succeeded: true, Summary: "No results found"}}, nil, nil)
	if warning == "" {
		t.Fatalf("empty-result search evidence should not satisfy no-change task")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, []CodingSubAgentSearchResult{{Tool: "list_directory", Path: "src", Succeeded: true, Summary: "README.md\nmain.go"}}, nil, nil)
	if warning != "" {
		t.Fatalf("successful list_directory evidence should satisfy no-change task, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, []CodingSubAgentSearchResult{{Tool: "list_directory", Path: "src", Succeeded: false}}, nil, nil)
	if warning == "" {
		t.Fatalf("failed list_directory evidence should not satisfy no-change task")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, nil)
	if warning != "" {
		t.Fatalf("successful verification evidence should satisfy no-change task, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: true, Summary: "captured screenshot"}})
	if warning != "" {
		t.Fatalf("successful inspection dynamic tool should satisfy no-change task, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: true, Summary: "(无输出)"}})
	if warning == "" {
		t.Fatalf("empty-output MCP inspection should not satisfy no-change inspection evidence")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "knowledge_search", Succeeded: true, Summary: "No results found for query: settings handler"}})
	if warning == "" {
		t.Fatalf("empty-result knowledge search should not satisfy no-change inspection evidence")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "knowledge_search", Succeeded: true, Summary: "Found 2 results: settings handler, config store"}})
	if warning != "" {
		t.Fatalf("non-empty knowledge search should satisfy no-change inspection evidence, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Succeeded: true}})
	if warning == "" {
		t.Fatalf("unnamed MCP tool call should not satisfy no-change inspection evidence")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "github/create_issue", Succeeded: true}})
	if warning == "" {
		t.Fatalf("mutating MCP tool call should not satisfy no-change inspection evidence")
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "github/createIssue", Succeeded: true, Summary: "created issue"}})
	if warning == "" {
		t.Fatalf("camelCase mutating MCP tool call should not satisfy no-change inspection evidence")
	}

	for _, name := range []string{
		"files/renameFile",
		"cms/publish_page",
		"server/restart-server",
		"process/startServer",
		"jobs/stopJob",
		"comments/addComment",
		"records/upsert_record",
		"threads/archiveThread",
		"requests/approve-request",
		"flags/disableFeature",
	} {
		warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: name, Succeeded: true, Summary: "ok"}})
		if warning == "" {
			t.Fatalf("mutating MCP tool call %q should not satisfy no-change inspection evidence", name)
		}
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "contacts/addressLookup", Succeeded: true, Summary: "found address"}})
	if warning != "" {
		t.Fatalf("read-only MCP tool with name starting address should satisfy no-change inspection evidence, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "runtime/info", Succeeded: true, Summary: "runtime status"}})
	if warning != "" {
		t.Fatalf("read-only MCP tool with name starting runtime should satisfy no-change inspection evidence, got %q", warning)
	}

	warning = summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "manage_skill", Name: "impeccable", Succeeded: true}})
	if warning == "" {
		t.Fatalf("manage_skill alone should not satisfy no-change inspection evidence")
	}

	warning = summarizeSubAgentNoChangeEvidence([]string{"src/a.go"}, nil, nil, nil, nil, nil)
	if warning != "" {
		t.Fatalf("changed task should not use no-change evidence warning, got %q", warning)
	}
}

func TestNoChangeWithoutEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityNotNeeded, codingSubAgentQualityNotNeeded, false, nil, nil, nil, 0, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentNoChangeEvidence(nil, nil, nil, nil, nil, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "no file changes and no inspection or verification evidence") {
		t.Fatalf("empty no-change task should become a quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestExistingSubAgentModifiedFilesNormalizesPathSeparators(t *testing.T) {
	existing := existingSubAgentModifiedFiles([]string{"src/new_handler.go", "src/existing_handler.go"}, []string{"src\\new_handler.go"})
	if len(existing) != 1 || existing[0] != "src/existing_handler.go" {
		t.Fatalf("expected only existing file after slash-normalized created-file comparison, got %#v", existing)
	}
}

func TestSummarizeSubAgentCreatedFileContextEvidence(t *testing.T) {
	warning := summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, nil)
	if warning == "" || !strings.Contains(warning, "created files") || !strings.Contains(warning, "project-context evidence") {
		t.Fatalf("created files without inspection evidence should warn, got %q", warning)
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, []string{"src/new_handler.go"}, nil, nil)
	if warning == "" {
		t.Fatalf("reading only the created file should not satisfy project-context evidence")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, []string{"src\\new_handler.go"}, nil, nil)
	if warning == "" {
		t.Fatalf("reading only the created file with different path separators should not satisfy project-context evidence")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, []string{"src/existing_handler.go"}, nil, nil)
	if warning != "" {
		t.Fatalf("read evidence from an existing file should satisfy created-file context, got %q", warning)
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, []CodingSubAgentSearchResult{{Tool: "codegraph", Query: "codegraph explore handlers", Succeeded: true, Summary: "Found handler symbols in src/existing_handler.go"}}, nil)
	if warning != "" {
		t.Fatalf("successful search should satisfy created-file context, got %q", warning)
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, []CodingSubAgentSearchResult{{Tool: "ripgrep", Query: "NewHandler", Succeeded: true, Summary: "0 results"}}, nil)
	if warning == "" {
		t.Fatalf("empty-result search should not satisfy created-file context")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "project_docs/search", Succeeded: true, Summary: "found related handlers"}})
	if warning != "" {
		t.Fatalf("inspection dynamic tool should satisfy created-file context, got %q", warning)
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "coding_knowledge_search", Succeeded: true, Summary: "0 results"}})
	if warning == "" {
		t.Fatalf("empty-result knowledge search should not satisfy created-file context")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "repo/write_file", Succeeded: true}})
	if warning == "" {
		t.Fatalf("mutating MCP tool call should not satisfy created-file context")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, []CodingSubAgentDynamicToolResult{{Tool: "call_mcp_tool", Name: "repo/writeFile", Succeeded: true, Summary: "wrote file"}})
	if warning == "" {
		t.Fatalf("camelCase mutating MCP tool call should not satisfy created-file context")
	}

	warning = summarizeSubAgentCreatedFileContextEvidence(nil, nil, nil, nil)
	if warning != "" {
		t.Fatalf("no created files should not warn, got %q", warning)
	}
}

func TestCreatedFileContextEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityNotNeeded, codingSubAgentQualityPassed, true, []string{"src/new_handler.go"}, []string{"src/new_handler.go"}, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true, Summary: "ok", seq: 1}}, 0, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentCreatedFileContextEvidence([]string{"src/new_handler.go"}, nil, nil, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "created files without inspection") {
		t.Fatalf("created-file context gap should become a quality failure, got (%q, %q, %d)", status, summary, count)
	}
}
func TestCodingSubAgentListDirectoryRecordsSearchEvidence(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	result := cb.executeToolWithOutcome("list_directory", `{"path":"."}`)
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("list_directory outcome = %s, result = %q", result.Outcome, result.Text)
	}
	searches := cb.getSearchesRun()
	if len(searches) != 1 || searches[0].Tool != "list_directory" || !searches[0].Succeeded {
		t.Fatalf("list_directory should record successful search evidence, got %#v", searches)
	}
	if warning := summarizeSubAgentNoChangeEvidence(nil, nil, nil, searches, nil, nil); warning != "" {
		t.Fatalf("recorded list_directory evidence should satisfy no-change task, got %q", warning)
	}

	failed := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	result = failed.executeToolWithOutcome("list_directory", `{"path":"missing"}`)
	if result.Outcome == codingToolOutcomeSuccess {
		t.Fatalf("missing list_directory unexpectedly succeeded: %q", result.Text)
	}
	searches = failed.getSearchesRun()
	if len(searches) != 1 || searches[0].Tool != "list_directory" || searches[0].Succeeded {
		t.Fatalf("failed list_directory should record failed search evidence, got %#v", searches)
	}
	if warning := summarizeSubAgentNoChangeEvidence(nil, nil, nil, searches, nil, nil); warning == "" {
		t.Fatalf("failed list_directory evidence should not satisfy no-change task")
	}
}

func TestSummarizeSubAgentAcceptanceCriteriaEvidence(t *testing.T) {
	task := &TaskItem{AcceptanceCriteria: []string{"save button persists the setting"}}

	warning := summarizeSubAgentAcceptanceCriteriaEvidence(task, "Changed the settings handler. Verification: go test ./gui", []string{"settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "acceptance criteria") {
		t.Fatalf("expected missing acceptance evidence warning, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(task, "Acceptance criteria verified with npm test.", []string{"settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "each listed criterion") {
		t.Fatalf("generic acceptance verification should not pass without criterion evidence, got %q", warning)
	}

	tokenBoundaryTask := &TaskItem{AcceptanceCriteria: []string{"auth token expires", "save setting"}}
	warning = summarizeSubAgentAcceptanceCriteriaEvidence(tokenBoundaryTask, "Acceptance criteria verified with npm test: authorization tokenization covered and autosave settings checked.", []string{"settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "each listed criterion") {
		t.Fatalf("acceptance criterion token substrings should not satisfy criteria, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(tokenBoundaryTask, "Acceptance criteria verified with npm test: auth token expires and save setting.", []string{"settings.go"}, nil)
	if warning != "" {
		t.Fatalf("acceptance criterion token boundaries should satisfy criteria, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(task, "Acceptance criteria verified with npm test: save button persists the setting.", []string{"settings.go"}, nil)
	if warning != "" {
		t.Fatalf("English acceptance verification summary should pass when it references the criterion, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(task, "Acceptance criteria AC1 verified with npm test.", []string{"settings.go"}, nil)
	if warning != "" {
		t.Fatalf("acceptance criterion index reference should pass, got %q", warning)
	}

	multiTask := &TaskItem{AcceptanceCriteria: []string{
		"save button persists the setting",
		"cancel button restores the previous setting",
	}}
	warning = summarizeSubAgentAcceptanceCriteriaEvidence(multiTask, "Acceptance criteria AC1 verified with npm test.", []string{"settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "each listed criterion") {
		t.Fatalf("partial acceptance criterion references should warn, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(multiTask, "Acceptance criteria AC1 and AC2 verified with npm test.", []string{"settings.go"}, nil)
	if warning != "" {
		t.Fatalf("all acceptance criterion index references should pass, got %q", warning)
	}

	for _, summary := range []string{
		"Acceptance criteria AC-1 and AC #2 verified with npm test.",
		"Acceptance criterion #1 and criterion #2 verified with npm test.",
		"Acceptance criteria (1) and (2) verified with npm test.",
		"验收验证：第 1 条和第 2 条已通过 go test ./gui。",
		"验收验证：验收第一条和验收第二条已通过 go test ./gui。",
		"标准验证：标准第1条和标准第2条均已通过 go test ./gui。",
	} {
		warning = summarizeSubAgentAcceptanceCriteriaEvidence(multiTask, summary, []string{"settings.go"}, nil)
		if warning != "" {
			t.Fatalf("acceptance criterion numbered references should pass for %q, got %q", summary, warning)
		}
	}

	for _, summary := range []string{
		"Acceptance criteria AC10 and AC20 verified with npm test.",
		"Acceptance criterion #10 and criterion #20 verified with npm test.",
		"Acceptance criteria AC-10 and AC-20 verified with npm test.",
	} {
		warning = summarizeSubAgentAcceptanceCriteriaEvidence(multiTask, summary, []string{"settings.go"}, nil)
		if warning == "" || !strings.Contains(warning, "each listed criterion") {
			t.Fatalf("acceptance criterion index prefixes should not satisfy AC1/AC2 for %q, got %q", summary, warning)
		}
	}

	zhTask := &TaskItem{AcceptanceCriteria: []string{"保存按钮持久化设置"}}
	warning = summarizeSubAgentAcceptanceCriteriaEvidence(zhTask, "验收验证：go test ./gui 覆盖保存按钮持久化设置。", []string{"settings.go"}, nil)
	if warning != "" {
		t.Fatalf("Chinese acceptance verification summary should pass, got %q", warning)
	}

	warning = summarizeSubAgentAcceptanceCriteriaEvidence(task, "No changes needed.", nil, nil)
	if warning != "" {
		t.Fatalf("unchanged task should not require acceptance evidence, got %q", warning)
	}
}

func TestAcceptanceCriteriaEvidenceAddsQualityFailure(t *testing.T) {
	task := &TaskItem{AcceptanceCriteria: []string{
		"save button persists the setting",
		"cancel button restores the previous setting",
	}}
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 1, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentAcceptanceCriteriaEvidence(task, "Acceptance criteria AC1 verified with go test ./gui.", []string{"settings.go"}, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "acceptance criteria verification does not reference each listed criterion") {
		t.Fatalf("partial acceptance evidence should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestSummarizeSubAgentScopeEvidence(t *testing.T) {
	task := &TaskItem{Files: []string{"src/settings.go", "tests"}}

	warning := summarizeSubAgentScopeEvidence(task, "Changed src/settings.go and ran go test.", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("planned file change should not warn, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Updated tests for settings behavior.", []string{"tests/settings_test.go"}, nil)
	if warning != "" {
		t.Fatalf("planned directory change should not warn, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Changed settings behavior and ran go test.", []string{"src/settings.go", "src/router.go"}, nil)
	if warning == "" || !strings.Contains(warning, "outside listed task scope") || !strings.Contains(warning, "src/router.go") {
		t.Fatalf("unexplained out-of-scope change should warn, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Scope note: src/router.go had to be updated because the setting route is registered there.", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("summary scope rationale should satisfy out-of-scope change, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Scope unchanged; settings behavior updated and tests passed.", []string{"src/router.go"}, nil)
	if warning == "" || !strings.Contains(warning, "outside listed task scope") || !strings.Contains(warning, "src/router.go") {
		t.Fatalf("generic scope mention should not satisfy out-of-scope change, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Scope expansion: updated route registration helper required by the settings change.", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("explicit scope expansion rationale should satisfy out-of-scope change, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "范围扩展：设置路由注册逻辑需要同步调整。", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("Chinese explicit scope expansion rationale should satisfy out-of-scope change, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Also changed src/router.go for route registration.", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning out-of-scope file should satisfy scope evidence, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, `Also changed src\router.go for route registration.`, []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning out-of-scope file with backslashes should satisfy scope evidence, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Mentioned src/router.go.bak in the summary.", []string{"src/router.go"}, nil)
	if warning == "" || !strings.Contains(warning, "outside listed task scope") || !strings.Contains(warning, "src/router.go") {
		t.Fatalf("path prefix mention should not satisfy out-of-scope file evidence, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(task, "Also changed ./src/router.go for route registration.", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning out-of-scope file with ./ prefix should satisfy scope evidence, got %q", warning)
	}

	warning = summarizeSubAgentScopeEvidence(&TaskItem{}, "Changed src/router.go.", []string{"src/router.go"}, nil)
	if warning != "" {
		t.Fatalf("task without planned files should not warn, got %q", warning)
	}
}

func TestScopeEvidenceAddsQualityFailure(t *testing.T) {
	task := &TaskItem{Files: []string{"src/settings.go"}}
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"src/settings.go", "src/router.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 1, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentScopeEvidence(task, "Changed settings behavior and ran go test.", []string{"src/settings.go", "src/router.go"}, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "outside listed task scope") || !strings.Contains(summary, "src/router.go") {
		t.Fatalf("unexplained out-of-scope change should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestSummarizeSubAgentChangedFileSummaryEvidence(t *testing.T) {
	warning := summarizeSubAgentChangedFileSummaryEvidence("Updated `src/settings.go` and ran tests.", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning changed file should not warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence(`Updated src\settings.go and ran tests.`, []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning changed file with backslashes should not warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("Updated src/settings.go.bak and ran tests.", []string{"src/settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "src/settings.go") {
		t.Fatalf("path prefix mention should not satisfy changed-file evidence, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("Updated ./src/settings.go and ran tests.", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("summary mentioning changed file with ./ prefix should not warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("Updated the settings handler and ran tests.", []string{"src/settings.go"}, []string{"tests/settings_test.go"})
	if warning == "" || !strings.Contains(warning, "changed files not referenced") || !strings.Contains(warning, "src/settings.go") || !strings.Contains(warning, "tests/settings_test.go") {
		t.Fatalf("summary missing changed file paths should warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("Updated `src/settings.go` and ran tests.", []string{"src/settings.go"}, []string{"tests/settings_test.go"})
	if warning == "" || !strings.Contains(warning, "changed files not referenced") || strings.Contains(warning, "src/settings.go") || !strings.Contains(warning, "tests/settings_test.go") {
		t.Fatalf("summary should require every changed file and list only missing paths, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("Updated `src/settings.go` and `tests/settings_test.go`; ran tests.", []string{"src/settings.go"}, []string{"tests/settings_test.go"})
	if warning != "" {
		t.Fatalf("summary mentioning every changed file should not warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("empty model summary should rely on fallback summary and not warn, got %q", warning)
	}

	warning = summarizeSubAgentChangedFileSummaryEvidence("No changes needed.", nil, nil)
	if warning != "" {
		t.Fatalf("no changed files should not warn, got %q", warning)
	}
}

func TestChangedFileSummaryEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 1, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentChangedFileSummaryEvidence("Updated the settings handler and ran tests.", []string{"src/settings.go"}, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "changed files not referenced") || !strings.Contains(summary, "src/settings.go") {
		t.Fatalf("missing changed-file summary should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestSummarizeSubAgentRiskSummaryEvidence(t *testing.T) {
	warning := summarizeSubAgentRiskSummaryEvidence("Updated src/settings.go. Risk: no known remaining risk after go test.", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("summary with risk note should not warn, got %q", warning)
	}

	warning = summarizeSubAgentRiskSummaryEvidence("更新 src/settings.go。剩余风险：未发现。", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("Chinese summary with risk note should not warn, got %q", warning)
	}

	for _, summary := range []string{
		"Updated src/settings.go. Risk: none.",
		"Updated src/settings.go. Residual risk: manual browser smoke test not covered.",
		"Updated src/settings.go. Known issue: upstream API may still reject malformed input.",
		"Updated src/settings.go. Manual verification required for the legacy browser path.",
		"更新 src/settings.go。风险：无。",
		"更新 src/settings.go。已知问题：旧浏览器路径需要人工验证。",
		"更新 src/settings.go。无法自动验证旧浏览器路径。",
	} {
		warning = summarizeSubAgentRiskSummaryEvidence(summary, []string{"src/settings.go"}, nil)
		if warning != "" {
			t.Fatalf("explicit risk summary should not warn for %q, got %q", summary, warning)
		}
	}

	warning = summarizeSubAgentRiskSummaryEvidence("Updated src/settings.go. Remaining work: none.", []string{"src/settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "remaining risk") {
		t.Fatalf("generic remaining-work note should not satisfy risk summary, got %q", warning)
	}

	warning = summarizeSubAgentRiskSummaryEvidence("Updated src/settings.go. No known regressions after go test.", []string{"src/settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "remaining risk") {
		t.Fatalf("generic no-regressions note should not satisfy remaining-risk summary, got %q", warning)
	}

	for _, summary := range []string{
		"Updated src/settings.go. The unblocker path was cleaned up.",
		"Updated src/settings.go. The manual tester helper was renamed.",
		"Updated src/settings.go. The brisk parser path was refactored.",
	} {
		warning = summarizeSubAgentRiskSummaryEvidence(summary, []string{"src/settings.go"}, nil)
		if warning == "" || !strings.Contains(warning, "remaining risk") {
			t.Fatalf("risk phrase substrings should not satisfy remaining-risk summary for %q, got %q", summary, warning)
		}
	}

	warning = summarizeSubAgentRiskSummaryEvidence("Updated src/settings.go and ran go test.", []string{"src/settings.go"}, nil)
	if warning == "" || !strings.Contains(warning, "remaining risk") {
		t.Fatalf("changed task summary without risk note should warn, got %q", warning)
	}

	warning = summarizeSubAgentRiskSummaryEvidence("", []string{"src/settings.go"}, nil)
	if warning != "" {
		t.Fatalf("empty model summary should rely on fallback summary and not warn, got %q", warning)
	}

	warning = summarizeSubAgentRiskSummaryEvidence("No changes needed.", nil, nil)
	if warning != "" {
		t.Fatalf("no changed files should not require risk note, got %q", warning)
	}
}

func TestRiskSummaryEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"}}, 1, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentRiskSummaryEvidence("Updated src/settings.go and ran go test.", []string{"src/settings.go"}, nil))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "remaining risk not called out") {
		t.Fatalf("missing remaining-risk summary should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestSummarizeSubAgentVerificationCommandSummaryEvidence(t *testing.T) {
	warning := summarizeSubAgentVerificationCommandSummaryEvidence("Updated src/settings.go and ran tests. Risk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning == "" || !strings.Contains(warning, "verification command not referenced") {
		t.Fatalf("changed task with fresh verification but no command in summary should warn, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary naming fresh verification command should not warn, got %q", warning)
	}

	for _, summary := range []string{
		"Verification: go test ./gui completed successfully.\nRisk: none.",
		"Verification: go test ./gui exit 0.\nRisk: none.",
		"Verification: go test ./gui all tests passed.\nRisk: none.",
		"Verification: go test ./gui green.\nRisk: none.",
		"Verification: go test ./gui, go vet ./gui passed.\nRisk: none.",
		"Verification: go test ./gui; go vet ./gui passed.\nRisk: none.",
		"Ran: go test ./gui passed.\nRisk: none.",
		"Test command: go test ./gui ok.\nRisk: none.",
		"Check command: go test ./gui succeeded.\nRisk: none.",
		"验证：go test ./gui 全部通过。\n风险：无。",
		"测试命令：go test ./gui 已通过。\n风险：无。",
		"检查：go test ./gui 执行成功。\n风险：无。",
		"已运行：go test ./gui，通过。\n风险：无。",
	} {
		warning = summarizeSubAgentVerificationCommandSummaryEvidence(summary, []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
			{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
		}, 2)
		if warning != "" {
			t.Fatalf("summary with natural successful verification outcome should not warn for %q, got %q", summary, warning)
		}
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui, go vet ./gui passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go vet ./gui", Succeeded: true, Summary: "go vet ./gui ok", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary listing multiple verification commands should match fresh later command, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui && go vet ./gui passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui && go vet ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary with compound verification command should match full audit command, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: `go test ./gui` and `go vet ./gui` passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui && go vet ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary naming all verification subcommands should match fresh compound audit command, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: `go test ./gui` and `go vet ./gui`.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui && go vet ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning == "" || !strings.Contains(warning, "verification command outcome not referenced") {
		t.Fatalf("summary naming compound verification subcommands without outcome should warn, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui; go vet ./gui failed.\nRisk: known vet failure.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go vet ./gui", Succeeded: false, Summary: "vet failed", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary listing multiple verification commands should match fresh failed later command, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning == "" || !strings.Contains(warning, "verification command outcome not referenced") {
		t.Fatalf("summary naming fresh verification command without outcome should warn, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui failed.\nRisk: known failing assertion.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("summary naming fresh failed verification command with outcome should not warn, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./old passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./old", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/old 0.1s", seq: 1},
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning == "" || !strings.Contains(warning, "fresh verification command not referenced") {
		t.Fatalf("summary naming only stale verification command should warn, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("No changes needed.", nil, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}, 2)
	if warning != "" {
		t.Fatalf("no changed files should not require verification command summary, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Updated src/settings.go. Risk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 1},
	}, 2)
	if warning != "" {
		t.Fatalf("stale verification command should not require command summary, got %q", warning)
	}

	warning = summarizeSubAgentVerificationCommandSummaryEvidence("Verification: go test ./gui passed.\nRisk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true, Summary: "(无输出)", seq: 3},
	}, 2)
	if warning == "" || !strings.Contains(warning, "did not produce execution evidence") {
		t.Fatalf("fresh empty-output verification should not satisfy final summary evidence, got %q", warning)
	}
}

func TestVerificationCommandSummaryEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3}}, 2, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentVerificationCommandSummaryEvidence("Updated src/settings.go and ran tests. Risk: none.", []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3}}, 2))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "verification command not referenced") {
		t.Fatalf("missing verification command summary should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestSummarizeSubAgentClaimedVerificationEvidence(t *testing.T) {
	warning := summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("claimed verification command present in audit log should not warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui -run TestParser` passed.", []CodingSubAgentCommandResult{
		{Command: `go   test ./gui -run "TestParser"`, Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("equivalent quoted verification command should match audit log, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: `cmake --build D:\\test\\build --config Debug` passed.\nVerification: `ctest --test-dir D:\\test\\build -C Debug --output-on-failure` passed.", []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded"},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed"},
	})
	if warning != "" {
		t.Fatalf("PowerShell full-path cmake verification commands should match summary commands, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: D:\\test\nVerification: cmake --build D:\\test\\build --config Debug passed.", []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded"},
	})
	if warning != "" {
		t.Fatalf("Windows work directory line should not be treated as a claimed test command, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui -run TestParser` passed.", []CodingSubAgentCommandResult{
		{Command: `bash -lc "go test ./gui -run TestParser"`, Succeeded: true},
	})
	if warning == "" || !strings.Contains(warning, "claimed verification command not found") {
		t.Fatalf("wrapped verification command should not be treated as the same audit command, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./api", Succeeded: true},
	})
	if warning == "" || !strings.Contains(warning, "claimed verification command not found") || !strings.Contains(warning, "go test ./gui") {
		t.Fatalf("missing claimed verification command should warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Touched `src/settings.go` and ran tests.", nil)
	if warning != "" {
		t.Fatalf("non-command inline code should not warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("structured unquoted verification command present in audit log should not warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui && go vet ./gui passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui && go vet ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("structured compound verification command should match full audit log entry, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui; go vet ./gui passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui; go vet ./gui", Succeeded: true},
	})
	if warning == "" || !strings.Contains(warning, "claimed verification command not found") {
		t.Fatalf("semicolon compound verification should not count as reliable audit evidence, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification command: go test ./gui (passed)", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("structured verification command with parenthesized status should match audit log, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification command: go test ./gui (failed)", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if warning != "" {
		t.Fatalf("structured verification command with failed status should match audit log, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Tests: go test ./gui => passed", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("tests-prefixed verification command with arrow status should match audit log, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Tests: go test ./gui => failed", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if warning != "" {
		t.Fatalf("tests-prefixed verification command with failed arrow status should match audit log, got %q", warning)
	}

	failure := summarizeSubAgentClaimedVerificationFailureEvidence("Validated with: go test ./gui -> passed", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if failure == "" || !strings.Contains(failure, "claimed verification command passed but audit log recorded failure or empty success") || !strings.Contains(failure, "go test ./gui") {
		t.Fatalf("arrow-status structured command claimed passed but failed in audit log should fail quality, got %q", failure)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if warning != "" {
		t.Fatalf("structured command present in audit log should not warn as missing even when failed, got %q", warning)
	}
	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: go test ./gui passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if failure == "" || !strings.Contains(failure, "claimed verification command passed but audit log recorded failure or empty success") || !strings.Contains(failure, "go test ./gui") {
		t.Fatalf("structured command claimed passed but failed in audit log should fail quality, got %q", failure)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if strings.Contains(warning, "claimed verification command passed but audit log recorded failure") {
		t.Fatalf("unqualified structured command should not warn about claimed pass, got %q", warning)
	}
	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: go test ./gui", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if failure != "" {
		t.Fatalf("unqualified structured command should not fail claimed-pass evidence, got %q", failure)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: go test ./gui passed.", nil)
	if warning == "" || !strings.Contains(warning, "go test ./gui") {
		t.Fatalf("structured unquoted verification command missing from audit log should warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("验证命令：go test ./gui；通过。", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
	})
	if warning != "" {
		t.Fatalf("Chinese structured verification command present in audit log should not warn, got %q", warning)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("验证命令：go test ./gui；通过。", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if warning != "" {
		t.Fatalf("Chinese structured command present in audit log should not warn as missing even when failed, got %q", warning)
	}
	failure = summarizeSubAgentClaimedVerificationFailureEvidence("验证命令：go test ./gui；通过。", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if failure == "" || !strings.Contains(failure, "claimed verification command passed but audit log recorded failure or empty success") || !strings.Contains(failure, "go test ./gui") {
		t.Fatalf("Chinese structured command claimed passed but failed in audit log should fail quality, got %q", failure)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if warning != "" {
		t.Fatalf("inline command present in audit log should not warn as missing even when failed, got %q", warning)
	}
	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: `go test ./gui` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL"},
	})
	if failure == "" || !strings.Contains(failure, "claimed verification command passed but audit log recorded failure or empty success") || !strings.Contains(failure, "go test ./gui") {
		t.Fatalf("inline command claimed passed but failed in audit log should fail quality, got %q", failure)
	}

	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: `pytest tests` passed.", []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s"},
	})
	if failure == "" || !strings.Contains(failure, "empty success") || !strings.Contains(failure, "pytest tests") {
		t.Fatalf("claimed passed command with empty-success audit output should fail quality, got %q", failure)
	}

	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: `go test ./...` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "api"), Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/api 0.1s", seq: 2},
	})
	if failure == "" || !strings.Contains(failure, "go test ./...") {
		t.Fatalf("claimed pass should not be cleared by same command text in a different working dir, got %q", failure)
	}

	failure = summarizeSubAgentClaimedVerificationFailureEvidence("Verification: `go test ./...` passed.", []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "api"), Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/api 0.1s", seq: 2},
		{Command: "go   test ./...", WorkingDir: filepath.Join("repo", "gui"), Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	})
	if failure != "" {
		t.Fatalf("later equivalent real success in same working dir should clear claimed-pass failure, got %q", failure)
	}

	warning = summarizeSubAgentClaimedVerificationEvidence("I would run go test ./gui if more time were available.", nil)
	if warning != "" {
		t.Fatalf("unstructured unquoted command mention should be ignored, got %q", warning)
	}
}

func TestClaimedVerificationEvidenceAddsQualityFailure(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality(codingSubAgentQualityExplored, codingSubAgentQualityPassed, true, []string{"src/settings.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./api", Succeeded: true, Summary: "ok", seq: 1}}, 0, nil, nil)
	status, summary, count = appendSubAgentQualityFailure(status, summary, count, summarizeSubAgentClaimedVerificationEvidence("Verification: `go test ./gui` passed.", []CodingSubAgentCommandResult{{Command: "go test ./api", Succeeded: true, Summary: "ok", seq: 1}}))
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "claimed verification command not found") || !strings.Contains(summary, "go test ./gui") {
		t.Fatalf("claimed missing verification command should become quality failure, got (%q, %q, %d)", status, summary, count)
	}
}

func TestAppendSubAgentQualityFailure(t *testing.T) {
	status, summary, count := appendSubAgentQualityFailure(codingSubAgentQualityPassed, "exploration, verification, and diff check passed", 0, "claimed verification command passed but audit log recorded failure: `go test ./gui`")
	if status != codingSubAgentQualityFailed || count != 1 || !strings.Contains(summary, "audit log recorded failure") {
		t.Fatalf("passed quality should become failed, got (%q, %q, %d)", status, summary, count)
	}

	status, summary, count = appendSubAgentQualityFailure(codingSubAgentQualityWarning, "scope warning", 1, "claimed verification command passed but audit log recorded failure")
	if status != codingSubAgentQualityFailed || count != 2 || !strings.Contains(summary, "scope warning; claimed verification") {
		t.Fatalf("warning quality should become failed and keep prior summary, got (%q, %q, %d)", status, summary, count)
	}

	status, summary, count = appendSubAgentQualityFailure(codingSubAgentQualityPassed, "ok", 0, "")
	if status != codingSubAgentQualityPassed || summary != "ok" || count != 0 {
		t.Fatalf("empty failure should be no-op, got (%q, %q, %d)", status, summary, count)
	}
}

func TestUnresolvedFailedSubAgentCommandsRequireLaterEquivalentRealSuccess(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./api", Succeeded: true, Summary: "ok", seq: 2},
	}
	unresolved := unresolvedFailedSubAgentCommands(commands)
	if len(unresolved) != 1 || unresolved[0].Command != "go test ./gui" {
		t.Fatalf("different successful command should not resolve failed command, got %#v", unresolved)
	}

	commands = append(commands, CodingSubAgentCommandResult{Command: "go   test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3})
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later equivalent real success should resolve failed command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "cmake --build D:\\test\\build --config Debug", Succeeded: false, Summary: "PowerShell rejected && syntax", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later PowerShell full-path cmake success should resolve failed shorthand command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `Get-ChildItem "C:\Program Files (x86)\Microsoft Visual Studio\2019" -ErrorAction SilentlyContinue 2>&1`, Succeeded: false, Summary: "PowerShell error: 找不到路径", seq: 1},
		{Command: `dir "C:\Program Files\Microsoft Visual Studio" /s /b 2>&1`, Succeeded: false, Summary: "PowerShell exception: 找不到接受实际参数“/b”的位置形式参数。", seq: 2},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 3},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later verification success should resolve environment probe failures, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `cmake --version 2>&1; node --version 2>&1`, Succeeded: false, Summary: "node not found", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("plain version probes should resolve as probe noise after later verification, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `node --version --token secret-token`, Succeeded: false, Summary: "node not found", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("version probe with secret flag should resolve as probe noise after later verification, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `cmake --version > version.txt`, Succeeded: false, Summary: "write failed", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Command, "version.txt") {
		t.Fatalf("version probe with file-writing redirection should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `node --version extra`, Succeeded: false, Summary: "unexpected arg", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Command, "extra") {
		t.Fatalf("version probe with extra args should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `gcc -v 2>&1`, Succeeded: false, Summary: "gcc: command not found", seq: 1},
		{Command: `clang++ -print-search-dirs 2>&1`, Succeeded: false, Summary: "clang++: command not found", seq: 2},
		{Command: `g++ -o hello hello.cpp`, Succeeded: true, Summary: "compiled", seq: 3},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later compiler verification success should resolve compiler probe failures, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `cl 2> probe.err; where cl.exe`, Succeeded: false, Summary: "cl not found", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Command, "probe.err") {
		t.Fatalf("compiler probe with file-writing redirection should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `gcc -v hello.c 2>&1; where gcc 2>&1`, Succeeded: false, Summary: "fatal error: hello.c: No such file", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Summary, "hello.c") {
		t.Fatalf("compiler diagnostic flag with source args should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `g++ hello.cpp 2>&1; where g++ 2>&1`, Succeeded: false, Summary: "fatal error: hello.cpp: No such file", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Summary, "hello.cpp") {
		t.Fatalf("compiler command with source args should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `g++ --version 2>&1; echo not-a-separator; cmake --version 2>&1`, Succeeded: false, Summary: "PowerShell exception", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Command, "not-a-separator") {
		t.Fatalf("non-separator echo segment should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `git reset --hard HEAD; where cl.exe`, Succeeded: false, Summary: "blocked destructive command", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || unresolved[0].Command != `git reset --hard HEAD; where cl.exe` {
		t.Fatalf("mixed real command and diagnostic probe should not be resolved as probe noise, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: false, Summary: "fatal error LNK1120: unresolved externals", seq: 1},
		{Command: `& "C:\Program Files\CMake\bin\ctest.exe" --test-dir D:\test\build -C Debug --output-on-failure`, Succeeded: true, Summary: "100% tests passed", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || !strings.Contains(unresolved[0].Summary, "LNK1120") {
		t.Fatalf("real build failure should not be resolved by different later verification, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "gui"), Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./...", WorkingDir: filepath.Join("repo", "api"), Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/api 0.1s", seq: 2},
	}
	unresolved = unresolvedFailedSubAgentCommands(commands)
	if len(unresolved) != 1 || unresolved[0].WorkingDir != filepath.Join("repo", "gui") {
		t.Fatalf("same command in different working dir should not resolve failed command, got %#v", unresolved)
	}

	commands = append(commands, CodingSubAgentCommandResult{Command: "go   test ./...", WorkingDir: filepath.Join("repo", "gui"), Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3})
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("same command in same working dir should resolve failed command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: "D:/Repo/GUI", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./...", WorkingDir: "d:/repo/gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("same Windows drive working dir with different casing should resolve failed command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "go test ./...", WorkingDir: "//Server/Share/Repo/GUI", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./...", WorkingDir: "//server/share/repo/gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("same Windows UNC working dir with different casing should resolve failed command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "timeout 30s go test ./gui", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later direct success should resolve failed timeout-wrapped command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "env CGO_ENABLED=0 go test ./gui", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "CGO_ENABLED=0 go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later equivalent env assignment success should resolve failed env-wrapped command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "cmd /c go test ./gui", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later direct success should resolve failed cmd-wrapped command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: `bash -lc "go test ./gui"`, Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later direct success should resolve failed shell-wrapped command, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "prepare-fixtures && go test ./gui", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "go test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 1 || unresolved[0].Command != "prepare-fixtures && go test ./gui" {
		t.Fatalf("later direct success should not resolve failed compound command with non-wrapper setup, got %#v", unresolved)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: false, Summary: "FAIL", seq: 1},
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s", seq: 2},
	}
	unresolved = unresolvedFailedSubAgentCommands(commands)
	if len(unresolved) != 2 {
		t.Fatalf("empty-success verification should not resolve failed command and should remain actionable, got %#v", unresolved)
	}
}

func TestUnresolvedSubAgentGuardrailViolationsKeepsOnlyBlockingItems(t *testing.T) {
	guardrails := []CodingSubAgentGuardrailViolation{
		{
			Tool:     "bash",
			Category: codingSubAgentGuardrailCategoryCommand,
			Command:  `cd D:\test\build && cmake --build D:\test\build --config Debug`,
			Summary:  "PowerShell command compatibility: uses bash-only syntax such as `&&`.",
			seq:      2,
		},
		{
			Tool:     "bash",
			Category: codingSubAgentGuardrailCategoryGit,
			Command:  "git reset --hard HEAD",
			Summary:  "blocked destructive git command",
			seq:      3,
		},
	}
	commands := []CodingSubAgentCommandResult{
		{Command: `& "C:\Program Files\CMake\bin\cmake.exe" --build D:\test\build --config Debug`, Succeeded: true, Summary: "Build succeeded", seq: 4},
	}

	unresolved := unresolvedSubAgentGuardrailViolations(guardrails, commands)
	if len(unresolved) != 1 || unresolved[0].Command != "git reset --hard HEAD" {
		t.Fatalf("resolved PowerShell compatibility guardrail should be filtered while destructive guardrail remains, got %#v", unresolved)
	}
}

func TestUnresolvedFailedSubAgentDynamicToolsRequireLaterSameTargetSuccess(t *testing.T) {
	tools := []CodingSubAgentDynamicToolResult{
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "browser closed", seq: 1},
		{Tool: "call_mcp_tool", Name: "browser/open", Succeeded: true, Summary: "opened", seq: 2},
	}
	unresolved := unresolvedFailedSubAgentDynamicTools(tools)
	if len(unresolved) != 1 || unresolved[0].Name != "browser/screenshot" {
		t.Fatalf("different successful dynamic tool target should not resolve failed target, got %#v", unresolved)
	}

	tools = append(tools, CodingSubAgentDynamicToolResult{Tool: "call_mcp_tool", Name: " browser/screenshot ", Succeeded: true, Summary: "captured", seq: 3})
	if unresolved := unresolvedFailedSubAgentDynamicTools(tools); len(unresolved) != 0 {
		t.Fatalf("later same dynamic tool target success should resolve failed target, got %#v", unresolved)
	}

	tools = []CodingSubAgentDynamicToolResult{
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "browser closed", seq: 1},
		{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: true, Summary: "(无输出)", seq: 2},
	}
	unresolved = unresolvedFailedSubAgentDynamicTools(tools)
	if len(unresolved) != 2 {
		t.Fatalf("empty successful dynamic tool output should not resolve failed target and should remain actionable, got %#v", unresolved)
	}

	tools = []CodingSubAgentDynamicToolResult{
		{Tool: "call_mcp_tool", Succeeded: false, Summary: "target unavailable", seq: 1},
		{Tool: "call_mcp_tool", Name: "browser/open", Succeeded: true, Summary: "opened", seq: 2},
	}
	unresolved = unresolvedFailedSubAgentDynamicTools(tools)
	if len(unresolved) != 1 || unresolved[0].Summary != "target unavailable" {
		t.Fatalf("unnamed failed dynamic tool should not be resolved by same tool success on another target, got %#v", unresolved)
	}
}

func TestCodingSubAgentCodeGraphCommandCountsAsExploration(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}

	cb.trackCommandResult(map[string]interface{}{
		"command":     `codegraph explore "buildTaskUserMessage"`,
		"working_dir": project,
	}, "found symbol and callers", true)
	cb.trackFile("gui/coding_subagent.go")

	searches := cb.getSearchesRun()
	if len(searches) != 1 {
		t.Fatalf("expected codegraph command to create one search record, got %d", len(searches))
	}
	if searches[0].Tool != "codegraph" || !strings.Contains(searches[0].Query, "codegraph explore") || !searches[0].Succeeded {
		t.Fatalf("unexpected codegraph search record: %#v", searches[0])
	}
	if !cb.exploredBeforeFirstEdit() {
		t.Fatalf("successful codegraph exploration before edit should satisfy exploration gate")
	}

	cb = &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackCommandResult(map[string]interface{}{
		"command":     `codegraph.exe node gui/coding_subagent.go`,
		"working_dir": project,
	}, "file source", true)
	cb.trackCommandResult(map[string]interface{}{
		"command":     `codegraph.cmd explore "quality audit"`,
		"working_dir": project,
	}, "symbols", true)
	cb.trackCommandResult(map[string]interface{}{
		"command":     `./tools/codegraph explore "orchestrator"`,
		"working_dir": project,
	}, "symbols", true)
	if searches := cb.getSearchesRun(); len(searches) != 3 || searches[0].Tool != "codegraph" || searches[1].Tool != "codegraph" || searches[2].Tool != "codegraph" {
		t.Fatalf("codegraph executable suffixes and paths should count as exploration, got %#v", searches)
	}

	cb = &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackCommandResult(map[string]interface{}{
		"command":     `codegraph explore "missing"`,
		"working_dir": project,
	}, "No results found", true)
	cb.trackFile("gui/coding_subagent.go")
	searches = cb.getSearchesRun()
	if len(searches) != 1 || !subAgentSearchSuccessLooksEmpty(searches[0]) {
		t.Fatalf("empty codegraph result should be audited as empty successful search, got %#v", searches)
	}
	if cb.exploredBeforeFirstEdit() {
		t.Fatalf("empty codegraph result before edit should not satisfy exploration gate")
	}

	cb = &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackCommandResult(map[string]interface{}{
		"command":     `codegraph explore "missing"`,
		"working_dir": project,
	}, "command failed", false)
	cb.trackCommandResult(map[string]interface{}{
		"command":     "go test ./gui",
		"working_dir": project,
	}, "ok", true)
	if searches := cb.getSearchesRun(); len(searches) != 0 {
		t.Fatalf("failed codegraph or ordinary commands should not create search records: %#v", searches)
	}
}

func TestCodingSubAgentSearchTracking(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackSearchResult("Glob", map[string]interface{}{
		"pattern": "**/*.go",
		"path":    project,
	}, filepath.Join(project, "main.go"), true)
	cb.trackSearchResult("ripgrep", map[string]interface{}{
		"pattern": "func main",
		"path":    project,
	}, "main.go:1:func main() {}", true)
	cb.trackSearchResult("knowledge_search", map[string]interface{}{
		"query": "API contract",
	}, "2 results", true)

	searches := cb.getSearchesRun()
	if len(searches) != 3 {
		t.Fatalf("expected 3 search records, got %d", len(searches))
	}
	if searches[0].Tool != "Glob" || searches[0].Query != "**/*.go" || !searches[0].Succeeded {
		t.Fatalf("unexpected Glob record: %#v", searches[0])
	}
	if searches[1].Tool != "ripgrep" || searches[1].Query != "func main" || !searches[1].Succeeded {
		t.Fatalf("unexpected ripgrep record: %#v", searches[1])
	}
	if searches[2].Tool != "knowledge_search" || searches[2].Query != "API contract" || !searches[2].Succeeded {
		t.Fatalf("unexpected knowledge_search record: %#v", searches[2])
	}
}

func TestCodingSubAgentSearchTrackingCompactsLongFields(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	longQuery := "func " + strings.Repeat("VeryLongSymbolName", 30)
	longPath := filepath.Join(project, strings.Repeat("very-long-folder"+string(filepath.Separator), 30))

	cb.trackSearchResult("ripgrep", map[string]interface{}{
		"pattern": longQuery,
		"path":    longPath,
	}, strings.Repeat("main.go:1:match\n", 300), true)

	searches := cb.getSearchesRun()
	if len(searches) != 1 {
		t.Fatalf("expected 1 search record, got %d", len(searches))
	}
	if !strings.Contains(searches[0].Query, "截断") {
		t.Fatalf("expected long query to be compacted, got %q", searches[0].Query)
	}
	if !strings.Contains(searches[0].Path, "截断") {
		t.Fatalf("expected long path to be compacted, got %q", searches[0].Path)
	}
	if !strings.Contains(searches[0].Summary, "截断") {
		t.Fatalf("expected long search result to be compacted, got %q", searches[0].Summary)
	}
}

func TestSearchResultSummaryAndStatus(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	matched := agent.ToolGlobDetailed(map[string]interface{}{"path": dir, "pattern": "**/*.go"})
	if matched.Outcome != agent.SearchToolOutcomeMatched {
		t.Fatalf("matched outcome = %q, text=%q", matched.Outcome, matched.Text)
	}
	missing := agent.ToolGlobDetailed(map[string]interface{}{"path": dir, "pattern": "**/*.md"})
	if missing.Outcome != agent.SearchToolOutcomeNoMatch {
		t.Fatalf("missing outcome = %q, text=%q", missing.Outcome, missing.Text)
	}
	invalid := agent.ToolRipgrepDetailed(map[string]interface{}{"path": dir, "pattern": "["})
	if invalid.Outcome != agent.SearchToolOutcomeError {
		t.Fatalf("invalid regex outcome = %q, text=%q", invalid.Outcome, invalid.Text)
	}
	long := compactSearchResult(strings.Repeat("main.go:1:x\n", 300))
	if len(long) >= len(strings.Repeat("main.go:1:x\n", 300)) {
		t.Fatal("expected long search result to be truncated")
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: dir}}
	noMatchResult := cb.executeToolWithOutcome("Glob", `{"pattern":"**/*.md"}`)
	if noMatchResult.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("no-match Glob outcome = %q, want success; result=%s", noMatchResult.Outcome, noMatchResult.Text)
	}
	searches := cb.getSearchesRun()
	if len(searches) != 1 || !searches[0].Succeeded || searches[0].Query != "**/*.md" {
		t.Fatalf("no-match search should be audited as a successful tool call, got %#v", searches)
	}
	if countSuccessfulSubAgentSearches(searches) != 0 {
		t.Fatalf("no-match search should not count as successful exploration evidence, got %#v", searches)
	}
}

func TestSummarizeSubAgentExploration(t *testing.T) {
	status, summary := summarizeSubAgentExploration(nil, nil, nil, true)
	if status != "not_needed" || !strings.Contains(summary, "跳过") {
		t.Fatalf("not_needed exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, nil, nil, true)
	if status != "missing" || !strings.Contains(summary, "没有记录") {
		t.Fatalf("missing exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, []string{"main.go"}, nil, true)
	if status != "read_only" || !strings.Contains(summary, "1") {
		t.Fatalf("read_only exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, []string{"main.go"}, []CodingSubAgentSearchResult{
		{Tool: "ripgrep", Query: "func main", Succeeded: true, Summary: "main.go: func main()"},
		{Tool: "Glob", Query: "**/*.go", Succeeded: false},
	}, true)
	if status != "explored" || !strings.Contains(summary, "1 次成功搜索") {
		t.Fatalf("explored summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, nil, []CodingSubAgentSearchResult{
		{Tool: "list_directory", Path: ".", Succeeded: true, Summary: "main.go\nREADME.md"},
	}, true)
	if status != codingSubAgentQualityMissing || !strings.Contains(summary, "没有记录") {
		t.Fatalf("directory listing alone should not satisfy existing-file exploration, got (%q, %q)", status, summary)
	}
}

func TestSummarizeSubAgentExplorationRequiresPreEditEvidence(t *testing.T) {
	status, summary := summarizeSubAgentExploration(
		[]string{"main.go"},
		[]string{"main.go"},
		[]CodingSubAgentSearchResult{{Tool: "ripgrep", Query: "func main", Succeeded: true, Summary: "main.go: func main()"}},
		false,
	)
	if status != codingSubAgentQualityMissing || !strings.Contains(summary, "首次修改前") {
		t.Fatalf("post-edit exploration should be missing, got (%q, %q)", status, summary)
	}
}

func TestCodingSubAgentExploredBeforeFirstEdit(t *testing.T) {
	project := t.TempDir()
	file := filepath.Join(project, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	readFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	readFirst.trackReadFile(file)
	readFirst.trackFile(file)
	if !readFirst.exploredBeforeFirstEdit() {
		t.Fatal("read before edit should satisfy pre-edit exploration")
	}

	editFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	editFirst.trackFile(file)
	editFirst.trackReadFile(file)
	if editFirst.exploredBeforeFirstEdit() {
		t.Fatal("read after edit should not satisfy pre-edit exploration")
	}

	failedSearchFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	failedSearchFirst.trackSearchResult("ripgrep", map[string]interface{}{"pattern": "func main"}, "no matches", false)
	failedSearchFirst.trackFile(file)
	if failedSearchFirst.exploredBeforeFirstEdit() {
		t.Fatal("failed search before edit should not satisfy pre-edit exploration")
	}

	noMatchSearchFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	noMatchSearchFirst.trackSearchResult("ripgrep", map[string]interface{}{"pattern": "removed symbol"}, "no matches", true)
	noMatchSearchFirst.trackFile(file)
	if noMatchSearchFirst.exploredBeforeFirstEdit() {
		t.Fatal("no-match search before edit should not satisfy pre-edit exploration")
	}

	listDirFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	listDirFirst.trackSearchResult("list_directory", map[string]interface{}{"path": "."}, "main.go\nREADME.md", true)
	listDirFirst.trackFile(file)
	if listDirFirst.exploredBeforeFirstEdit() {
		t.Fatal("directory listing before edit should not satisfy pre-edit exploration for existing files")
	}

	successSearchFirst := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	successSearchFirst.trackSearchResult("ripgrep", map[string]interface{}{"pattern": "func main"}, "main.go:1:func main", true)
	successSearchFirst.trackFile(file)
	if !successSearchFirst.exploredBeforeFirstEdit() {
		t.Fatal("successful search before edit should satisfy pre-edit exploration")
	}
}

func TestSubAgentPathEvidenceKeyCleansEquivalentPaths(t *testing.T) {
	cases := map[string]string{
		" /repo/./src//main.go ": "/repo/src/main.go",
		"src/../src/main.go":     "src/main.go",
		`C:\repo\src\main.go`:    "C:/repo/src/main.go",
		".":                      "",
		" ":                      "",
	}
	for input, want := range cases {
		if got := subAgentPathEvidenceKey(input); got != want {
			t.Fatalf("subAgentPathEvidenceKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAppendSubAgentExplorationSummary(t *testing.T) {
	summary := appendSubAgentExplorationSummary("完成", "read_only", "读取了 1 个文件后修改。")
	if !strings.Contains(summary, "## 探索状态") || !strings.Contains(summary, "READ_ONLY") {
		t.Fatalf("unexpected exploration summary: %s", summary)
	}
}

func TestSummarizeSubAgentVerification(t *testing.T) {
	status, summary := summarizeSubAgentVerification(nil, nil, 0)
	if status != "not_needed" || !strings.Contains(summary, "跳过") {
		t.Fatalf("not_needed summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, nil, 0)
	if status != "missing" || !strings.Contains(summary, "verification command") {
		t.Fatalf("missing summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./..." + strings.Repeat(" very-long-flag", 30), Succeeded: false, Summary: "compiler error: missing symbol\\nfull output"},
	}, 0)
	if status != "failed" || !strings.Contains(summary, "go test ./...") || !strings.Contains(summary, "compiler error: missing symbol") || !strings.Contains(summary, "截断") {
		t.Fatalf("failed summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
		{Command: "go vet ./...", Succeeded: true, Summary: "go vet ./... ok"},
	}, 0)
	if status != "passed" || !strings.Contains(summary, "2 条") || !strings.Contains(summary, "`go test ./...`") || !strings.Contains(summary, "`go vet ./...`") {
		t.Fatalf("passed summary should include verification command list, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true, seq: 2},
		{Command: "go test ./... > test.log", Succeeded: true, seq: 3},
	}, 1)
	if status != codingSubAgentQualityMissing || !strings.Contains(summary, "failure-suppressing shell syntax") || !strings.Contains(summary, "output redirection") {
		t.Fatalf("fresh unsafe verification should invalidate adjacent passing verification, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go fmt ./...", Succeeded: true},
		{Command: "npm run format", Succeeded: true},
		{Command: "make fmt", Succeeded: true},
		{Command: "prettier --write .", Succeeded: true},
		{Command: "biome format --write .", Succeeded: true},
	}, 0)
	if status != codingSubAgentQualityMissing || !strings.Contains(summary, "test/build/lint/typecheck") {
		t.Fatalf("formatter-only commands should not satisfy verification, got (%q, %q)", status, summary)
	}
}

func TestSummarizeSubAgentVerificationRejectsEmptySuccessfulOutput(t *testing.T) {
	status, summary := summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "================ no tests collected in 0.01s ================"},
	}, 0)
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "未实际执行测试或检查") || !strings.Contains(summary, "`pytest tests`") {
		t.Fatalf("empty successful pytest output should fail quality audit, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.ts"}, []CodingSubAgentCommandResult{
		{Command: "npm test", Succeeded: true, Summary: "No test files found, exiting with code 0"},
	}, 0)
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "未实际执行测试或检查") {
		t.Fatalf("empty successful npm test output should fail quality audit, got (%q, %q)", status, summary)
	}

	emptySuccessfulOutputs := []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true},
		{Command: "go test ./...", Succeeded: true, Summary: "   "},
		{Command: "go test ./...", Succeeded: true, Summary: "(无输出)"},
		{Command: "go test ./pkg/empty", Succeeded: true, Summary: "?   \tgithub.com/RapidAI/CodeClaw/pkg/empty\t[no test files]"},
		{Command: "node --test", Succeeded: true, Summary: "# tests 0\n# suites 0\n# pass 0\n# fail 0"},
		{Command: "rspec", Succeeded: true, Summary: "0 examples, 0 failures"},
		{Command: "vendor/bin/phpunit", Succeeded: true, Summary: "No tests executed!"},
		{Command: "gradle test", Succeeded: true, Summary: "0 tests completed, 0 failed"},
		{Command: "python -m unittest", Succeeded: true, Summary: "----------------------------------------------------------------------\nRan 0 tests in 0.000s\n\nOK"},
		{Command: "bundle exec cucumber", Succeeded: true, Summary: "0 scenarios\n0 steps\n0m0.000s"},
		{Command: "vendor/bin/phpunit", Succeeded: true, Summary: "OK (0 tests, 0 assertions)"},
		{Command: "vitest run", Succeeded: true, Summary: "Test Files  0 passed (0)\nTests  0 passed (0)"},
		{Command: "jest --runInBand", Succeeded: true, Summary: "Test Suites: 0 passed, 0 total\nTests:       0 total\nSnapshots:   0 total"},
		{Command: "vitest run", Succeeded: true, Summary: "Test Files: 0 passed (0)\nTests: 0 total"},
		{Command: "go test ./...", Succeeded: true, Summary: `go: warning: "./..." matched no packages`},
		{Command: "mvn test", Succeeded: true, Summary: "[INFO] No tests to run."},
		{Command: "cargo test", Succeeded: true, Summary: "running 0 tests\n\ntest result: ok. 0 passed; 0 failed; 0 ignored"},
		{Command: "pytest -m slow", Succeeded: true, Summary: "collected 12 items / 12 deselected / 0 selected"},
		{Command: "dotnet test", Succeeded: true, Summary: "Total tests: 0. Passed: 0. Failed: 0. Skipped: 0."},
		{Command: "biome check .", Succeeded: true, Summary: "Checked 0 files in 2ms. No fixes applied."},
		{Command: "prettier --check src", Succeeded: true, Summary: "Checking formatting...\n0 files checked."},
		{Command: "eslint .", Succeeded: true, Summary: "Processed 0 files."},
		{Command: "eslint .", Succeeded: true, Summary: "No files matched the pattern \"src/**/*.ts\"."},
		{Command: "prettier --check src", Succeeded: true, Summary: "No matching files. Patterns tried: src/**/*.tsx"},
		{Command: "biome check src", Succeeded: true, Summary: "No files to check."},
		{Command: "ruff check src", Succeeded: true, Summary: "Found 0 files."},
		{Command: "mypy src", Succeeded: true, Summary: "There are no source files."},
		{Command: "pnpm --filter missing-package test", Succeeded: true, Summary: "No projects matched the filters in \"D:\\work\\repo\""},
		{Command: "npm test -- --testNamePattern missing", Succeeded: true, Summary: "No tests matching pattern \"missing\" were found."},
	}
	for _, command := range emptySuccessfulOutputs {
		status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{command}, 0)
		if status != codingSubAgentQualityFailed || !strings.Contains(summary, "未实际执行测试或检查") || !strings.Contains(summary, "`"+command.Command+"`") {
			t.Fatalf("empty successful output should fail quality audit for %q, got (%q, %q)", command.Command, status, summary)
		}
	}

	status, summary = summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "1 passed in 0.05s"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`pytest tests`") {
		t.Fatalf("normal successful pytest output should pass, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest -m slow", Succeeded: true, Summary: "collected 12 items / 2 deselected / 10 selected"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`pytest -m slow`") {
		t.Fatalf("pytest deselected count should not look empty when tests were selected, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"spec/app_spec.rb"}, []CodingSubAgentCommandResult{
		{Command: "rspec", Succeeded: true, Summary: "10 examples, 0 failures"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`rspec`") {
		t.Fatalf("normal successful rspec output should pass, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"features/app.feature"}, []CodingSubAgentCommandResult{
		{Command: "bundle exec cucumber", Succeeded: true, Summary: "1 scenario (1 passed)\n3 steps (3 passed)\n0 failures"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`bundle exec cucumber`") {
		t.Fatalf("normal successful cucumber output with 0 failures should pass, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"App.Tests/UnitTest1.cs"}, []CodingSubAgentCommandResult{
		{Command: "dotnet test", Succeeded: true, Summary: "Total tests: 10. Passed: 10. Failed: 0. Skipped: 0."},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`dotnet test`") {
		t.Fatalf("normal successful dotnet output should pass, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"src/app.ts"}, []CodingSubAgentCommandResult{
		{Command: "eslint .", Succeeded: true, Summary: "10 files checked, 0 errors"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`eslint .`") {
		t.Fatalf("normal successful eslint output with 0 errors should pass, got (%q, %q)", status, summary)
	}
}

func TestCountSuccessfulSubAgentVerificationCommandsRejectsEmptySuccessfulOutput(t *testing.T) {
	count := countSuccessfulSubAgentVerificationCommands([]CodingSubAgentCommandResult{
		{Command: "pytest tests", Succeeded: true, Summary: "no tests collected in 0.01s"},
		{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
	})
	if count != 1 {
		t.Fatalf("expected only non-empty verification output to count, got %d", count)
	}
}

func TestSummarizeSubAgentVerificationAllowsLaterEquivalentSuccess(t *testing.T) {
	status, summary := summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest tests", WorkingDir: "/repo", Succeeded: false, Summary: "FAILED tests/test_main.py", seq: 2},
		{Command: "pytest tests", WorkingDir: "/repo", Succeeded: true, Summary: "1 passed in 0.1s", seq: 3},
	}, 1)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "1 条有效") || strings.Contains(summary, "FAILED tests/test_main.py") {
		t.Fatalf("later equivalent success should resolve verification failure, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest tests", WorkingDir: "/repo", Succeeded: false, Summary: "FAILED tests/test_main.py", seq: 2},
		{Command: "pytest tests", WorkingDir: "/other", Succeeded: true, Summary: "1 passed in 0.1s", seq: 3},
	}, 1)
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "FAILED tests/test_main.py") {
		t.Fatalf("different working dir success should not resolve verification failure, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.py"}, []CodingSubAgentCommandResult{
		{Command: "pytest tests", WorkingDir: "/repo", Succeeded: false, Summary: "FAILED tests/test_main.py", seq: 2},
		{Command: "pytest tests", WorkingDir: "/repo", Succeeded: true, Summary: "no tests collected", seq: 3},
	}, 1)
	if status != codingSubAgentQualityFailed || !strings.Contains(summary, "FAILED tests/test_main.py") {
		t.Fatalf("empty equivalent success should not resolve verification failure, got (%q, %q)", status, summary)
	}
}

func TestSummarizeSubAgentVerificationPassedSummaryCapsCommands(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./pkg/a", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/a 0.1s"},
		{Command: "go test ./pkg/b", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/b 0.1s"},
		{Command: "go test ./pkg/c", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/c 0.1s"},
		{Command: "go test ./pkg/d", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/d 0.1s"},
		{Command: "go test ./pkg/e", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/e 0.1s"},
		{Command: "go test ./pkg/f", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/pkg/f 0.1s"},
	}
	status, summary := summarizeSubAgentVerification([]string{"main.go"}, commands, 0)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("passed summary status = %q, summary=%q", status, summary)
	}
	if !strings.Contains(summary, "`go test ./pkg/a`") || !strings.Contains(summary, "还有 1 条未展开") || strings.Contains(summary, "go test ./pkg/f") {
		t.Fatalf("passed summary should cap command list, got %q", summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "npm run `test`", Succeeded: true, Summary: "1 test passed"},
	}, 0)
	if status != codingSubAgentQualityPassed || !strings.Contains(summary, "`npm run 'test'`") {
		t.Fatalf("passed summary should escape inline-code backticks, got (%q, %q)", status, summary)
	}
}
func TestSummarizeSubAgentVerificationIgnoresNonVerificationCommands(t *testing.T) {
	status, summary := summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: true},
		{Command: "pwd", Succeeded: true},
	}, 0)
	if status != "missing" || !strings.Contains(summary, "test/build/lint/typecheck") {
		t.Fatalf("non-verification commands should not satisfy verification, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: false},
		{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s"},
	}, 0)
	if status != "passed" || !strings.Contains(summary, "1 条") {
		t.Fatalf("only verification commands should determine pass count, got (%q, %q)", status, summary)
	}
}

func TestIsSubAgentVerificationCommand(t *testing.T) {
	positive := []string{
		"go test ./...",
		"go.exe test ./...",
		"go -C gui test ./...",
		"go -C=gui test ./...",
		"go -C gui vet ./...",
		"go -C=gui build ./...",
		"go test ./... 2>&1",
		`go test ./... -run "TestAPI|TestHandler"`,
		"go test ./... -run TestIsSubAgentVerificationCommand",
		"go test ./... -run '^$' -bench .",
		"go test -run=^$ -bench=BenchmarkRender ./gui",
		"go test ./... -run '^$' -fuzz FuzzParse",
		"go test ./... -count=1",
		"go test ./... -count 2",
		`bash -lc 'go test ./... -run "TestAPI|TestHandler"'`,
		`powershell -NoProfile -Command 'go test ./... -run "TestAPI|TestHandler"'`,
		"bash -c go test ./...",
		`bash -lc "go test ./..."`,
		`powershell -NoProfile -Command "go test ./..."`,
		"bash -lc go test ./...",
		"powershell -NoProfile -Command go test ./...",
		"pwsh -Command go test ./...",
		"cmd /c go test ./...",
		`cmd.exe /d /s /c "go test ./..."`,
		"cmd /c npm test",
		"npm run build",
		"npm --workspace web test",
		"npm --workspace=web run lint",
		"npm run -w web test",
		"npm.cmd test",
		"npm run lint",
		"npm test",
		"npm run test -- --watch=false",
		"npm run test:unit",
		`npm run "test:unit"`,
		"npm run unit",
		"npm run e2e",
		"npm run integration",
		"npm run verify",
		"npm run validate",
		"npm run ci",
		"npm run build:prod",
		"npm run type-check",
		"pnpm test",
		"pnpm --filter web test",
		"pnpm --filter=web run lint",
		"pnpm -F web exec vitest run",
		"pnpm lint",
		"pnpm run test:e2e",
		"pnpm run verify:all",
		"yarn test",
		"yarn --cwd web test",
		"yarn workspace web test",
		"yarn workspace web verify",
		"yarn workspaces foreach -A run test",
		"yarn workspaces foreach --all --topological run build",
		"yarn workspaces foreach --from web run lint",
		"yarn workspaces foreach --from web run ci",
		"yarn workspaces foreach -A exec tsc --noEmit",
		"yarn workspaces foreach --from web exec eslint .",
		"yarn workspaces foreach --all exec vitest run",
		"yarn build",
		"yarn test:unit",
		"node --test",
		"node --test test/*.test.js",
		"node --test --test-only=false test/*.test.js",
		"bun test",
		"bun run test",
		"bun run build",
		"bun run lint",
		"deno test",
		"deno lint",
		"deno check mod.ts",
		"deno task test",
		"deno task build",
		"deno task type-check",
		"dart test",
		"dart analyze lib test",
		"dart compile exe bin/server.dart",
		"flutter test",
		"flutter --device-id chrome test",
		"flutter analyze",
		"flutter build apk --debug",
		"mix test",
		"mix compile --warnings-as-errors",
		"mix credo --strict",
		"mix dialyzer",
		"MIX_ENV=test mix test",
		"cucumber",
		"bundle exec cucumber",
		"npm run typecheck",
		"eslint .",
		"eslint . --fix=false",
		"npm exec eslint .",
		"npm exec -- eslint .",
		"pnpm exec vitest run",
		"vitest run --ui=false",
		"jest --watchAll=false --runInBand",
		"pnpm dlx --package typescript tsc --noEmit",
		"yarn dlx tsc --noEmit",
		"corepack pnpm test",
		"corepack pnpm --filter web test",
		"corepack yarn --cwd web run lint",
		"corepack yarn run lint",
		"corepack npx eslint .",
		"corepack npx --yes eslint .",
		"npx tsc --noEmit",
		"npx turbo run test",
		"pnpm exec turbo run build --filter web",
		"yarn dlx turbo run lint typecheck",
		"npx nx test web",
		"npx nx run web:test",
		"pnpm exec nx affected -t test",
		"yarn nx run-many --target=lint",
		"npx --yes tsc --noEmit",
		"prettier --check .",
		"prettier -c .",
		"prettier --list-different .",
		"prettier --check . --write=false",
		"prettier --check . -w=false",
		"npx prettier --check .",
		"biome check .",
		"biome check . --write=false",
		"biome ci .",
		"biome lint .",
		"npx biome check .",
		"npx --package typescript tsc --noEmit",
		"npx.cmd tsc --noEmit",
		"cargo clippy --all-targets",
		"cargo test --workspace",
		"cargo test -- --nocapture",
		"cargo check --all-targets",
		"swift test",
		"swift test --filter ParserTests",
		"swift build",
		"zig test src/main.zig",
		"zig build test",
		"zig build",
		"stack test",
		"stack --stack-yaml stack-ci.yaml test",
		"stack build",
		"cabal test all",
		"cabal v2-test all",
		"cabal --project-file=cabal.project.ci build all",
		"cabal haddock all",
		"lein test",
		"lein check",
		"lein clj-kondo",
		"clojure -M:test",
		"clojure -X:test",
		"clojure -M:kaocha",
		"clj -M -m kaocha.runner",
		"bb test",
		"bb run test",
		"sbt test",
		"sbt compile",
		"sbt scalafmtCheckAll",
		"sbt clean test",
		"sbt +testQuick",
		"mill __.test",
		"mill app.compile",
		"mill app.scalafmtCheck",
		"dune runtest",
		"dune test",
		"dune build @check",
		"dune --profile release build @runtest",
		"opam exec -- dune runtest",
		"opam --switch 5.1 exec -- dune build @check",
		"go vet ./...",
		"go build ./...",
		"golangci-lint run ./...",
		"golangci-lint.exe run",
		"staticcheck ./...",
		"revive ./...",
		"pytest tests",
		"pytest.exe tests",
		"python -m pytest tests",
		"ruff check .",
		"ruff.exe check .",
		"python -m ruff check .",
		"mypy src",
		"python -m mypy src",
		"pyright",
		"basedpyright src",
		"pyre check",
		"uv run pytest tests",
		"uv run --with pytest pytest tests",
		"uvx --from ruff ruff check .",
		"uv run ruff check .",
		"uv run lint --fix=false",
		"uv run --project app pytest tests",
		"poetry run pytest tests",
		"poetry run -- mypy src",
		"poetry run mypy src",
		"poetry run lint --fix=false",
		"poetry check",
		"poetry --directory app check",
		"pipenv run pytest tests",
		"hatch run test",
		"hatch test",
		"pdm run pytest",
		"pdm check",
		"pdm --project app check",
		"rye test",
		"rye lint --fix=false",
		"tox -q",
		"nox -s tests",
		"make check",
		"make -C build",
		"make -C build test",
		"make -j4 all",
		"make -w test",
		"mingw32-make check",
		"just test",
		"just verify",
		"just ci",
		"just --justfile recipes.just lint",
		"just -d gui typecheck",
		"just --set profile ci test",
		"task test",
		"task e2e",
		"go-task integration",
		"go-task --taskfile Taskfile.yml build",
		"task -d gui lint",
		"task -s test",
		"mage test",
		"mage -d gui check",
		"bazel test //...",
		"bazel --bazelrc=.bazelrc build //app:all",
		"bazelisk test //pkg:unit",
		"pants test ::",
		"pants --pants-workdir .pants.d lint ::",
		"pants check src/python::",
		"buck2 test //app/...",
		"buck2 --isolation-dir task build //app:lib",
		"cmake --build build",
		"cmake --build build --target test",
		"ctest --test-dir build --output-on-failure",
		"ninja -C build test",
		"ninja -C build all",
		"ninja",
		"g++ -o hello hello.cpp",
		"gcc -Wall -Wextra main.c -o main",
		"clang++ -std=c++17 src/main.cc -o app",
		"cc -c src/module.c",
		"cl /EHsc hello.cpp",
		"make lint",
		"dotnet build",
		"dotnet test --no-build",
		"dotnet test --filter FullyQualifiedName~Unit",
		"dotnet vstest bin/Debug/app.Tests.dll",
		"dotnet format --verify-no-changes",
		"dotnet msbuild -t:Test",
		"dotnet msbuild /t:Restore,Build",
		"bundle exec rspec",
		"bundle exec lint --fix=false",
		"rails test",
		"bin/rails test test/models/user_test.rb",
		"bundle exec rails test",
		"rake test",
		"bundle exec rake spec",
		"bundle exec rake test:models",
		"rubocop",
		"rubocop -A=false",
		"bundle exec rubocop",
		"vendor/bin/phpunit",
		"./vendor/bin/phpunit",
		"vendor/bin/phpstan analyse src",
		"./vendor/bin/psalm --no-cache",
		"vendor/bin/pest --ci",
		"composer test",
		"composer run test",
		"composer run-script lint",
		"composer run lint -- --fix=false",
		"composer --working-dir app exec phpunit",
		"composer exec -- phpstan analyse src",
		"composer exec pest --ci",
		"./mvnw test",
		"mvn -q test",
		"mvn -B -pl app -am verify",
		"mvnw -DskipTests=false test",
		"mvn -Dmaven.test.skip=false verify",
		"mvnw verify",
		"gradlew.bat test",
		"gradlew.bat :app:build",
		"./gradlew check",
		"./gradlew --continue :service:check",
		"gradle :app:test",
		"gradle --continue :app:test",
		"CGO_ENABLED=0 go test ./...",
		"env CGO_ENABLED=0 go test ./...",
		"env -i CGO_ENABLED=0 go test ./...",
		"env -u GOPROXY go test ./...",
		"env --unset GOPROXY go test ./...",
		"cross-env CI=1 npm test",
		"cross-env-shell CI=1 npm run lint",
		"time go test ./...",
		"timeout 30s go test ./...",
		"timeout --preserve-status -k 5s 30s npm test",
		"timeout --signal=TERM 1m pytest tests",
		"gtimeout 45s go test ./gui",
		"env CI=1 timeout 30s go test ./...",
		"go test ./... && go vet ./...",
		"mkdir out || true; go test ./...",
		"optional-setup || true && go test ./...",
	}
	for _, command := range positive {
		if !isSubAgentVerificationCommand(command) {
			t.Fatalf("expected verification command: %q", command)
		}
	}

	negative := []string{
		"git status --short",
		"pwd",
		"ls",
		"echo test",
		"echo build",
		"echo lint",
		"echo --test",
		"rake assets:precompile",
		"rg test .",
		"git log --oneline --grep test",
		"npm exec serve .",
		"npm --workspace web exec serve .",
		"pnpm --filter web dev",
		"pytest --collect-only tests",
		"pytest --collect-only=true tests",
		"python -m pytest --co tests",
		"pytest --setup-only tests",
		"python -m pytest --setup-plan tests",
		"pytest --markers",
		"python -m pytest --trace-config",
		"pytest --help",
		"npx jest --listTests",
		"npx jest --listTests=true",
		"jest --showConfig",
		"jest --showConfig=true",
		"vitest list",
		"npx vitest --list",
		"npx vitest --list=true",
		"eslint --print-config src/app.ts",
		"eslint --print-config=true src/app.ts",
		"tsc --showConfig",
		"tsc --showConfig=true",
		"npx tsc --init",
		"npm test -- --watch",
		"npm run test -- --watch=true",
		"npm run test:watch",
		"pnpm run lint:watch",
		"yarn test:watch",
		"bun run test:watch",
		"deno task test:watch",
		"pnpm exec vitest watch",
		"vitest --watch",
		"vitest run --ui=true",
		"vitest run --ui=1",
		"jest --watchAll=true",
		"pytest tests --interactive=true",
		"jest --watchAll",
		"node --test --test-only",
		"node --test --test-only test/*.test.js",
		"node --test --test-only=true test/*.test.js",
		"node --test --test-only=1 test/*.test.js",
		"node --test --help",
		"node --test -h",
		"node --test --version",
		"node --test -v",
		"yarn workspace web start",
		"corepack npm exec serve .",
		"corepack pnpm --filter web dev",
		"poetry run serve",
		"poetry install",
		"poetry update",
		"poetry build",
		"poetry run lint --fix",
		"poetry run lint --fix=true",
		"poetry run lint --write=true",
		"uv run lint --fix",
		"uv run lint --write=true",
		"pipenv run flask run",
		"hatch run serve",
		"hatch build",
		"hatch env show",
		"pdm run serve",
		"pdm install",
		"pdm update",
		"pdm build",
		"rye lint --fix",
		"rye lint --write=true",
		"tox --listenvs",
		"tox --listenvs=true",
		"tox -l",
		"tox --showconfig",
		"nox --list-sessions",
		"nox --list-sessions=true",
		"nox --json",
		"bundle exec rails server",
		"bundle exec lint --fix",
		"bundle exec rspec --dry-run",
		"cucumber --dry-run",
		"bundle exec cucumber --dry-run",
		"cmake -S . -B build",
		"cmake --build build --target clean",
		"cmake --build build --target install",
		"ctest --show-only",
		"ctest -N",
		"ctest --help",
		"ninja -C build clean",
		"ninja -C build clean || true",
		"ninja -t clean",
		"ninja -tclean",
		"ninja -t=clean",
		"make -C build clean",
		"make clean",
		"make install",
		"g++ --version",
		"gcc -v",
		"clang++ -print-search-dirs",
		"cc -o hello hello.o",
		"cl /?",
		"dotnet run",
		"dotnet watch test",
		"dotnet tool restore",
		"dotnet format",
		"dotnet test --list-tests",
		"dotnet test --list-tests=true",
		"dotnet test -t",
		"dotnet vstest bin/Debug/app.Tests.dll /ListTests",
		"dotnet msbuild -t:Clean",
		"./vendor/bin/php-cs-fixer fix",
		"composer install",
		"composer update",
		"composer dump-autoload",
		"composer run serve",
		"composer run lint -- --fix",
		"composer run lint -- --fix=true",
		"composer run-script lint -- --write",
		"composer exec php-cs-fixer fix",
		"vendor/bin/phpunit --list-tests",
		"./vendor/bin/phpunit --list-groups",
		"vendor/bin/pest --list-suites",
		"phpunit --generate-configuration",
		"phpunit --migrate-configuration",
		"vendor/bin/phpstan clear-result-cache",
		"vendor/bin/phpstan dump-parameters",
		"./vendor/bin/psalm --init",
		"vendor/bin/psalm --alter",
		"vendor/bin/psalm --set-baseline=psalm-baseline.xml",
		"vendor/bin/psalm --clear-cache",
		"./mvnw dependency:tree",
		"mvn -q dependency:tree",
		"mvn -DskipTests test",
		"mvn test -DskipTests=true",
		"mvn -Dmaven.test.skip=true verify",
		"gradle dependencies",
		"./gradlew --dry-run test",
		"./gradlew --dry-run=true test",
		"gradle -m :app:test",
		"./gradlew build -x test",
		"gradle build -xtest",
		"gradle check --exclude-task test",
		"gradlew :app:build --exclude-task=:app:test",
		"./gradlew bootRun",
		"gradlew :app:run",
		"TEST_NAME=unit echo test",
		"env TEST_NAME=unit echo test",
		"time echo test",
		"timeout 1s echo test",
		"timeout --help",
		"timeout 30s go test ./... || true",
		"cmd /c echo test",
		"cmd /k go test ./...",
		"cmd /c go test ./... || true",
		"rg \"TODO\" .",
		"git diff -- .",
		"go -C gui env",
		"go test -c",
		"go test -c -o package.test",
		"go test -list . ./...",
		"go test ./... -list Test",
		"go test -list=Test ./...",
		"go test ./... -run '^$'",
		"go test -run=^$ ./...",
		"go test ./... -run $^",
		"go test ./... -count=0",
		"go test ./... -count 0",
		`go test ./... -count "0"`,
		"go fmt ./...",
		"cargo fmt --all",
		"cargo test --no-run",
		"cargo test -- --list",
		"cargo test -- --help",
		"cargo test --help",
		"cargo clippy --fix",
		"cargo clippy --fix=true",
		"eslint . --fix=true",
		"prettier --write=true .",
		"prettier --check . -w=true",
		"biome check . --write=true",
		"biome check . --apply=true",
		"rubocop --auto-correct=true",
		"rubocop -A=true",
		"swift run App",
		"swift package update",
		"swift package resolve",
		"zig run src/main.zig",
		"zig fmt src/main.zig",
		"stack run app",
		"stack setup",
		"cabal run app",
		"cabal update",
		"cabal install exe:app",
		"lein repl",
		"lein run",
		"lein deps",
		"clojure -M:dev",
		"clojure -X",
		"clj -M -m app.main",
		"bb run app",
		"bb repl",
		"sbt run",
		"sbt console",
		"sbt update",
		"sbt assembly",
		"mill app.run",
		"mill clean",
		"dune exec ./app.exe",
		"dune utop",
		"dune promote",
		"dune clean",
		"opam install dune",
		"opam update",
		"npm run format",
		"npm run fmt",
		"pnpm run format",
		"yarn format",
		"yarn workspaces foreach -A run format",
		"yarn workspaces foreach -A exec vite --host 0.0.0.0",
		"yarn workspaces foreach -A exec eslint --fix .",
		"make fmt",
		"make format",
		"just",
		"just --list",
		"just --list test",
		"just --summary test",
		"just test:watch",
		"just fmt",
		"just dev",
		"task --list",
		"task --list test",
		"task -l test",
		"go-task --list-all test",
		"task --dry test",
		"task test:watch",
		"task dev",
		"task format",
		"mage -l",
		"mage -l test",
		"mage -h test",
		"mage dev",
		"bazel run //app:server",
		"bazel clean",
		"bazel query //...",
		"pants run src/python/app.py",
		"pants tailor",
		"buck2 run //app:server",
		"buck2 clean",
		"prettier .",
		"prettier --write .",
		"npx prettier --write .",
		"npm run lint -- --fix",
		"pnpm run lint -- --fix",
		"yarn lint --fix",
		"eslint --fix .",
		"npx eslint --fix .",
		"ruff check --fix .",
		"python -m ruff check --fix .",
		"golangci-lint run --fix ./...",
		"rubocop -a",
		"bundle exec rubocop -A",
		"bundle exec rubocop --show-cops",
		"rubocop --init",
		"biome format .",
		"biome format --write .",
		"biome check --write .",
		"biome lint --suppress --reason reviewed .",
		"npx biome lint --suppress .",
		"go test ./... | Select-String FAIL",
		"go test ./... | findstr FAIL",
		"go test ./... 2>&1 | tee test.log",
		"bash -lc go test ./... | cat",
		"go test ./... || true",
		"bash -lc go test ./... || true",
		`bash -lc "go test ./... || true"`,
		`bash -lc "go test ./... | cat"`,
		`bash -lc "go test ./... > test.log"`,
		"go test ./... || echo ignored",
		"npm test || Write-Host ignored",
		"cmd /c go test ./... || exit /b 0",
		"npm test || exit 0",
		"pytest tests ; exit 0",
		"go test ./... ; true",
		"go test ./... ; echo done",
		"go test ./... ; go vet ./...",
		"go test ./... && echo done",
		"go test ./... & echo done",
		"bash -lc go test ./... ; echo done",
		"bash -lc go test ./... && echo done",
		`bash -lc "go test ./... && echo done"`,
		`bash -lc "go test ./... & echo done"`,
		"go test ./... | tee test.log || true",
		"go test ./... > test.log",
		"go test ./... 1> test.log",
		"go test ./... >> test.log",
		"go test ./... 2> test.err",
		"go test ./... 2>>test.err",
		"go test ./... &> test.log",
		"go test ./... *> test.log",
		"bash -lc go test ./... > test.log",
		"ruff format .",
		"python -m ruff format .",
		"uv run --with ruff ruff format .",
		"uv run ruff format .",
		"npx --yes vite --host 0.0.0.0",
		"bun run dev",
		"bun run format",
		"deno fmt",
		"deno task dev",
		"deno task format",
		"dart format .",
		"dart pub get",
		"dart run build_runner watch",
		"flutter run",
		"flutter pub get",
		"flutter format .",
		"mix format",
		"mix deps.get",
		"mix phx.server",
		"npx turbo run format",
		"npx turbo prune web",
		"npx nx serve web",
		"npx nx graph",
		"npx nx reset",
		"npm exec -- vite --host 0.0.0.0",
		"golangci-lint cache clean",
		"mypy --install-types",
		"python -m mypy --install-types --non-interactive",
		"pyright --createstub requests",
		"basedpyright --createstub django",
		"pyre init",
	}
	for _, command := range negative {
		if isSubAgentVerificationCommand(command) {
			t.Fatalf("expected non-verification command: %q", command)
		}
	}
}

func TestCountFreshSubAgentVerificationAttemptsIncludesUnsafe(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true, seq: 1},
		{Command: "go test ./... > test.log", Succeeded: true, seq: 3},
		{Command: "npm test", Succeeded: true, seq: 4},
	}

	if got := countFreshSubAgentVerificationAttempts(commands, 2); got != 2 {
		t.Fatalf("fresh verification attempt count = %d, want 2", got)
	}
}

func TestSummarizeSubAgentVerificationRequiresPostEditVerification(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true, seq: 1},
	}
	status, summary := summarizeSubAgentVerification([]string{"main.go"}, commands, 2)
	if status != codingSubAgentQualityMissing || !strings.Contains(summary, "before the final edit") {
		t.Fatalf("stale verification should be missing, got (%q, %q)", status, summary)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: false, seq: 1},
		{Command: "go test ./...", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 3},
	}
	status, summary = summarizeSubAgentVerification([]string{"main.go"}, commands, 2)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("fresh passing verification should ignore stale pre-edit failure, got (%q, %q)", status, summary)
	}

	commands = []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true, seq: 1},
		{Command: "go test ./... > test.log", Succeeded: true, seq: 3},
	}
	status, summary = summarizeSubAgentVerification([]string{"main.go"}, commands, 2)
	if status != codingSubAgentQualityMissing ||
		!strings.Contains(summary, "failure-suppressing shell syntax") ||
		!strings.Contains(summary, "output redirection") ||
		strings.Contains(summary, "before the final edit") {
		t.Fatalf("fresh unsafe verification should be diagnosed before stale verification, got (%q, %q)", status, summary)
	}
}

func TestSummarizeSubAgentVerificationRejectsSuppressedFailureCommands(t *testing.T) {
	status, summary := summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./... || true", Succeeded: true},
		{Command: "npm test || exit 0", Succeeded: true},
		{Command: "go test ./... | tee test.log", Succeeded: true},
		{Command: "go test ./... ; echo done", Succeeded: true},
		{Command: "go test ./... && echo done", Succeeded: true},
		{Command: "go test ./... & echo done", Succeeded: true},
		{Command: "go test ./... > test.log", Succeeded: true},
	}, 0)
	if status != codingSubAgentQualityMissing ||
		!strings.Contains(summary, "failure-suppressing shell syntax") ||
		!strings.Contains(summary, "without || fallback") ||
		!strings.Contains(summary, "pipe filters") ||
		!strings.Contains(summary, "output redirection") ||
		!strings.Contains(summary, "extra commands") {
		t.Fatalf("suppressed verification commands should produce targeted missing summary, got (%q, %q)", status, summary)
	}
}
func TestSummarizeSubAgentVerificationCapsFailedCommands(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: false,
			Summary:   fmt.Sprintf("failure %02d\\nmore output", i),
		})
	}

	status, summary := summarizeSubAgentVerification([]string{"main.go"}, commands, 0)
	if status != "failed" {
		t.Fatalf("status = %q, want failed; summary=%q", status, summary)
	}
	if strings.Contains(summary, "go test ./pkg/06") {
		t.Fatalf("failed verification summary should be capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 2 条未通过命令未展开") {
		t.Fatalf("expected remaining failed command count, got %q", summary)
	}
}

func TestSummarizeSubAgentVerificationPrioritizesActionableFailureDiagnostics(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: false,
			Summary:   "command exited with code 1",
			seq:       uint64(i + 1),
		})
	}
	commands[len(commands)-1].Summary = "[stderr] src/main.go:12: error: missing symbol\ncommand exited with code 1"

	status, summary := summarizeSubAgentVerification([]string{"main.go"}, commands, 0)
	if status != codingSubAgentQualityFailed {
		t.Fatalf("status = %q, want failed; summary=%q", status, summary)
	}
	if !strings.Contains(summary, "go test ./pkg/06") || !strings.Contains(summary, "src/main.go:12: error: missing symbol") {
		t.Fatalf("failed verification summary should keep actionable late diagnostic, got %q", summary)
	}
}

func TestAppendSubAgentVerificationSummary(t *testing.T) {
	summary := appendSubAgentVerificationSummary("完成", "missing", "没有运行 bash 验证命令。")
	if !strings.Contains(summary, "## 验证状态") || !strings.Contains(summary, "MISSING") {
		t.Fatalf("unexpected verification summary: %s", summary)
	}
}

func TestCountExistingSubAgentModifiedFiles(t *testing.T) {
	got := countExistingSubAgentModifiedFiles(
		[]string{"new.go", "existing.go", "existing.go", "also_existing.go"},
		[]string{"new.go"},
	)
	if got != 2 {
		t.Fatalf("existing modified count = %d, want 2", got)
	}

	got = countExistingSubAgentModifiedFiles([]string{"new.go"}, []string{"new.go"})
	if got != 0 {
		t.Fatalf("created-only modified count = %d, want 0", got)
	}
}

func TestApplySubAgentExplorationOutcomeFailsPassedTaskWhenExistingEditsLackExploration(t *testing.T) {
	status, errMsg := applySubAgentExplorationOutcome(TaskExecPassed, "", codingSubAgentQualityMissing, "no exploration", 1)
	if status != TaskExecFailed || errMsg != "no exploration" {
		t.Fatalf("missing exploration should fail existing-file edits, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentExplorationOutcome(TaskExecPassed, "", codingSubAgentQualityMissing, "created only", 0)
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("created-only edits should not require prior exploration, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentExplorationOutcome(TaskExecPassed, "", codingSubAgentQualityReadOnly, "read file", 1)
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("read-only exploration should satisfy exploration gate, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentExplorationOutcome(TaskExecFailed, "model error", codingSubAgentQualityMissing, "no exploration", 1)
	if status != TaskExecFailed || errMsg != "model error" {
		t.Fatalf("existing failure should be preserved, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentExplorationOutcome(TaskExecPassed, "", codingSubAgentQualityMissing, "", 1)
	if status != TaskExecFailed || !strings.Contains(errMsg, "no exploration before editing existing files") {
		t.Fatalf("missing exploration should use default diagnostic, got status=%s err=%q", status, errMsg)
	}
}
func TestApplySubAgentVerificationOutcome(t *testing.T) {
	status, errMsg := applySubAgentVerificationOutcome(TaskExecPassed, "", "failed", "go test failed")
	if status != TaskExecFailed || errMsg != "go test failed" {
		t.Fatalf("expected failed verification to fail task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentVerificationOutcome(TaskExecPassed, "", "missing", "no commands")
	if status != TaskExecFailed || errMsg != "no commands" {
		t.Fatalf("missing verification should fail passed task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentVerificationOutcome(TaskExecPassed, "", "missing", "")
	if status != TaskExecFailed || !strings.Contains(errMsg, "no verification command") {
		t.Fatalf("missing verification should use default diagnostic, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentVerificationOutcome(TaskExecPassed, "", "not_needed", "no changes")
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("not-needed verification should not fail task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentVerificationOutcome(TaskExecFailed, "model error", "failed", "go test failed")
	if status != TaskExecFailed || errMsg != "model error" {
		t.Fatalf("existing task failure should be preserved, got status=%s err=%q", status, errMsg)
	}

	longVerification := strings.Repeat("verification failed\n", codingSubAgentErrorSummaryMaxRunes)
	status, errMsg = applySubAgentVerificationOutcome(TaskExecPassed, "", "failed", longVerification)
	if status != TaskExecFailed || !strings.Contains(errMsg, "截断") {
		t.Fatalf("failed verification should compact long errors, got status=%s err=%q", status, errMsg)
	}
}

func TestSubAgentVerificationOutcomeStatusTreatsMissingAsFailed(t *testing.T) {
	if got := subAgentVerificationOutcomeStatus(codingSubAgentQualityMissing); got != codingSubAgentQualityFailed {
		t.Fatalf("missing verification outcome = %q, want %q", got, codingSubAgentQualityFailed)
	}
	status, errMsg := applySubAgentVerificationOutcome(TaskExecPassed, "", subAgentVerificationOutcomeStatus(codingSubAgentQualityMissing), "no verification command")
	if status != TaskExecFailed || errMsg != "no verification command" {
		t.Fatalf("missing verification should fail passed task after outcome mapping, got status=%s err=%q", status, errMsg)
	}
	if got := subAgentVerificationOutcomeStatus(codingSubAgentQualityNotNeeded); got != codingSubAgentQualityNotNeeded {
		t.Fatalf("not-needed verification outcome = %q, want %q", got, codingSubAgentQualityNotNeeded)
	}
	if got := subAgentVerificationOutcomeStatus(codingSubAgentQualityPassed); got != codingSubAgentQualityPassed {
		t.Fatalf("passed verification outcome = %q, want %q", got, codingSubAgentQualityPassed)
	}
}

func TestApplySubAgentGuardrailOutcomeFailsPassedTask(t *testing.T) {
	violations := []CodingSubAgentGuardrailViolation{
		{Tool: "bash", Category: codingSubAgentGuardrailCategoryGit, Command: "git reset --hard", Summary: "blocked destructive git command"},
		{Tool: "read_file", Category: codingSubAgentGuardrailCategoryScope, Path: "../secret.txt", Summary: "outside project"},
	}
	status, errMsg := applySubAgentGuardrailOutcome(TaskExecPassed, "", violations)
	if status != TaskExecFailed || !strings.Contains(errMsg, "2 guardrail block") || !strings.Contains(errMsg, "blocked destructive git command") {
		t.Fatalf("guardrail outcome should fail passed task with compact diagnostic, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentGuardrailOutcome(TaskExecFailed, "model error", violations)
	if status != TaskExecFailed || !strings.Contains(errMsg, "model error") || !strings.Contains(errMsg, "2 guardrail block") {
		t.Fatalf("guardrail outcome should append to existing failure, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentGuardrailOutcome(TaskExecFailed, "", violations)
	if status != TaskExecFailed || !strings.Contains(errMsg, "2 guardrail block") {
		t.Fatalf("guardrail outcome should provide diagnostic for existing failed status with empty error, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentGuardrailOutcome(TaskExecPassed, "", nil)
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("empty guardrail outcome should not fail task, got status=%s err=%q", status, errMsg)
	}
}

func TestApplySubAgentDiffOutcomeFailsPassedTaskWhenModifiedDiffMissing(t *testing.T) {
	status, errMsg := applySubAgentDiffOutcome(TaskExecPassed, "", false, "git diff failed", 1)
	if status != TaskExecFailed || errMsg != "git diff failed" {
		t.Fatalf("missing diff check should fail passed task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentDiffOutcome(TaskExecPassed, "", false, "", 1)
	if status != TaskExecFailed || !strings.Contains(errMsg, "git diff self-check") {
		t.Fatalf("missing diff check should use default diagnostic, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentDiffOutcome(TaskExecPassed, "", false, "no diff", 0)
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("unchanged task should not require diff check, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentDiffOutcome(TaskExecFailed, "model error", false, "git diff failed", 1)
	if status != TaskExecFailed || errMsg != "model error" {
		t.Fatalf("diff outcome should not replace existing failure, got status=%s err=%q", status, errMsg)
	}
}

func TestLimitSubAgentResultAuditSlices(t *testing.T) {
	var files []string
	for i := 0; i < codingSubAgentResultFilesMax+3; i++ {
		files = append(files, fmt.Sprintf("file-%03d.go", i))
	}
	limitedFiles := limitSubAgentStringSlice(files, codingSubAgentResultFilesMax)
	if len(limitedFiles) != codingSubAgentResultFilesMax {
		t.Fatalf("limited files = %d, want %d", len(limitedFiles), codingSubAgentResultFilesMax)
	}
	if limitedFiles[len(limitedFiles)-1] != fmt.Sprintf("file-%03d.go", codingSubAgentResultFilesMax-1) {
		t.Fatalf("expected earliest files to be kept, got last=%q", limitedFiles[len(limitedFiles)-1])
	}

	var commands []CodingSubAgentCommandResult
	var searches []CodingSubAgentSearchResult
	var guardrails []CodingSubAgentGuardrailViolation
	var dynamicTools []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{Command: fmt.Sprintf("cmd-%03d", i), Succeeded: true})
		searches = append(searches, CodingSubAgentSearchResult{Query: fmt.Sprintf("query-%03d", i), Succeeded: true, Summary: "found match"})
		guardrails = append(guardrails, CodingSubAgentGuardrailViolation{Summary: fmt.Sprintf("guard-%03d", i)})
		dynamicTools = append(dynamicTools, CodingSubAgentDynamicToolResult{Tool: "call_mcp_tool", Name: fmt.Sprintf("server/tool-%03d", i), Succeeded: true})
	}
	commands[len(commands)-1].Succeeded = false
	searches[len(searches)-1].Succeeded = false
	searches[len(searches)-1].Summary = "ripgrep failed: invalid pattern"
	guardrails[len(guardrails)-1].Category = codingSubAgentGuardrailCategoryGit
	guardrails[len(guardrails)-1].Command = "git reset --hard"
	dynamicTools[len(dynamicTools)-1].Succeeded = false
	limitedCommands := limitSubAgentCommandResults(commands, codingSubAgentResultAuditMax)
	if len(limitedCommands) != codingSubAgentResultAuditMax {
		t.Fatalf("limited commands = %d, want %d", len(limitedCommands), codingSubAgentResultAuditMax)
	}
	if limitedCommands[0].Command != fmt.Sprintf("cmd-%03d", codingSubAgentResultAuditMax+1) || limitedCommands[0].Succeeded {
		t.Fatalf("late failed command should be preserved first, got %#v", limitedCommands[0])
	}
	limitedSearches := limitSubAgentSearchResults(searches, codingSubAgentResultAuditMax)
	if len(limitedSearches) != codingSubAgentResultAuditMax {
		t.Fatalf("limited searches = %d, want %d", len(limitedSearches), codingSubAgentResultAuditMax)
	}
	if limitedSearches[0].Query != fmt.Sprintf("query-%03d", codingSubAgentResultAuditMax+1) || limitedSearches[0].Succeeded {
		t.Fatalf("late failed search should be preserved first, got %#v", limitedSearches[0])
	}

	var successfulSearches []CodingSubAgentSearchResult
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		successfulSearches = append(successfulSearches, CodingSubAgentSearchResult{Query: fmt.Sprintf("pass-query-%03d", i), Succeeded: true, Summary: "found match", seq: uint64(i + 1)})
	}
	limitedSuccessfulSearches := limitSubAgentSearchResults(successfulSearches, codingSubAgentResultAuditMax)
	if limitedSuccessfulSearches[0].Query != fmt.Sprintf("pass-query-%03d", codingSubAgentResultAuditMax+1) || limitedSuccessfulSearches[len(limitedSuccessfulSearches)-1].Query != "pass-query-002" {
		t.Fatalf("same-status search audit should keep latest entries first, got %#v", limitedSuccessfulSearches)
	}
	limitedGuardrails := limitSubAgentGuardrailViolations(guardrails, codingSubAgentResultAuditMax)
	if len(limitedGuardrails) != codingSubAgentResultAuditMax {
		t.Fatalf("limited guardrails = %d, want %d", len(limitedGuardrails), codingSubAgentResultAuditMax)
	}
	if limitedGuardrails[0].Command != "git reset --hard" || limitedGuardrails[0].Category != codingSubAgentGuardrailCategoryGit {
		t.Fatalf("late high-risk guardrail should be preserved first, got %#v", limitedGuardrails[0])
	}
	limitedDynamicTools := limitSubAgentDynamicToolResults(dynamicTools, codingSubAgentResultAuditMax)
	if len(limitedDynamicTools) != codingSubAgentResultAuditMax {
		t.Fatalf("limited dynamic tools = %d, want %d", len(limitedDynamicTools), codingSubAgentResultAuditMax)
	}
	if limitedDynamicTools[0].Name != fmt.Sprintf("server/tool-%03d", codingSubAgentResultAuditMax+1) || limitedDynamicTools[0].Succeeded {
		t.Fatalf("late failed dynamic tool should be preserved first, got %#v", limitedDynamicTools[0])
	}

	allPassingCommands := make([]CodingSubAgentCommandResult, 0, codingSubAgentResultAuditMax+2)
	allPassingDynamicTools := make([]CodingSubAgentDynamicToolResult, 0, codingSubAgentResultAuditMax+2)
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		allPassingCommands = append(allPassingCommands, CodingSubAgentCommandResult{Command: fmt.Sprintf("pass-cmd-%03d", i), Succeeded: true})
		allPassingDynamicTools = append(allPassingDynamicTools, CodingSubAgentDynamicToolResult{Tool: "call_mcp_tool", Name: fmt.Sprintf("pass-tool-%03d", i), Succeeded: true, Summary: "ok"})
	}
	limitedPassingCommands := limitSubAgentCommandResults(allPassingCommands, codingSubAgentResultAuditMax)
	if limitedPassingCommands[0].Command != "pass-cmd-002" || limitedPassingCommands[len(limitedPassingCommands)-1].Command != fmt.Sprintf("pass-cmd-%03d", codingSubAgentResultAuditMax+1) {
		t.Fatalf("all-passing command audit should keep latest entries in order, got %#v", limitedPassingCommands)
	}
	limitedPassingDynamicTools := limitSubAgentDynamicToolResults(allPassingDynamicTools, codingSubAgentResultAuditMax)
	if limitedPassingDynamicTools[0].Name != "pass-tool-002" || limitedPassingDynamicTools[len(limitedPassingDynamicTools)-1].Name != fmt.Sprintf("pass-tool-%03d", codingSubAgentResultAuditMax+1) {
		t.Fatalf("all-passing dynamic tool audit should keep latest entries in order, got %#v", limitedPassingDynamicTools)
	}

	var commandsWithEmptySuccess []CodingSubAgentCommandResult
	var toolsWithEmptySuccess []CodingSubAgentDynamicToolResult
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		commandsWithEmptySuccess = append(commandsWithEmptySuccess, CodingSubAgentCommandResult{Command: fmt.Sprintf("pytest tests/%03d", i), Succeeded: true, Summary: "1 passed"})
		toolsWithEmptySuccess = append(toolsWithEmptySuccess, CodingSubAgentDynamicToolResult{Tool: "call_mcp_tool", Name: fmt.Sprintf("empty-tool-%03d", i), Succeeded: true, Summary: "ok"})
	}
	commandsWithEmptySuccess[0].Summary = "no tests collected"
	toolsWithEmptySuccess[0].Summary = "(无输出)"
	limitedEmptyCommands := limitSubAgentCommandResults(commandsWithEmptySuccess, codingSubAgentResultAuditMax)
	if limitedEmptyCommands[0].Command != "pytest tests/000" {
		t.Fatalf("empty-success command audit should preserve problem entry first, got %#v", limitedEmptyCommands)
	}
	limitedEmptyDynamicTools := limitSubAgentDynamicToolResults(toolsWithEmptySuccess, codingSubAgentResultAuditMax)
	if limitedEmptyDynamicTools[0].Name != "empty-tool-000" {
		t.Fatalf("empty-success dynamic tool audit should preserve problem entry first, got %#v", limitedEmptyDynamicTools)
	}

	var sameRiskGuardrails []CodingSubAgentGuardrailViolation
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		sameRiskGuardrails = append(sameRiskGuardrails, CodingSubAgentGuardrailViolation{
			Category: codingSubAgentGuardrailCategoryCommand,
			Command:  fmt.Sprintf("blocked-cmd-%03d", i),
			seq:      uint64(i + 1),
		})
	}
	limitedSameRiskGuardrails := limitSubAgentGuardrailViolations(sameRiskGuardrails, codingSubAgentResultAuditMax)
	if limitedSameRiskGuardrails[0].Command != fmt.Sprintf("blocked-cmd-%03d", codingSubAgentResultAuditMax+1) || limitedSameRiskGuardrails[len(limitedSameRiskGuardrails)-1].Command != "blocked-cmd-002" {
		t.Fatalf("same-risk guardrail audit should keep latest entries first, got %#v", limitedSameRiskGuardrails)
	}
}

func TestCollectSubAgentAuditKeepsUnboundedGateInputs(t *testing.T) {
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}}
	for i := 0; i < codingSubAgentResultFilesMax+3; i++ {
		path := fmt.Sprintf("file-%03d.go", i)
		cb.trackFile(path)
	}
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		cb.trackCommandResult(map[string]interface{}{"command": fmt.Sprintf("go test ./pkg/%03d", i)}, "ok", true)
		cb.trackSearchResult("ripgrep", map[string]interface{}{"pattern": fmt.Sprintf("symbol-%03d", i)}, "match", true)
		cb.trackGuardrailViolation("bash", map[string]interface{}{"command": fmt.Sprintf("git reset --hard %03d", i)}, "blocked")
	}

	audit := collectSubAgentAudit(cb)
	if len(audit.AllFilesModified) != codingSubAgentResultFilesMax+3 || len(audit.FilesModified) != codingSubAgentResultFilesMax {
		t.Fatalf("files audit lengths = all %d limited %d", len(audit.AllFilesModified), len(audit.FilesModified))
	}
	if len(audit.AllCommandsRun) != codingSubAgentResultAuditMax+2 || len(audit.CommandsRun) != codingSubAgentResultAuditMax {
		t.Fatalf("command audit lengths = all %d limited %d", len(audit.AllCommandsRun), len(audit.CommandsRun))
	}
	if len(audit.AllSearchesRun) != codingSubAgentResultAuditMax+2 || len(audit.SearchesRun) != codingSubAgentResultAuditMax {
		t.Fatalf("search audit lengths = all %d limited %d", len(audit.AllSearchesRun), len(audit.SearchesRun))
	}
	if audit.SearchesRun[0].Query != fmt.Sprintf("symbol-%03d", codingSubAgentResultAuditMax+1) {
		t.Fatalf("search audit should keep latest tracked search first, got %#v", audit.SearchesRun)
	}
	if len(audit.AllGuardrailViolations) != codingSubAgentResultAuditMax+2 || len(audit.GuardrailViolations) != codingSubAgentResultAuditMax {
		t.Fatalf("guardrail audit lengths = all %d limited %d", len(audit.AllGuardrailViolations), len(audit.GuardrailViolations))
	}
	if audit.GuardrailViolations[0].Command != fmt.Sprintf("git reset --hard %03d", codingSubAgentResultAuditMax+1) {
		t.Fatalf("guardrail audit should keep latest tracked block first, got %#v", audit.GuardrailViolations)
	}
}

func TestCodingSubAgentLogsFailedOperationWithRedactedArgs(t *testing.T) {
	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: t.TempDir()},
		task:     &TaskItem{Index: 7, Title: "write failure"},
	}
	args, _ := json.Marshal(map[string]string{"path": "out.txt", "content": "secret-token-value" + strings.Repeat("x", writeFileMaxSize)})
	result := cb.executeToolWithOutcome("write_file", string(args))
	if result.Outcome == codingToolOutcomeSuccess {
		t.Fatal("expected write_file to fail")
	}
	logText := buf.String()
	if !strings.Contains(logText, "[coding-subagent] operation failed") || !strings.Contains(logText, "tool=write_file") || !strings.Contains(logText, "outcome=failed") {
		t.Fatalf("missing failure log details: %s", logText)
	}
	if strings.Contains(logText, "secret-token-value") {
		t.Fatalf("log should redact content argument, got: %s", logText)
	}
	if !strings.Contains(logText, "[redacted") {
		t.Fatalf("log should include redaction marker, got: %s", logText)
	}

	argsText := compactCodingSubAgentArgsLogText(`{"token":"abc123","password":"pw","path":"main.go","file_content":"full text","headers":{"authorization":"Bearer secret-token","x-api-key":"header-key"},"items":[{"api_key":"nested-key"}]}`, 500)
	if strings.Contains(argsText, "abc123") || strings.Contains(argsText, "pw") || strings.Contains(argsText, "full text") || strings.Contains(argsText, "secret-token") || strings.Contains(argsText, "header-key") || strings.Contains(argsText, "nested-key") {
		t.Fatalf("sensitive arg keys should be redacted, got: %s", argsText)
	}

	freeform := compactCodingSubAgentLogText(`failed: token=abc123 Authorization: Bearer secret-token api-key: header-key {"access_token":"json-token","password":"json-password"} Authorization: Basic basic-token path=D:\workprj\aicoder\main.go`, 500)
	if strings.Contains(freeform, "abc123") || strings.Contains(freeform, "secret-token") || strings.Contains(freeform, "header-key") || strings.Contains(freeform, "json-token") || strings.Contains(freeform, "json-password") || strings.Contains(freeform, "basic-token") {
		t.Fatalf("freeform log text should redact secret-looking values, got: %s", freeform)
	}
	if !strings.Contains(freeform, `path=D:\workprj\aicoder\main.go`) {
		t.Fatalf("freeform log text should preserve non-secret diagnostics, got: %s", freeform)
	}

	quotedCLI := compactCodingSubAgentLogText(`failed: --token "abc def" /password 'one two' --api-key=key-123 --secret secret-value`, 500)
	for _, leaked := range []string{"abc def", "one two", "key-123", "secret-value"} {
		if strings.Contains(quotedCLI, leaked) {
			t.Fatalf("freeform CLI flag log text should redact quoted/inline secret %q, got: %s", leaked, quotedCLI)
		}
	}
	if !strings.Contains(quotedCLI, `--token "[redacted]"`) || !strings.Contains(quotedCLI, `/password '[redacted]'`) || !strings.Contains(quotedCLI, "--api-key=[redacted]") || !strings.Contains(quotedCLI, "--secret [redacted]") {
		t.Fatalf("freeform CLI flag log text should keep useful flag names with redaction markers, got: %s", quotedCLI)
	}

	invalidArgsText := compactCodingSubAgentArgsLogText(`{"path":"out.go","content":"secret-token-value`+strings.Repeat("x", 1024), 500)
	if strings.Contains(invalidArgsText, "secret-token-value") || strings.Contains(invalidArgsText, strings.Repeat("x", 128)) {
		t.Fatalf("invalid JSON arg text should redact content, got: %s", invalidArgsText)
	}
	if !strings.Contains(invalidArgsText, "invalid JSON redacted") || !strings.Contains(invalidArgsText, "content_field=true") {
		t.Fatalf("invalid JSON arg text should include redacted diagnostic summary, got: %s", invalidArgsText)
	}
}

func TestCodingSubAgentSkipsDiagnosticProbeFailureLog(t *testing.T) {
	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Index: 3, Title: "probe compiler"},
	}
	args, _ := json.Marshal(map[string]string{"command": `g++ --version 2>&1; cmake --version 2>&1`})
	logCodingSubAgentOperationFailure(cb, "bash", string(args), codingToolExecutionResult{
		Text:    "PowerShell exception: 无法将“g++”项识别为 cmdlet",
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	if got := buf.String(); got != "" {
		t.Fatalf("diagnostic probe failure should not be written to the main log, got %q", got)
	}
	cb.trackCommandResult(map[string]interface{}{"command": `g++ --version 2>&1; cmake --version 2>&1`}, "PowerShell exception: 无法将“g++”项识别为 cmdlet", false)
	if len(cb.commandsRun) != 1 || cb.commandsRun[0].Command != `g++ --version 2>&1; cmake --version 2>&1` || cb.commandsRun[0].Succeeded {
		t.Fatalf("diagnostic probe failure should remain in local command audit, got %#v", cb.commandsRun)
	}
	unresolved := unresolvedFailedSubAgentCommands(cb.commandsRun)
	if len(unresolved) != 1 || unresolved[0].Command != `g++ --version 2>&1; cmake --version 2>&1` {
		t.Fatalf("diagnostic probe without later verification should remain unresolved, got %#v", unresolved)
	}

	logCodingSubAgentOperationFailure(cb, "bash", `{"command":"clang++ -print-search-dirs"}`, codingToolExecutionResult{
		Text:    "clang++: command not found",
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	if got := buf.String(); got != "" {
		t.Fatalf("compiler diagnostic probe failure should not be written to the main log, got %q", got)
	}

	logCodingSubAgentOperationFailure(cb, "bash", `{"command":"node --version && npm test"}`, codingToolExecutionResult{
		Text:    "FAIL src/app.test.ts: expected true to be false",
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	logText := buf.String()
	if !strings.Contains(logText, "[coding-subagent] operation failed") || !strings.Contains(logText, "node --version") || !strings.Contains(logText, "npm test") || !strings.Contains(logText, "FAIL src/app.test.ts") {
		t.Fatalf("mixed diagnostic plus verification failure should still be logged, got %q", logText)
	}
	buf.Reset()

	logCodingSubAgentOperationFailure(cb, "bash", `{"command":"where.exe cl.exe"}`, codingToolExecutionResult{
		Text:    `FAIL at D:\test\test_hello.cpp:11: CHECK (result == "Hello, World!")`,
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	logText = buf.String()
	if !strings.Contains(logText, "[coding-subagent] operation failed") || !strings.Contains(logText, "CHECK (result") {
		t.Fatalf("diagnostic-looking command with test assertion failure should still be logged, got %q", logText)
	}
	buf.Reset()

	logCodingSubAgentOperationFailure(cb, "bash", `{"command":"cmake --build build"}`, codingToolExecutionResult{
		Text:    "fatal error LNK1120: unresolved externals",
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	logText = buf.String()
	if !strings.Contains(logText, "[coding-subagent] operation failed") || !strings.Contains(logText, "cmake --build build") || !strings.Contains(logText, "LNK1120") {
		t.Fatalf("real bash failure should still be logged, got %q", logText)
	}

	buf.Reset()
	logCodingSubAgentOperationFailure(cb, "bash", `{"command":"git reset --hard HEAD; where cl.exe"}`, codingToolExecutionResult{
		Text:    "blocked destructive command",
		Outcome: codingToolOutcomeFailed,
	}, time.Millisecond)
	if logText := buf.String(); !strings.Contains(logText, "[coding-subagent] operation failed") || !strings.Contains(logText, "git reset --hard HEAD") {
		t.Fatalf("mixed real command and diagnostic probe should still be logged, got %q", logText)
	}
}

func TestCodingSubAgentListDirectoryFailureHasErrorOutcome(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Index: 1, Title: "list missing directory"},
	}
	result := cb.executeToolWithOutcome("list_directory", `{"path":"missing"}`)
	if result.Outcome != codingToolOutcomeFailed {
		t.Fatalf("list_directory outcome = %q, want failed; result=%s", result.Outcome, result.Text)
	}
}

func TestCodingSubAgentEnsureFinalGitDiffReusesFreshDiff(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent:       &CodingSubAgent{projectPath: t.TempDir()},
		gitDiffChecked: true,
		lastGitDiff:    "diff --git a/main.go b/main.go\n+fresh",
		lastEditSeq:    2,
		lastDiffSeq:    3,
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if !checked || !strings.Contains(summary, "+fresh") {
		t.Fatalf("fresh diff should be reused, checked=%v summary=%q", checked, summary)
	}
}

func TestCodingSubAgentEnsureFinalGitDiffReusesFreshUntrackedCreatedDiff(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent:       &CodingSubAgent{projectPath: t.TempDir()},
		gitDiffChecked: true,
		lastGitDiff:    "Untracked files:\n- new_feature.go",
		lastEditSeq:    2,
		lastDiffSeq:    3,
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"new_feature.go"}, []string{"new_feature.go"})
	if !checked || !strings.Contains(summary, "new_feature.go") {
		t.Fatalf("fresh untracked created diff should be reused, checked=%v summary=%q", checked, summary)
	}
}

func TestCodingSubAgentEnsureFinalGitDiffRejectsFreshUnrelatedUntrackedDiff(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent:       &CodingSubAgent{projectPath: t.TempDir()},
		gitDiffChecked: true,
		lastGitDiff:    "Untracked files:\n- scratch.txt",
		lastEditSeq:    2,
		lastDiffSeq:    3,
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if checked {
		t.Fatalf("fresh unrelated untracked diff should not be reused, summary=%q", summary)
	}
	if !strings.Contains(summary, "未跟踪文件") || !strings.Contains(summary, "缺少本任务改动证据") {
		t.Fatalf("unrelated cached untracked diff should explain missing task evidence, got %q", summary)
	}
	if cb.gitDiffChecked {
		t.Fatal("gitDiffChecked should be reset after rejecting cached unrelated untracked diff")
	}
}

func TestCodingSubAgentEnsureFinalGitDiffRejectsStaleDiff(t *testing.T) {
	missingProject := filepath.Join(t.TempDir(), "missing-project")
	cb := &codingSubAgentCallbacks{
		subagent:       &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: missingProject},
		gitDiffChecked: true,
		lastGitDiff:    "diff --git a/main.go b/main.go\n+stale",
		lastEditSeq:    5,
		lastDiffSeq:    4,
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if checked {
		t.Fatalf("stale diff should force a new final diff check, got checked=true summary=%q", summary)
	}
	if strings.Contains(summary, "+stale") {
		t.Fatalf("stale diff summary should not be reused, got %q", summary)
	}
}

func TestCodingSubAgentGitDiffNonGitRepoReturnsSoftSuccess(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: project},
		task:     &TaskItem{Index: 3, Title: "diff non git repo"},
	}

	result := cb.executeToolWithOutcome("git_diff", `{}`)
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("non-git git_diff outcome = %q, want success; text=%q", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "not a Git repository") || !strings.Contains(result.Text, "coding audit") {
		t.Fatalf("non-git git_diff should explain soft skip, got %q", result.Text)
	}
	if !cb.gitDiffChecked {
		t.Fatal("gitDiffChecked should be true after soft non-git git_diff")
	}
}

func TestCodingSubAgentFinalGitDiffNonGitRepoMarksChecked(t *testing.T) {
	project := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: project},
		task:     &TaskItem{Index: 3, Title: "diff non git repo"},
	}

	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if !checked {
		t.Fatalf("non-git project should mark diff self-check as softly handled; summary=%q", summary)
	}
	if !strings.Contains(summary, "not a Git repository") || !strings.Contains(summary, "coding audit") {
		t.Fatalf("non-git diff soft skip should explain fallback evidence, got %q", summary)
	}
	if !cb.gitDiffChecked {
		t.Fatal("gitDiffChecked should be true after non-git soft diff self-check")
	}
}

func TestCodingSubAgentFinalGitDiffRejectsEmptyDiffAfterTrackedModification(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	runGitForTest(t, "", "init", repo)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitForTest(t, repo, "add", "main.go")
	runGitForTest(t, repo, "-c", "user.email=a@b.test", "-c", "user.name=test", "commit", "-m", "init")

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: repo},
		task:     &TaskItem{Index: 5, Title: "diff empty after revert"},
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if checked {
		t.Fatalf("empty final diff after a tracked modification should fail, summary=%q", summary)
	}
	if !strings.Contains(summary, "git diff 无输出") || !strings.Contains(summary, "最终 diff 为空") {
		t.Fatalf("empty final diff should explain missing evidence, got %q", summary)
	}
	if cb.gitDiffChecked {
		t.Fatal("gitDiffChecked should be reset after empty final diff")
	}
}

func TestCodingSubAgentFinalGitDiffRejectsUnrelatedUntrackedFiles(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	runGitForTest(t, "", "init", repo)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitForTest(t, repo, "add", "main.go")
	runGitForTest(t, repo, "-c", "user.email=a@b.test", "-c", "user.name=test", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("unrelated\n"), 0644); err != nil {
		t.Fatalf("write unrelated untracked file: %v", err)
	}

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: repo},
		task:     &TaskItem{Index: 7, Title: "diff unrelated untracked"},
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if checked {
		t.Fatalf("unrelated untracked file should not satisfy final diff self-check, summary=%q", summary)
	}
	if !strings.Contains(summary, "未跟踪文件") || !strings.Contains(summary, "缺少本任务改动证据") {
		t.Fatalf("unrelated untracked failure should explain missing task evidence, got %q", summary)
	}
}
func TestCodingSubAgentFinalGitDiffIncludesUntrackedCreatedFiles(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	runGitForTest(t, "", "init", repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitForTest(t, repo, "add", "README.md")
	runGitForTest(t, repo, "-c", "user.email=a@b.test", "-c", "user.name=test", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "new_feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: repo},
		task:     &TaskItem{Index: 6, Title: "diff untracked new file"},
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"new_feature.go"}, []string{"new_feature.go"})
	if !checked {
		t.Fatalf("untracked created file should satisfy final diff self-check, summary=%q", summary)
	}
	if !strings.Contains(summary, "Untracked files") || !strings.Contains(summary, "new_feature.go") {
		t.Fatalf("final diff summary should include untracked file evidence, got %q", summary)
	}
}
func TestCodingSubAgentGitDiffSupportsGitFileWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "worktree")
	runGitForTest(t, "", "init", repo)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitForTest(t, repo, "add", "main.go")
	runGitForTest(t, repo, "-c", "user.email=a@b.test", "-c", "user.name=test", "commit", "-m", "init")
	runGitForTest(t, repo, "worktree", "add", worktree, "-b", "subagent-test-worktree")
	if err := os.WriteFile(filepath.Join(worktree, "main.go"), []byte("package main\n\nfunc changed() {}\n"), 0644); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}
	if info, err := os.Stat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
		t.Fatalf("worktree .git should be a file, info=%v err=%v", info, err)
	}

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: worktree},
		task:     &TaskItem{Index: 4, Title: "diff git file worktree"},
	}
	result := cb.executeToolWithOutcome("git_diff", `{}`)
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("git_diff worktree outcome = %q, result=%s", result.Outcome, result.Text)
	}
	if !strings.Contains(result.Text, "func changed") {
		t.Fatalf("git_diff worktree should include local changes, got %q", result.Text)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitForTest(t), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func gitForTest(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("git"); err == nil {
		return path
	}
	if runtime.GOOS == "windows" {
		for _, path := range []string{
			`C:\Program Files\Git\cmd\git.exe`,
			`C:\Program Files\Git\bin\git.exe`,
			`C:\Program Files (x86)\Git\cmd\git.exe`,
			`C:\Program Files (x86)\Git\bin\git.exe`,
		} {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	t.Fatalf("git executable not found in PATH")
	return ""
}

func TestCodingSubAgentFinalGitDiffFailureDoesNotMarkChecked(t *testing.T) {
	missingProject := filepath.Join(t.TempDir(), "missing-project")
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{}, projectPath: missingProject},
		task:     &TaskItem{Index: 2, Title: "diff outside repo"},
	}
	checked, summary := cb.ensureFinalGitDiff([]string{"main.go"}, nil)
	if checked {
		t.Fatalf("git diff failure should not mark diff checked; summary=%q", summary)
	}
	if strings.TrimSpace(summary) == "" {
		t.Fatal("git diff failure should return diagnostic summary")
	}
	if cb.gitDiffChecked {
		t.Fatal("gitDiffChecked should remain false after failed final diff")
	}
}
