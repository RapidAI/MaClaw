package agentruntime

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// goalToolFailurePrefixes are the first-line contracts emitted by the goal
// handler when the call did not land. Success text (计划已保存, 步骤已完成,
// 目标已完成, 目标失败) is absent on purpose: those bodies quote 失败.
var goalToolFailurePrefixes = []string{
	"错误:",
	"错误：",
	"未知 goal action",
	"创建目标失败",
	"已有活跃目标",
	"目标已处于终态",
	"当前没有活跃目标",
	"目标不在活跃状态",
	"目标不在暂停状态",
}

// goalHandlerSuccessPrefixes are first lines the goal handler emits after a
// landed call. "当前没有活跃目标" is a failure and is matched earlier.
var goalHandlerSuccessPrefixes = []string{
	"目标已创建",
	"计划已保存",
	"步骤 ",
	"目标已完成",
	"目标失败",
	"目标已暂停",
	"目标已恢复",
	"目标已清除",
	"当前没有目标",
	"目标 [",
	"[>>] 当前目标",
	"[||] 当前目标",
	"[!!] 当前目标",
	"[OK] 当前目标",
	"[ERR] 当前目标",
	"[?] 当前目标",
}

// GoalToolReportedFailure is the goal handler's own refusal. A saved plan, a
// recorded step, and a recorded complete are success even when the body
// quotes 失败, 执行失败, or 已保存. Re-parsing that prose with ToolTextFailure
// told the model the write failed, so it never called complete and the row
// stayed active.
func GoalToolReportedFailure(text string) bool {
	first := goalOutcomeFirstLine(text)
	if first == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(first), "goal unavailable") {
		return true
	}
	for _, prefix := range goalToolFailurePrefixes {
		if strings.HasPrefix(first, prefix) {
			return true
		}
	}
	return false
}

// GoalToolExecutionResult classifies one goal tool result. Handler success
// lines stay successful without a body scan. Handler refusals stay failures.
// Any other text (a system rejection, an unknown tool, a panic) keeps the
// generic classifier.
func GoalToolExecutionResult(text string) agent.ToolExecutionResult {
	if GoalToolReportedFailure(text) {
		return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeError}
	}
	if goalHandlerSuccessLine(text) {
		return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
	}
	return ToolTextResult(text)
}

func goalHandlerSuccessLine(text string) bool {
	first := goalOutcomeFirstLine(text)
	if first == "" {
		return false
	}
	for _, prefix := range goalHandlerSuccessPrefixes {
		if strings.HasPrefix(first, prefix) {
			return true
		}
	}
	return false
}

func goalOutcomeFirstLine(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(first)
}
