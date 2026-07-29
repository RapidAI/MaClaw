package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for confirmationApprovedText — pending item extraction
// ---------------------------------------------------------------------------

func TestConfirmationApprovedText_NoPendingItems_DirectExecution(t *testing.T) {
	item := &pendingConfirmation{
		OriginalText:        "帮我搜索 hugging face daily papers",
		ResumeText:          "帮我搜索 hugging face daily papers",
		EnhancedInstruction: "搜索 Hugging Face 每日论文并整理结果",
		EnhancedSummary:     "任务类型：信息搜集\n任务理解：搜索论文",
	}
	result := confirmationApprovedText(item)
	if !strings.Contains(result, "直接开始执行") {
		t.Errorf("expected '直接开始执行' for task with no pending items, got: %s", result)
	}
	if strings.Contains(result, "尚未提供") {
		t.Errorf("should NOT contain pending item guidance when no items are pending, got: %s", result)
	}
}

func TestConfirmationApprovedText_WithPendingItems_AsksForInfo(t *testing.T) {
	item := &pendingConfirmation{
		OriginalText:        "帮我上去部署一下maclaw的hub端",
		ResumeText:          "帮我上去部署一下maclaw的hub端",
		EnhancedInstruction: "通过SSH连接至远程服务器，拉取GitHub仓库并完成Hub端部署",
		EnhancedSummary: "任务类型：远程操作\n" +
			"任务理解：通过SSH连接至远程服务器\n" +
			"约束/要求：\n" +
			"  • 待确认：远程服务器的 SSH 连接凭证（IP、端口、用户名、密码/密钥）\n" +
			"  • 待确认：目标部署路径",
	}
	result := confirmationApprovedText(item)
	if strings.Contains(result, "直接开始执行") {
		t.Errorf("should NOT say '直接开始执行' when there are pending items, got: %s", result)
	}
	if !strings.Contains(result, "尚未提供") {
		t.Errorf("expected '尚未提供' guidance for pending items, got: %s", result)
	}
	if !strings.Contains(result, "SSH 连接凭证") {
		t.Errorf("expected SSH credential item to be extracted, got: %s", result)
	}
	if !strings.Contains(result, "目标部署路径") {
		t.Errorf("expected deployment path item to be extracted, got: %s", result)
	}
}

func TestConfirmationApprovedText_PendingItemsInConstraints(t *testing.T) {
	// Constraints field is part of the LLM understanding, shown in Summary.
	item := &pendingConfirmation{
		OriginalText: "连接服务器查看GPU",
		ResumeText:   "连接服务器查看GPU",
		EnhancedSummary: "约束/要求：\n" +
			"  • 待确认：服务器IP地址\n" +
			"  • 待确认：SSH用户名和密码",
	}
	result := confirmationApprovedText(item)
	if strings.Contains(result, "直接开始执行") {
		t.Errorf("should NOT say '直接开始执行' when there are pending items")
	}
	if !strings.Contains(result, "服务器IP地址") {
		t.Errorf("expected server IP item to be extracted, got: %s", result)
	}
	if !strings.Contains(result, "SSH用户名和密码") {
		t.Errorf("expected SSH credentials item to be extracted, got: %s", result)
	}
}

func TestConfirmationApprovedText_NilItem(t *testing.T) {
	result := confirmationApprovedText(nil)
	if result != "" {
		t.Errorf("expected empty string for nil item, got: %s", result)
	}
}

func TestConfirmationApprovedText_FallbackToResumeText_NoPending(t *testing.T) {
	// No EnhancedInstruction — should use ResumeText.
	item := &pendingConfirmation{
		OriginalText: "部署服务",
		ResumeText:   "部署服务到远程服务器",
	}
	result := confirmationApprovedText(item)
	if !strings.Contains(result, "部署服务到远程服务器") {
		t.Errorf("expected ResumeText in output, got: %s", result)
	}
	if !strings.Contains(result, "直接开始执行") {
		t.Errorf("expected '直接开始执行' when no pending items, got: %s", result)
	}
}

func TestConfirmationApprovedText_DeduplicatesPendingItems(t *testing.T) {
	// Same pending item appears in both Summary and EnhancedSummary.
	item := &pendingConfirmation{
		OriginalText: "连接服务器",
		ResumeText:   "连接服务器",
		Summary:      "待确认：SSH密码",
		EnhancedSummary: "约束/要求：\n" +
			"  • 待确认：SSH密码",
	}
	result := confirmationApprovedText(item)
	// The pending items section should only list "SSH密码" once.
	pendingSection := ""
	if idx := strings.Index(result, "尚未提供"); idx >= 0 {
		pendingSection = result[idx:]
	}
	if strings.Count(pendingSection, "SSH密码") > 1 {
		t.Errorf("pending items should be deduplicated, got section: %s", pendingSection)
	}
}

