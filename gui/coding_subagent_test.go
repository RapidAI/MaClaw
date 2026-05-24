package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

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
	if !strings.Contains(prompt, "ripgrep") || !strings.Contains(prompt, "Glob") {
		t.Error("prompt should guide search before reading/editing")
	}
	if !strings.Contains(prompt, "git_diff") {
		t.Error("prompt should require git_diff self-check")
	}
	if !strings.Contains(prompt, "Single-task contract") || !strings.Contains(prompt, "Avoid broad refactors") {
		t.Error("prompt should contain explicit single-task scope contract")
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
	if !strings.Contains(prompt, "Git commands that rewrite") || !strings.Contains(prompt, "merge") || !strings.Contains(prompt, "Remove-Item -Recurse/-r/-rf") {
		t.Error("prompt should describe expanded command guardrails")
	}
	if !strings.Contains(prompt, "shell helpers") || !strings.Contains(prompt, "Set-Content") || !strings.Contains(prompt, "writeFileSync") || !strings.Contains(prompt, "Python open") {
		t.Error("prompt should describe shell file mutation guardrails")
	}
	if !strings.Contains(prompt, "working_dir 必须在项目路径内") {
		t.Error("prompt should describe bash working directory boundary")
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
	// Should contain previous outputs.
	if !strings.Contains(prompt, "src/player.h") {
		t.Error("prompt should contain previous task outputs")
	}
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
	if !strings.Contains(userMsg, "还有 3 项未展开") {
		t.Fatalf("acceptance criteria should report remaining count, got %q", userMsg)
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
	if len([]rune(userMsg)) > codingSubAgentTaskDescriptionMaxRunes+codingSubAgentTaskTitleMaxRunes+200 {
		t.Fatalf("task user message too long: %d", len([]rune(userMsg)))
	}
}

func TestBuildCodingSubAgentSystemPromptCapsPreviousOutputs(t *testing.T) {
	var prevOutputs []string
	for i := 0; i < codingSubAgentPrevOutputsMax+4; i++ {
		prevOutputs = append(prevOutputs, fmt.Sprintf("src/previous_%02d.go (modified)", i))
	}

	prompt := buildCodingSubAgentSystemPrompt(&TaskItem{Index: 4, Title: "Next"}, "/project", "", "", prevOutputs)
	if strings.Contains(prompt, "src/previous_23.go") {
		t.Fatalf("previous outputs should be capped, got %q", prompt)
	}
	if !strings.Contains(prompt, "还有 4 项未展开") {
		t.Fatalf("previous outputs should report remaining count, got %q", prompt)
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

	success := executeCodingBash(map[string]interface{}{"command": successCmd}, nil)
	if success.Kind != codingCommandResultOK || success.ExitCode != 0 {
		t.Fatalf("success result = %#v", success)
	}
	failed := executeCodingBash(map[string]interface{}{"command": failCmd}, nil)
	if failed.Kind != codingCommandResultExitError || failed.ExitCode == 0 {
		t.Fatalf("failed result = %#v", failed)
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
		{Command: "go test ./gui", Succeeded: true},
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

	cb.emitQualitySummaryEvent("missing", "missing", false, []string{"main.go"}, nil, nil)

	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"event":"quality_summary"`) ||
		!strings.Contains(joined, `"phase":"result"`) ||
		!strings.Contains(joined, `"task_id":"T13"`) ||
		!strings.Contains(joined, `"outcome":"warning"`) ||
		!strings.Contains(joined, `"summary":"no exploration before edits; verification not run; diff not checked"`) ||
		!strings.Contains(joined, `"count":3`) {
		t.Fatalf("expected structured quality progress, got %#v", progress)
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
	if msg := cb.requireReadBeforeModify(path, "edit_file"); !strings.Contains(msg, "已变化") {
		t.Fatalf("expected external change warning, got %q", msg)
	}

	cb.refreshFileSnapshot(path)
	if msg := cb.requireReadBeforeModify(path, "edit_file"); msg != "" {
		t.Fatalf("expected modify to be allowed after snapshot refresh, got %q", msg)
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
	if msg := cb.requireReadBeforeWriteExisting(existing); msg == "" {
		t.Fatal("expected write_file on existing file to require read_file first")
	}

	cb.trackReadFile(existing)
	if msg := cb.requireReadBeforeWriteExisting(existing); msg != "" {
		t.Fatalf("expected write_file on existing file to be allowed after read_file, got %q", msg)
	}

	newFile := filepath.Join(dir, "new.txt")
	if msg := cb.requireReadBeforeWriteExisting(newFile); msg != "" {
		t.Fatalf("expected write_file on new file to be allowed, got %q", msg)
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
	spacedBashArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test ./...", "working_dir": "  gui  "})
	if got, _ := spacedBashArgs["working_dir"].(string); got != wantDir {
		t.Fatalf("spaced relative working_dir = %q, want %q", got, wantDir)
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
	if got, _ := normalArgs["timeout"].(float64); got != 90 {
		t.Fatalf("normal timeout = %v, want 90", got)
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
		"go test ./...; git reset --hard HEAD",
		"go test ./... && git reset --hard HEAD",
		"go test ./... || git checkout -- .",
		"(git reset --hard HEAD)",
		"bash -c touch src/a.go",
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
		"python -c \"from pathlib import Path; Path('src/a.go').write_text('x')\"",
		"python -c \"open('src/a.go','w').write('x')\"",
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
		"python -m pytest",
		"node --test",
		"npm run lint",
		"npx tsc --noEmit",
		"echo mkdir",
		"echo rm -rf build",
		"echo git reset --hard HEAD",
		"Write-Output Remove-Item -Recurse",
		"Write-Output Set-Content",
		"go test ./... | Select-String FAIL",
		"python -c \"print('touch src/a.go')\"",
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
		{name: "host", tool: "read_file", result: "coding subagent host tool handler is unavailable", category: "host"},
	}
	for _, tc := range cases {
		if got := classifyCodingGuardrailCategory(tc.tool, tc.path, tc.command, tc.result).String(); got != tc.category {
			t.Fatalf("%s category = %q, want %q", tc.name, got, tc.category)
		}
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
	checked, summary := cb.ensureFinalGitDiff(nil)
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
	if strings.Contains(summary, "go test ./pkg/12") {
		t.Fatalf("command summary should be capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 3 条命令记录未展开") {
		t.Fatalf("expected remaining command count, got %q", summary)
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

func TestSummarizeSubAgentCommands(t *testing.T) {
	status, summary := summarizeSubAgentCommands(nil)
	if status != "none" || summary != "no bash commands run" {
		t.Fatalf("empty command summary = %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
		{Command: "npm test", Succeeded: true},
	})
	if status != "passed" || !strings.Contains(summary, "2 bash commands run, no failures") {
		t.Fatalf("passed command summary = %q, %q", status, summary)
	}

	status, summary = summarizeSubAgentCommands([]CodingSubAgentCommandResult{
		{Command: "go test ./gui", Succeeded: true},
		{Command: "npm test", Succeeded: false},
	})
	if status != "failed" || !strings.Contains(summary, "1 failed: npm test") {
		t.Fatalf("failed command summary = %q, %q", status, summary)
	}
}

func TestSummarizeSubAgentQuality(t *testing.T) {
	status, summary, count := summarizeSubAgentQuality("not_needed", "not_needed", false, nil, nil, nil)
	if status != "passed" || count != 0 || !strings.Contains(summary, "no file changes") {
		t.Fatalf("empty quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true}}, nil)
	if status != "passed" || count != 0 || !strings.Contains(summary, "passed") {
		t.Fatalf("passed quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("missing", "missing", false, []string{"main.go"}, []CodingSubAgentCommandResult{{Command: "npm test", Succeeded: false}}, nil)
	if status != "warning" || count != 4 || !strings.Contains(summary, "1 command(s) failed") || !strings.Contains(summary, "verification not run") {
		t.Fatalf("warning quality summary = %q, %q, %d", status, summary, count)
	}

	status, summary, count = summarizeSubAgentQuality("explored", "failed", true, []string{"main.go"}, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: false}}, []CodingSubAgentGuardrailViolation{{Tool: "bash"}})
	if status != "failed" || count != 2 || !strings.Contains(summary, "guardrail") || !strings.Contains(summary, "verification failed") {
		t.Fatalf("failed quality summary = %q, %q, %d", status, summary, count)
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

	searches := cb.getSearchesRun()
	if len(searches) != 2 {
		t.Fatalf("expected 2 search records, got %d", len(searches))
	}
	if searches[0].Tool != "Glob" || searches[0].Query != "**/*.go" || !searches[0].Succeeded {
		t.Fatalf("unexpected Glob record: %#v", searches[0])
	}
	if searches[1].Tool != "ripgrep" || searches[1].Query != "func main" || !searches[1].Succeeded {
		t.Fatalf("unexpected ripgrep record: %#v", searches[1])
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
}

func TestSummarizeSubAgentExploration(t *testing.T) {
	status, summary := summarizeSubAgentExploration(nil, nil, nil)
	if status != "not_needed" || !strings.Contains(summary, "跳过") {
		t.Fatalf("not_needed exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, nil, nil)
	if status != "missing" || !strings.Contains(summary, "没有记录") {
		t.Fatalf("missing exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, []string{"main.go"}, nil)
	if status != "read_only" || !strings.Contains(summary, "读取了 1 个文件") {
		t.Fatalf("read_only exploration = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentExploration([]string{"main.go"}, []string{"main.go"}, []CodingSubAgentSearchResult{
		{Tool: "ripgrep", Query: "func main", Succeeded: true},
		{Tool: "Glob", Query: "**/*.go", Succeeded: false},
	})
	if status != "explored" || !strings.Contains(summary, "1 次成功搜索") {
		t.Fatalf("explored summary = (%q, %q)", status, summary)
	}
}

func TestAppendSubAgentExplorationSummary(t *testing.T) {
	summary := appendSubAgentExplorationSummary("完成", "read_only", "读取了 1 个文件后修改。")
	if !strings.Contains(summary, "## 探索状态") || !strings.Contains(summary, "READ_ONLY") {
		t.Fatalf("unexpected exploration summary: %s", summary)
	}
}

func TestSummarizeSubAgentVerification(t *testing.T) {
	status, summary := summarizeSubAgentVerification(nil, nil)
	if status != "not_needed" || !strings.Contains(summary, "跳过") {
		t.Fatalf("not_needed summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, nil)
	if status != "missing" || !strings.Contains(summary, "没有运行") {
		t.Fatalf("missing summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./..." + strings.Repeat(" very-long-flag", 30), Succeeded: false},
	})
	if status != "failed" || !strings.Contains(summary, "go test ./...") || !strings.Contains(summary, "截断") {
		t.Fatalf("failed summary = (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "go test ./...", Succeeded: true},
		{Command: "go vet ./...", Succeeded: true},
	})
	if status != "passed" || !strings.Contains(summary, "2 条") {
		t.Fatalf("passed summary = (%q, %q)", status, summary)
	}
}

func TestSummarizeSubAgentVerificationIgnoresNonVerificationCommands(t *testing.T) {
	status, summary := summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: true},
		{Command: "pwd", Succeeded: true},
	})
	if status != "missing" || !strings.Contains(summary, "没有发现") {
		t.Fatalf("non-verification commands should not satisfy verification, got (%q, %q)", status, summary)
	}

	status, summary = summarizeSubAgentVerification([]string{"main.go"}, []CodingSubAgentCommandResult{
		{Command: "git status --short", Succeeded: false},
		{Command: "go test ./...", Succeeded: true},
	})
	if status != "passed" || !strings.Contains(summary, "1 条") {
		t.Fatalf("only verification commands should determine pass count, got (%q, %q)", status, summary)
	}
}

func TestIsSubAgentVerificationCommand(t *testing.T) {
	positive := []string{
		"go test ./...",
		"go.exe test ./...",
		"go test ./... 2>&1",
		"npm run build",
		"npm.cmd test",
		"npm run lint",
		"npm test",
		"npm run test -- --watch=false",
		"npm run test:unit",
		"npm run build:prod",
		"npm run type-check",
		"pnpm test",
		"pnpm lint",
		"pnpm run test:e2e",
		"yarn test",
		"yarn build",
		"yarn test:unit",
		"node --test",
		"bun test",
		"deno test",
		"npm run typecheck",
		"npm exec eslint .",
		"pnpm exec vitest run",
		"yarn dlx tsc --noEmit",
		"corepack pnpm test",
		"corepack yarn run lint",
		"corepack npx eslint .",
		"npx tsc --noEmit",
		"npx.cmd tsc --noEmit",
		"cargo clippy --all-targets",
		"go vet ./...",
		"go build ./...",
		"pytest tests",
		"pytest.exe tests",
		"python -m pytest tests",
		"uv run pytest tests",
		"poetry run pytest tests",
		"pipenv run pytest tests",
		"hatch run test",
		"pdm run pytest",
		"rye test",
		"tox -q",
		"nox -s tests",
		"make check",
		"make lint",
		"dotnet build",
		"bundle exec rspec",
		"bundle exec rubocop",
		"vendor/bin/phpunit",
		"./vendor/bin/phpunit",
		"./mvnw test",
		"mvnw verify",
		"gradlew.bat test",
		"./gradlew check",
		"go test ./... | Select-String FAIL",
		"CGO_ENABLED=0 go test ./...",
		"env CGO_ENABLED=0 go test ./...",
		"env -i CGO_ENABLED=0 go test ./...",
		"env -u GOPROXY go test ./...",
		"env --unset GOPROXY go test ./...",
		"cross-env CI=1 npm test",
		"cross-env-shell CI=1 npm run lint",
		"time go test ./...",
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
		"rg test .",
		"git log --oneline --grep test",
		"npm exec serve .",
		"corepack npm exec serve .",
		"poetry run serve",
		"pipenv run flask run",
		"hatch run serve",
		"pdm run serve",
		"bundle exec rails server",
		"./vendor/bin/php-cs-fixer fix",
		"./mvnw dependency:tree",
		"TEST_NAME=unit echo test",
		"env TEST_NAME=unit echo test",
		"time echo test",
		"rg \"TODO\" .",
		"git diff -- .",
	}
	for _, command := range negative {
		if isSubAgentVerificationCommand(command) {
			t.Fatalf("expected non-verification command: %q", command)
		}
	}
}

func TestSummarizeSubAgentVerificationCapsFailedCommands(t *testing.T) {
	var commands []CodingSubAgentCommandResult
	for i := 0; i < codingSubAgentFailedVerificationSummaryMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{
			Command:   fmt.Sprintf("go test ./pkg/%02d", i),
			Succeeded: false,
		})
	}

	status, summary := summarizeSubAgentVerification([]string{"main.go"}, commands)
	if status != "failed" {
		t.Fatalf("status = %q, want failed; summary=%q", status, summary)
	}
	if strings.Contains(summary, "go test ./pkg/06") {
		t.Fatalf("failed verification summary should be capped, got %q", summary)
	}
	if !strings.Contains(summary, "还有 2 条失败命令未展开") {
		t.Fatalf("expected remaining failed command count, got %q", summary)
	}
}

func TestAppendSubAgentVerificationSummary(t *testing.T) {
	summary := appendSubAgentVerificationSummary("完成", "missing", "没有运行 bash 验证命令。")
	if !strings.Contains(summary, "## 验证状态") || !strings.Contains(summary, "MISSING") {
		t.Fatalf("unexpected verification summary: %s", summary)
	}
}

func TestApplySubAgentVerificationOutcome(t *testing.T) {
	status, errMsg := applySubAgentVerificationOutcome(TaskExecPassed, "", "failed", "go test failed")
	if status != TaskExecFailed || errMsg != "go test failed" {
		t.Fatalf("expected failed verification to fail task, got status=%s err=%q", status, errMsg)
	}

	status, errMsg = applySubAgentVerificationOutcome(TaskExecPassed, "", "missing", "no commands")
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("missing verification should not fail task automatically, got status=%s err=%q", status, errMsg)
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
	for i := 0; i < codingSubAgentResultAuditMax+2; i++ {
		commands = append(commands, CodingSubAgentCommandResult{Command: fmt.Sprintf("cmd-%03d", i)})
		searches = append(searches, CodingSubAgentSearchResult{Query: fmt.Sprintf("query-%03d", i)})
		guardrails = append(guardrails, CodingSubAgentGuardrailViolation{Summary: fmt.Sprintf("guard-%03d", i)})
	}
	if got := len(limitSubAgentCommandResults(commands, codingSubAgentResultAuditMax)); got != codingSubAgentResultAuditMax {
		t.Fatalf("limited commands = %d, want %d", got, codingSubAgentResultAuditMax)
	}
	if got := len(limitSubAgentSearchResults(searches, codingSubAgentResultAuditMax)); got != codingSubAgentResultAuditMax {
		t.Fatalf("limited searches = %d, want %d", got, codingSubAgentResultAuditMax)
	}
	if got := len(limitSubAgentGuardrailViolations(guardrails, codingSubAgentResultAuditMax)); got != codingSubAgentResultAuditMax {
		t.Fatalf("limited guardrails = %d, want %d", got, codingSubAgentResultAuditMax)
	}
}
