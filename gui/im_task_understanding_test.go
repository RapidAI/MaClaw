package main

import (
	"strings"
	"testing"
)

func TestParseTaskUnderstandingResponse_ValidJSON(t *testing.T) {
	raw := `{
		"task_type": "信息搜集",
		"summary": "搜集美发师 vibehair 的详细个人资料",
		"goals": ["查找从业经历", "确认工作单位"],
		"constraints": ["信息尽可能详细"],
		"execution_plan": ["搜索相关网页", "提取个人信息", "整理成报告"],
		"enhanced_instruction": "在互联网上搜集美发师 vibehair 的详细资料，包括从业经历、工作单位、技术水平、所在门店名称和地址。整理成结构化报告。"
	}`
	result, err := parseTaskUnderstandingResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "信息搜集" {
		t.Errorf("TaskType = %q, want %q", result.TaskType, "信息搜集")
	}
	if result.Summary != "搜集美发师 vibehair 的详细个人资料" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(result.Goals) != 2 {
		t.Errorf("Goals len = %d, want 2", len(result.Goals))
	}
	if len(result.ExecutionPlan) != 3 {
		t.Errorf("ExecutionPlan len = %d, want 3", len(result.ExecutionPlan))
	}
	if result.EnhancedInstruction == "" {
		t.Error("EnhancedInstruction should not be empty")
	}
}

func TestParseTaskUnderstandingResponse_MarkdownFenced(t *testing.T) {
	raw := "```json\n{\"task_type\": \"代码开发\", \"summary\": \"开发贪吃蛇游戏\", \"enhanced_instruction\": \"使用 HTML5 Canvas 开发贪吃蛇游戏\"}\n```"
	result, err := parseTaskUnderstandingResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "代码开发" {
		t.Errorf("TaskType = %q, want %q", result.TaskType, "代码开发")
	}
}