// ---------------------------------------------------------------------------
// Tests for extractPendingConfirmItems
// ---------------------------------------------------------------------------

func TestExtractPendingConfirmItems_EmptyItem(t *testing.T) {
	items := extractPendingConfirmItems(&pendingConfirmation{})
	if len(items) != 0 {
		t.Errorf("expected 0 items for empty confirmation, got %d", len(items))
	}
}

func TestExtractPendingConfirmItems_NilItem(t *testing.T) {
	items := extractPendingConfirmItems(nil)
	if items != nil {
		t.Errorf("expected nil for nil confirmation, got %v", items)
	}
}

func TestExtractPendingConfirmItems_MultipleSourcesDedup(t *testing.T) {
	item := &pendingConfirmation{
		Summary:         "待确认：服务器地址",
		EnhancedSummary: "待确认：服务器地址\n待确认：用户名",
	}
	items := extractPendingConfirmItems(item)
	if len(items) != 2 {
		t.Errorf("expected 2 unique items, got %d: %v", len(items), items)
	}
}

func TestExtractPendingConfirmItems_VariousFormats(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		count int
	}{
		{"emoji prefix", "待确认：SSH密码", 1},
		{"no emoji", "待确认：部署路径", 1},
		{"colon variant", "待确认:端口号", 1},
		{"bullet prefix", "  • 待确认：凭证信息", 1},
		{"no pending", "一切就绪，可以开始", 0},
		{"multiple lines", "待确认：A\n待确认：B\n正常内容", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &pendingConfirmation{Summary: tc.text}
			items := extractPendingConfirmItems(item)
			if len(items) != tc.count {
				t.Errorf("expected %d items, got %d: %v", tc.count, len(items), items)
			}
		})
	}
}

func TestExtractPendingConfirmItems_IgnoresEnhancedInstruction(t *testing.T) {
	// EnhancedInstruction is the execution directive, not the summary.
	// "待确认" in the instruction should NOT be extracted as a pending item.
	item := &pendingConfirmation{
		EnhancedInstruction: "确认待确认项后，通过SSH连接至远程服务器执行部署",
	}
	items := extractPendingConfirmItems(item)
	if len(items) != 0 {
		t.Errorf("should not extract from EnhancedInstruction, got %d: %v", len(items), items)
	}
}

// ---------------------------------------------------------------------------
// Tests for DriftDetector ResultHint — mechanism-level fix
// ---------------------------------------------------------------------------

func TestDriftDetector_RecoverPromptIncludesToolResult(t *testing.T) {
	// When drift is detected, the recover prompt should include the last
	// tool result so the LLM has actionable context to change strategy.
	d := NewDriftDetector(8, 0.8)
	hint := "SSH 连接失败: unable to authenticate\n\n认证失败且未提供密码。请使用 password 参数重试"
	for i := 0; i < 3; i++ {
		d.Record(ToolCallRecord{
			ToolName:   "ssh",
			ArgsHash:   "abc123",
			ResultHint: hint,
		})
	}
	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("expected drift to be detected")
	}
	if !strings.Contains(result.ReplanPrompt, "认证失败且未提供密码") {
		t.Errorf("recover prompt should include the tool result hint, got: %s", result.ReplanPrompt)
	}
	if !strings.Contains(result.ReplanPrompt, "根据以上工具反馈调整策略") {
		t.Errorf("recover prompt should instruct LLM to adjust strategy based on tool feedback, got: %s", result.ReplanPrompt)
	}
}

func TestDriftDetector_RecoverPromptWithoutHint(t *testing.T) {
	// When no result hint is provided, the recover prompt should still work
	// (backward compatibility with callers that don't set ResultHint).
	d := NewDriftDetector(8, 0.8)
	for i := 0; i < 3; i++ {
		d.Record(ToolCallRecord{
			ToolName: "web_search",
			ArgsHash: "def456",
		})
	}
	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("expected drift to be detected")
	}
	if strings.Contains(result.ReplanPrompt, "最后一次工具返回") {
		t.Errorf("should NOT include tool result section when hint is empty, got: %s", result.ReplanPrompt)
	}
}

func TestDriftDetector_SecondDriftIncludesHintInSeverePrompt(t *testing.T) {
	// On second drift (NeedHumanHelp=true), the severe prompt should also
	// include the tool result hint.
	d := NewDriftDetector(8, 0.8)
	// First drift
	for i := 0; i < 3; i++ {
		d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "aaa"})
	}
	d.DetectDrift()
	d.ResetWindow()
	// Second drift with hint
	hint := "dial tcp: connection refused"
	for i := 0; i < 3; i++ {
		d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "bbb", ResultHint: hint})
	}
	result := d.DetectDrift()
	if !result.NeedHumanHelp {
		t.Fatal("expected NeedHumanHelp on second drift")
	}
	if !strings.Contains(result.ReplanPrompt, "connection refused") {
		t.Errorf("severe prompt should include tool result hint, got: %s", result.ReplanPrompt)
	}
}

