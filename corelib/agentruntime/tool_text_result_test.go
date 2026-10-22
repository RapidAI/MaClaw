package agentruntime

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestToolTextResultClassifiesSharedMarkers(t *testing.T) {
	cases := []struct {
		text    string
		outcome agent.ToolExecutionOutcome
	}{
		{text: "normal output", outcome: agent.ToolExecutionOutcomeOK},
		{text: "命令超时（30 秒）", outcome: agent.ToolExecutionOutcomeTimeout},
		{text: "[错误] 工具执行失败", outcome: agent.ToolExecutionOutcomeError},
		{text: "unknown memory action", outcome: agent.ToolExecutionOutcomeError},
		{text: "output\nError: printed by command", outcome: agent.ToolExecutionOutcomeOK},
		{text: "[system rejected] Direct authenticated browser-side HTTP side effects through bash are disabled.", outcome: agent.ToolExecutionOutcomeError},
		{text: "page text\n[system rejected] quoted from a log", outcome: agent.ToolExecutionOutcomeOK},
		{text: "tool execution interrupted: context canceled", outcome: agent.ToolExecutionOutcomeError},
		{text: "计划已保存\n理解: 目标本身没有失败，上一轮只是被复核。", outcome: agent.ToolExecutionOutcomeOK},
		{text: "步骤 1 已完成: 结果已保存，旧方案失败已排除", outcome: agent.ToolExecutionOutcomeOK},
		{text: "保存配置失败: disk full", outcome: agent.ToolExecutionOutcomeError},
		{text: "配置保存失败，磁盘满", outcome: agent.ToolExecutionOutcomeError},
		{text: "登录成功但保存 token 失败，拒绝写入", outcome: agent.ToolExecutionOutcomeError},
		{text: "保存 bad.pdf 失败：空", outcome: agent.ToolExecutionOutcomeError},
		{text: "请先保存。若失败再重试", outcome: agent.ToolExecutionOutcomeOK},
		{text: "请先保存，若失败再重试", outcome: agent.ToolExecutionOutcomeOK},
		{text: "保存点已写下\n任务失败", outcome: agent.ToolExecutionOutcomeOK},
	}
	for _, tc := range cases {
		got := ToolTextResult(tc.text)
		if got.Result != tc.text || got.Outcome != tc.outcome {
			t.Errorf("ToolTextResult(%q)=%#v, want outcome=%q", tc.text, got, tc.outcome)
		}
	}
}