func TestParseTaskUnderstandingResponse_EmptyResponse(t *testing.T) {
	_, err := parseTaskUnderstandingResponse("")
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestParseTaskUnderstandingResponse_NoJSON(t *testing.T) {
	_, err := parseTaskUnderstandingResponse("I don't understand the request")
	if err == nil {
		t.Error("expected error for non-JSON response")
	}
}

func TestParseTaskUnderstandingResponse_EmptySummaryAndInstruction(t *testing.T) {
	raw := `{"task_type": "test", "summary": "", "enhanced_instruction": ""}`
	_, err := parseTaskUnderstandingResponse(raw)
	if err == nil {
		t.Error("expected error when both summary and enhanced_instruction are empty")
	}
}

func TestParseTaskUnderstandingResponse_PlanOnly(t *testing.T) {
	raw := `{"task_type":"","summary":"","execution_plan":["Inspect the affected login flow","Implement and verify the correction"],"enhanced_instruction":""}`
	result, err := parseTaskUnderstandingResponse(raw)
	if err != nil {
		t.Fatalf("plan-only structured understanding should be accepted: %v", err)
	}
	if len(result.ExecutionPlan) != 2 {
		t.Fatalf("execution plan = %#v, want two steps", result.ExecutionPlan)
	}
}

func TestFormatTaskUnderstandingSummary(t *testing.T) {
	r := &taskUnderstandingResult{
		TaskType:      "信息搜集",
		Summary:       "搜集美发师 vibehair 的详细个人资料",
		Goals:         []string{"查找从业经历", "确认工作单位"},
		Constraints:   []string{"信息尽可能详细"},
		ExecutionPlan: []string{"搜索相关网页", "提取个人信息", "整理成报告"},
	}
	summary := formatTaskUnderstandingSummary(r, "D:\\workprj\\aicoder")
	if !strings.Contains(summary, "任务类型：信息搜集") {
		t.Errorf("missing task type in summary: %s", summary)
	}
	if !strings.Contains(summary, "任务理解：搜集美发师") {
		t.Errorf("missing summary line: %s", summary)
	}
	if !strings.Contains(summary, "- 查找从业经历") {
		t.Errorf("missing goal: %s", summary)
	}
	if !strings.Contains(summary, "1. 搜索相关网页") {
		t.Errorf("missing execution plan step: %s", summary)
	}
	if !strings.Contains(summary, "默认工作目录：D:\\workprj\\aicoder") {
		t.Errorf("missing project path: %s", summary)
	}
}

func TestFormatTaskUnderstandingSummary_Nil(t *testing.T) {
	if got := formatTaskUnderstandingSummary(nil, ""); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
}

func TestFormatEnhancedInstruction(t *testing.T) {
	r := &taskUnderstandingResult{
		EnhancedInstruction: "搜集美发师 vibehair 的详细资料并整理成报告",
	}
	got := formatEnhancedInstruction(r)
	if got != "搜集美发师 vibehair 的详细资料并整理成报告" {
		t.Errorf("got %q", got)
	}
}

func TestFormatEnhancedInstruction_Empty(t *testing.T) {
	r := &taskUnderstandingResult{EnhancedInstruction: ""}
	if got := formatEnhancedInstruction(r); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestFormatEnhancedInstruction_Nil(t *testing.T) {
	if got := formatEnhancedInstruction(nil); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildPendingConfirmation_WithUnderstanding(t *testing.T) {
	understanding := &taskUnderstandingResult{
		TaskType:            "信息搜集",
		Summary:             "搜集美发师资料",
		Goals:               []string{"查找经历"},
		ExecutionPlan:       []string{"搜索网页", "提取信息"},
		EnhancedInstruction: "搜集美发师 vibehair 的详细资料",
	}
	intent := taskIntentResult{Intent: intentAmbiguous}
	item := buildPendingConfirmation(nil, "u1", "搜索网上美发师vibehair的资料", intent, understanding)

	// Summary should use LLM understanding, not raw text echo.
	if strings.Contains(item.Summary, "我理解你想让我处理这项任务") {
		t.Errorf("Summary should not contain raw-text echo when understanding is available: %s", item.Summary)
	}
	if !strings.Contains(item.Summary, "任务类型：信息搜集") {
		t.Errorf("Summary should contain structured task type: %s", item.Summary)
	}
	if !strings.Contains(item.Summary, "搜集美发师资料") {
		t.Errorf("Summary should contain LLM summary: %s", item.Summary)
	}

	// PlannedActions should use LLM execution plan.
	if len(item.PlannedActions) != 2 || item.PlannedActions[0] != "搜索网页" {
		t.Errorf("PlannedActions should use LLM plan: %v", item.PlannedActions)
	}

	// Enhanced fields should be set.
	if item.EnhancedSummary == "" {
		t.Error("EnhancedSummary should be set")
	}
	if item.EnhancedInstruction != "搜集美发师 vibehair 的详细资料" {
		t.Errorf("EnhancedInstruction = %q", item.EnhancedInstruction)
	}
}

func TestBuildPendingConfirmation_WithoutUnderstandingIsOnlyForNonIMCallers(t *testing.T) {
	intent := taskIntentResult{Intent: intentCoding, Matched: "修改代码"}
	item := buildPendingConfirmation(nil, "u1", "帮我修改代码", intent, nil)

	// Should fall back to raw-text echo.
	if !strings.Contains(item.Summary, "我理解你想让我处理这项任务：帮我修改代码") {
		t.Errorf("Summary should contain raw-text echo when no understanding: %s", item.Summary)
	}
	if item.EnhancedSummary != "" {
		t.Errorf("EnhancedSummary should be empty: %q", item.EnhancedSummary)
	}
	if item.EnhancedInstruction != "" {
		t.Errorf("EnhancedInstruction should be empty: %q", item.EnhancedInstruction)
	}
}

func TestHasMeaningfulTaskUnderstanding(t *testing.T) {
	original := "fix the login bug"
	for _, tc := range []struct {
		name  string
		value *taskUnderstandingResult
		want  bool
	}{
		{name: "missing", value: nil, want: false},
		{name: "echo only", value: &taskUnderstandingResult{Summary: original, EnhancedInstruction: original}, want: false},
		{name: "echo only with punctuation", value: &taskUnderstandingResult{Summary: "Fix the login bug."}, want: false},
		{name: "echo with label", value: &taskUnderstandingResult{Summary: "Task: fix the login bug"}, want: false},
		{name: "echo with polite wrapper", value: &taskUnderstandingResult{Summary: "Please fix the login bug"}, want: false},
		{name: "echo only plan", value: &taskUnderstandingResult{ExecutionPlan: []string{"Fix the login bug"}}, want: false},
		{name: "shortened extract", value: &taskUnderstandingResult{Summary: "Fix the login"}, want: false},
		{name: "echo plus expanded steps", value: &taskUnderstandingResult{Summary: "Fix the login bug, then verify the authentication flow."}, want: true},
		{name: "rewritten summary", value: &taskUnderstandingResult{Summary: "Diagnose and repair the login flow."}, want: true},
		{name: "execution plan", value: &taskUnderstandingResult{ExecutionPlan: []string{"Inspect logs", "Apply fix"}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMeaningfulTaskUnderstanding(tc.value, original); got != tc.want {
				t.Fatalf("hasMeaningfulTaskUnderstanding() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildPendingConfirmation_UsesEnhancedInstructionWithoutSummary(t *testing.T) {
	item := buildPendingConfirmation(nil, "u1", "fix the login bug", taskIntentResult{Intent: intentCoding}, &taskUnderstandingResult{
		TaskType:            "code repair",
		EnhancedInstruction: "Diagnose the login flow, correct the defect, and verify the affected path.",
	})
	if strings.Contains(item.Summary, "I understand you want me to handle this task") {
		t.Fatalf("confirmation must not fall back to raw-text echo: %q", item.Summary)
	}
	if !strings.Contains(item.Summary, "Diagnose the login flow") {
		t.Fatalf("confirmation should show enhanced instruction: %q", item.Summary)
	}
}

func TestBuildPendingConfirmation_DropsEchoOnlyPlanItems(t *testing.T) {
	item := buildPendingConfirmation(nil, "u1", "fix the login bug", taskIntentResult{Intent: intentCoding}, &taskUnderstandingResult{
		Summary:       "Diagnose the login flow and verify the correction.",
		ExecutionPlan: []string{"Fix the login bug", "Inspect the authentication flow", "Inspect the authentication flow"},
	})
	if len(item.PlannedActions) != 1 || item.PlannedActions[0] != "Inspect the authentication flow" {
		t.Fatalf("planned actions should retain only unique non-echo details, got %#v", item.PlannedActions)
	}
	if strings.Contains(item.Summary, "任务理解：fix the login bug") {
		t.Fatalf("confirmation must not display the echoed request: %q", item.Summary)
	}
}

func TestConfirmationApprovedText_WithEnhancedInstruction(t *testing.T) {
	item := &pendingConfirmation{
		OriginalText:        "搜索网上美发师vibehair的资料",
		ResumeText:          "搜索网上美发师vibehair的资料",
		EnhancedInstruction: "搜集美发师 vibehair 的详细资料，包括从业经历、工作单位、技术水平、所在门店和地址",
	}
	got := confirmationApprovedText(item)
	// Should lead with enhanced instruction.
	if !strings.Contains(got, "搜集美发师 vibehair 的详细资料") {
		t.Errorf("should use enhanced instruction: %s", got)
	}
	if strings.HasPrefix(got, "搜索网上美发师vibehair的资料") {
		t.Errorf("should NOT start with raw original text: %s", got)
	}
	// Should include original text as reference.
	if !strings.Contains(got, "[用户原始请求]") {
		t.Errorf("should include original text as reference: %s", got)
	}
	if !strings.Contains(got, "搜索网上美发师vibehair的资料") {
		t.Errorf("should contain original text: %s", got)
	}
	if !strings.Contains(got, "[执行上下文]") {
		t.Errorf("should contain execution context: %s", got)
	}
}

func TestConfirmationApprovedText_FallbackToResumeText(t *testing.T) {
	item := &pendingConfirmation{
		OriginalText:        "帮我修复 bug",
		ResumeText:          "帮我修复 bug\n\n用户补充/修正：目录改成 D:/fixed",
		EnhancedInstruction: "", // empty — no LLM understanding
	}
	got := confirmationApprovedText(item)
	if !strings.Contains(got, "帮我修复 bug") {
		t.Errorf("should fall back to ResumeText: %s", got)
	}
	if !strings.Contains(got, "用户补充/修正") {
		t.Errorf("should include revision: %s", got)
	}
}

func TestApplyConfirmationRevision_ClearsEnhancedFields(t *testing.T) {
	item := &pendingConfirmation{
		OriginalText:        "搜索美发师资料",
		ResumeText:          "搜索美发师资料",
		Summary:             "任务类型：信息搜集",
		EnhancedSummary:     "structured summary",
		EnhancedInstruction: "enhanced instruction",
	}
	revised := applyConfirmationRevision(item, "改成搜索北京地区的")
	if revised.EnhancedSummary != "" {
		t.Errorf("EnhancedSummary should be cleared after revision: %q", revised.EnhancedSummary)
	}
	if revised.EnhancedInstruction != "" {
		t.Errorf("EnhancedInstruction should be cleared after revision: %q", revised.EnhancedInstruction)
	}
	if !strings.Contains(revised.ResumeText, "改成搜索北京地区的") {
		t.Errorf("ResumeText should include revision: %s", revised.ResumeText)
	}
}

func TestParseTaskUnderstandingResponse_SimplifiedFormat(t *testing.T) {
	// Simplified prompt produces minimal JSON without goals/constraints.
	raw := `{"task_type":"代码优化","summary":"对项目进行全面优化修复","execution_plan":["分析现有代码","识别问题","逐一修复"],"enhanced_instruction":"对当前项目进行全面的代码优化和 bug 修复"}`
	result, err := parseTaskUnderstandingResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "代码优化" {
		t.Errorf("TaskType = %q, want %q", result.TaskType, "代码优化")
	}
	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
	if len(result.ExecutionPlan) != 3 {
		t.Errorf("ExecutionPlan len = %d, want 3", len(result.ExecutionPlan))
	}
	// Goals and Constraints are optional in simplified format.
	if len(result.Goals) != 0 {
		t.Errorf("Goals should be empty in simplified format: %v", result.Goals)
	}
}

func TestParseTaskUnderstandingResponse_JSONWithPrefixText(t *testing.T) {
	// Some models prepend explanation text before the JSON.
	raw := `以下是我的分析结果：
{"task_type":"文件处理","summary":"处理桌面文件","execution_plan":["读取文件","处理内容"],"enhanced_instruction":"处理桌面上的文件"}`
	result, err := parseTaskUnderstandingResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "文件处理" {
		t.Errorf("TaskType = %q, want %q", result.TaskType, "文件处理")
	}
}

func TestTaskUnderstandingSimplifiedPrompt_IsShort(t *testing.T) {
	// Keep the one-shot presentation classifier below 200 chars.
	if len(taskUnderstandingSimplifiedPrompt) > 200 {
		t.Errorf("simplified prompt too long: %d chars (want ≤200)", len(taskUnderstandingSimplifiedPrompt))
	}
}