func TestTruncateRunesForDrift(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		max      int
		contains string
		maxLen   int
	}{
		{"short string", "hello", 200, "hello", 5},
		{"exact limit", strings.Repeat("a", 200), 200, strings.Repeat("a", 200), 200},
		{"over limit", strings.Repeat("a", 300), 200, "", 202}, // 200 + "…"
		{"chinese chars", "这是一个很长的中文字符串需要被截断", 5, "这是一个很", 0},
		{"newline cut", "line1\nline2\nline3\nline4", 15, "line1\nline2", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateRunesForDrift(tc.input, tc.max)
			if tc.contains != "" && !strings.Contains(result, tc.contains) {
				t.Errorf("expected result to contain %q, got %q", tc.contains, result)
			}
			if tc.maxLen > 0 && len([]rune(result)) > tc.maxLen {
				t.Errorf("result too long: %d runes (max %d)", len([]rune(result)), tc.maxLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: confirmation with pending items + SSH scenario
// ---------------------------------------------------------------------------

func TestConfirmationFlow_SSHTaskWithMissingCredentials(t *testing.T) {
	// Simulates the exact scenario from the bug report:
	// User asks to deploy via SSH, confirmation panel shows "待确认: SSH凭证",
	// user clicks confirm. The approved text should tell the LLM to ask for
	// credentials, not "直接执行不要确认".
	item := &pendingConfirmation{
		ID:                  "confirm-test",
		UserID:              "user1",
		OriginalText:        "帮我上去部署一下maclaw的hub 端并运行它。maclaw仓库地址：https://github.com/RapidAI/MaCLaw",
		ResumeText:          "帮我上去部署一下maclaw的hub 端并运行它。maclaw仓库地址：https://github.com/RapidAI/MaCLaw",
		EnhancedInstruction: "通过SSH连接至远程服务器，拉取指定GitHub仓库并完成Hub端项目的部署与启动运行",
		EnhancedSummary: "任务类型：远程操作\n" +
			"任务理解：通过SSH连接至远程服务器，拉取指定GitHub仓库并完成Hub端项目的部署与启动运行。\n" +
			"目标：\n" +
			"  • 成功克隆 MaCLaw 仓库代码至远程服务器\n" +
			"  • 完成 Hub 端所需运行环境的配置与依赖安装\n" +
			"  • 成功启动并运行 MaCLaw Hub 端服务\n" +
			"约束/要求：\n" +
			"  • 目标仓库地址：https://github.com/RapidAI/MaCLaw\n" +
			"  • 待确认：远程服务器的 SSH 连接凭证（IP、端口、用户名、密码/密钥）\n" +
			"  • 待确认：目标部署路径（建议在服务器上新建专门目录，避免与本地工作目录混淆）\n" +
			"  • 待确认：服务运行环境要求（如 Python 版本、Node.js 版本等，需参考仓库 README）",
		TaskType:  "ssh",
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	result := confirmationApprovedText(item)

	// Must NOT tell LLM to execute directly.
	if strings.Contains(result, "直接开始执行") {
		t.Fatalf("CRITICAL: approved text tells LLM to execute directly despite missing SSH credentials!\nResult: %s", result)
	}

	// Must tell LLM to ask for the missing info.
	if !strings.Contains(result, "尚未提供") {
		t.Fatalf("approved text should instruct LLM to ask for missing info\nResult: %s", result)
	}

	// Must mention the specific missing items.
	mustContain := []string{
		"SSH 连接凭证",
		"目标部署路径",
		"服务运行环境要求",
	}
	for _, mc := range mustContain {
		if !strings.Contains(result, mc) {
			t.Errorf("approved text should mention missing item %q\nResult: %s", mc, result)
		}
	}

	// Must still contain the enhanced instruction (the task itself).
	if !strings.Contains(result, "通过SSH连接至远程服务器") {
		t.Errorf("approved text should contain the enhanced instruction\nResult: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for DriftDetector result-change mechanism — the core distinction
// between dead loops (same input + same output) and legitimate polling
// (same input + changing output).
// ---------------------------------------------------------------------------

func TestDriftDetector_PollingWithChangingResults_NoDrift(t *testing.T) {
	// Simulates the exact scenario from the bug: LLM polls a background task
	// with check_task. Args are identical (same task_id), but results change
	// as the task progresses: running 18s → running 23s → completed.
	// This is NOT drift — external state is progressing.
	d := NewDriftDetector(8, 0.8)
	hash := "same_args_hash"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "状态: running\n已运行: 18s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "状态: running\n已运行: 23s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "状态: completed\nEXIT: 0"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected when results are changing (polling)")
	}
}

func TestDriftDetector_SameResultsTriggersNormalDrift(t *testing.T) {
	// SSH exec same failing command 3 times with identical results.
	// This IS drift — dead loop, nothing is changing.
	d := NewDriftDetector(8, 0.8)
	hash := "same_args_hash"
	sameResult := "connection refused"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameResult})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameResult})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameResult})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("drift SHOULD be detected when results are identical (dead loop)")
	}
}

