package agentruntime

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestGoalToolReportedFailureIgnoresPlanProse(t *testing.T) {
	plan := "计划已保存\n理解: 上一轮执行失败，日志里有 error: unavailable，只是被复核。\n"
	if !ToolTextFailure(plan) {
		t.Fatal("generic classifier should still flag 执行失败 in the plan body")
	}
	if GoalToolReportedFailure(plan) {
		t.Fatalf("plan prose classified as failure: %q", plan)
	}
	for _, success := range []string{
		"目标已完成\n总结: 性能提升20%",
		"目标失败\n原因: 无法收尾",
		"步骤 1 已完成: 结果已保存，旧方案失败已排除",
		"步骤 1 已完成: 失败: 旧路径已排除",
	} {
		if GoalToolReportedFailure(success) {
			t.Fatalf("success classified as failure: %q", success)
		}
		if got := GoalToolExecutionResult(success); got.Outcome != agent.ToolExecutionOutcomeOK {
			t.Fatalf("handler success = %s for %q", got.Outcome, success)
		}
	}
	if got := GoalToolExecutionResult(plan); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("plan prose = %s", got.Outcome)
	}
	status := "[ERR] 当前目标 [failed]\n理解: 上一轮执行失败"
	if got := GoalToolExecutionResult(status); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("status read = %s", got.Outcome)
	}
	bare := "[ERR] 工具执行失败"
	if got := GoalToolExecutionResult(bare); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("bare status mark = %s", got.Outcome)
	}
	for _, failure := range []string{
		"错误: 不能完成目标\n总结未覆盖验收标准：性能提升20%",
		"已有活跃目标（active）：提升20%",
		"目标不在活跃状态（当前: paused），无法暂停。",
		"目标不在暂停状态（当前: active），无法恢复。",
		"goal unavailable: coding session owner is missing",
		"未知 goal action: pause（支持: create/plan/step/complete/fail/get）",
	} {
		if !GoalToolReportedFailure(failure) {
			t.Fatalf("handler refusal classified as success: %q", failure)
		}
	}
	for _, boundary := range []string{
		"[system rejected] tool not permitted",
		"Unknown tool: goal",
		"Tool execution panicked: boom",
		"tool execution interrupted: context canceled",
	} {
		if GoalToolReportedFailure(boundary) {
			t.Fatalf("transport text matched the handler contract: %q", boundary)
		}
		if got := GoalToolExecutionResult(boundary); got.Outcome != agent.ToolExecutionOutcomeError {
			t.Fatalf("transport text = %s for %q", got.Outcome, boundary)
		}
	}
}
