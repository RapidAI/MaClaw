package agentruntime

import "testing"

func TestRegisteredToolTextOutcome(t *testing.T) {
	cases := []struct {
		name string
		text string
		want RegisteredToolOutcome
	}{
		{"empty is uncertain", "", RegisteredToolOutcomeUncertain},
		{"blank is uncertain", "  \n ", RegisteredToolOutcomeUncertain},
		{"plain success", "文件已保存到 output/report.md", RegisteredToolOutcomeSucceeded},
		{"error prefix", "Error: connection refused", RegisteredToolOutcomeFailed},
		{"mcp error bracket", "[MCP Error] tool crashed", RegisteredToolOutcomeFailed},
		{"mcp call failed", "mcp call failed: timeout", RegisteredToolOutcomeFailed},
		{"mcp rejected", "mcp 调用被拒绝：权限不足", RegisteredToolOutcomeFailed},
		{"missing server id", "缺少 server_id 参数", RegisteredToolOutcomeFailed},
		{"unknown tool", "unknown tool: foo", RegisteredToolOutcomeFailed},
		{"panic marker", "tool execution panicked: runtime error", RegisteredToolOutcomeFailed},
		{"chinese error prefix", "错误:无法读取文件", RegisteredToolOutcomeFailed},
		{"exec failed contains", "步骤 2 执行失败，已回滚", RegisteredToolOutcomeFailed},
		{"tool exception contains", "工具执行异常：nil pointer", RegisteredToolOutcomeFailed},
		{"arg parse contains", "参数解析失败：unexpected token", RegisteredToolOutcomeFailed},
		{"pdf font missing", "未找到可用的中文字体，无法渲染", RegisteredToolOutcomeFailed},
		{"pdf cannot generate", "无法生成 PDF：内容为空", RegisteredToolOutcomeFailed},
		{"pdf failure prefix", "PDF 生成失败: 磁盘已满", RegisteredToolOutcomeFailed},
		{"pdf missing content", "缺少 content 参数", RegisteredToolOutcomeFailed},
		{"success mentioning pdf", "PDF 报告已生成：report.pdf", RegisteredToolOutcomeSucceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RegisteredToolTextOutcome(tc.text); got != tc.want {
				t.Fatalf("RegisteredToolTextOutcome(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