func TestDriftDetector_EmptyResultsConservativelyTriggersDrift(t *testing.T) {
	// When all ResultHints are empty, we have no data to compare.
	// Conservative: treat as potential drift (don't suppress detection).
	d := NewDriftDetector(8, 0.8)
	hash := "same_args_hash"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: ""})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: ""})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: ""})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("drift SHOULD be detected when all results are empty (conservative)")
	}
}

func TestDriftDetector_SessionOutputPolling_NoDrift(t *testing.T) {
	// get_session_output polling a coding session. Same args (session_id),
	// but output changes as the session produces new content.
	d := NewDriftDetector(8, 0.8)
	hash := "session_123_hash"
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: "(无新输出)"})
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: "compiling main.go..."})
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: "tests passed"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected when session output is changing")
	}
}

func TestDriftDetector_SessionOutputStuck_Drifts(t *testing.T) {
	// get_session_output polling but session is stuck — same output every time.
	// This IS drift.
	d := NewDriftDetector(8, 0.8)
	hash := "session_123_hash"
	stuck := "(无新输出)"
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: stuck})
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: stuck})
	d.Record(ToolCallRecord{ToolName: "get_session_output", ArgsHash: hash, ResultHint: stuck})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("drift SHOULD be detected when session output is stuck")
	}
}

func TestDriftDetector_AnyToolPollingWithChangingResults_NoDrift(t *testing.T) {
	// Generic: any tool called with same args but different results.
	// Future-proof — works for docker, MCP tools, etc.
	d := NewDriftDetector(8, 0.8)
	hash := "same_hash"
	d.Record(ToolCallRecord{ToolName: "some_mcp_tool", ArgsHash: hash, ResultHint: "status: pending"})
	d.Record(ToolCallRecord{ToolName: "some_mcp_tool", ArgsHash: hash, ResultHint: "status: processing"})
	d.Record(ToolCallRecord{ToolName: "some_mcp_tool", ArgsHash: hash, ResultHint: "status: done"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected for any tool when results are changing")
	}
}

func TestDriftDetector_PreviewDrift_RespectsResultChanges(t *testing.T) {
	// PreviewDrift should also respect the result-change mechanism.
	d := NewDriftDetector(8, 0.8)
	hash := "same_hash"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "running 10s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "running 20s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "completed"})

	preview := d.PreviewDrift()
	if preview.Drifted {
		t.Fatal("PreviewDrift should NOT report drift when results are changing")
	}
}

func TestDriftDetector_MixedResults_LastTwoSame_NoDrift(t *testing.T) {
	// Results: A → B → B. There was a change (A→B), so not a dead loop.
	// The task might have reached a stable state (e.g. "completed" twice).
	d := NewDriftDetector(8, 0.8)
	hash := "same_hash"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "running"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "completed"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "completed"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected when there was at least one result change")
	}
}

func TestDriftDetector_TruncatedHintSame_ButFullResultDiffers_NoDrift(t *testing.T) {
	// Edge case: ResultHint is identical (the changing part falls beyond the
	// 200-rune truncation boundary), but ResultHash differs because the full
	// tool result is different. This happens when a long command echo fills
	// the first 200 runes and the status/timestamp is beyond that.
	//
	// Without ResultHash, this would be a false positive (drift detected
	// despite results actually changing).
	d := NewDriftDetector(8, 0.8)
	hash := "same_args_hash"
	sameHint := "任务 bg_123\n命令: go install golang.org/dl/go1.24.2@latest 2>&1 || echo trying direct download && wget -q https://go.dev/dl/go1.24.2.linux-amd64.tar.gz -O /tmp/go1.24.2.tar.gz…"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameHint, ResultHash: "hash_running_18s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameHint, ResultHash: "hash_running_23s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: sameHint, ResultHash: "hash_completed"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected when ResultHash differs (full results are changing)")
	}
}

func TestDriftDetector_ResultHashFallsBackToHint(t *testing.T) {
	// When ResultHash is not set (backward compat), falls back to ResultHint.
	d := NewDriftDetector(8, 0.8)
	hash := "same_hash"
	// Only set ResultHint, not ResultHash — should still detect changes.
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "running"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "completed"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: hash, ResultHint: "completed"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("drift should NOT be detected — ResultHint fallback should detect the change")
	}
}
