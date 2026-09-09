package agentruntime

import "testing"

// TestToolOutcomeVocabularyUnified pins the merged boundary: the three-state
// registered-tool classifier must agree with the shared ToolTextFailure
// vocabulary on every non-empty text, so GUI and headless hosts cannot drift.
func TestToolOutcomeVocabularyUnified(t *testing.T) {
	texts := []string{
		"[MCP Error] connection lost",
		"mcp 调用失败：超时",
		"mcp 调用被拒绝",
		"tool execution panicked: nil map",
		"缺少 server_id",
		"未找到可用的中文字体",
		"无法生成 PDF：内容为空",
		"PDF 生成失败: 磁盘已满",
		"步骤 3 执行失败",
		"参数解析失败: bad json",
		"error: boom",
		"读取失败: no such file",
		"unknown tool: foo",
		"文件已保存到 report.pdf",
		"PDF 报告已生成：report.pdf",
		"命令执行完成",
	}
	for _, text := range texts {
		failed := ToolTextFailure(text)
		outcome := RegisteredToolTextOutcome(text)
		if failed && outcome != RegisteredToolOutcomeFailed {
			t.Fatalf("ToolTextFailure(%q)=true but outcome=%v", text, outcome)
		}
		if !failed && outcome != RegisteredToolOutcomeSucceeded {
			t.Fatalf("ToolTextFailure(%q)=false but outcome=%v", text, outcome)
		}
	}
	if got := RegisteredToolTextOutcome(" \n "); got != RegisteredToolOutcomeUncertain {
		t.Fatalf("blank text outcome=%v", got)
	}
}
