package main

import (
	"fmt"
	"strings"
)

func renderCreateSessionLaunchBanner(toolName, provider, projectPath string) string {
	return fmt.Sprintf("🚀 即将启动编程会话：\n   🔧 编程工具: %s\n   📦 服务商: %s\n   📁 工作目录: %s", toolName, provider, projectPath)
}

func renderCreateSessionStartedMessage(hints []string, sessionID string) string {
	var b strings.Builder
	for _, hint := range hints {
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("✅ 会话已创建 [%s]\n", sessionID))
	b.WriteString("\n📋 下一步操作：")
	b.WriteString(fmt.Sprintf("\n1. 调用 get_session_output(session_id=%q) 确认会话已启动（状态为 %s）", sessionID, SessionRunning))
	b.WriteString(fmt.Sprintf("\n2. 外部会话 %q 为旧兼容路径；agent 新编程任务请走内部 CodingSubAgent。", sessionID))
	b.WriteString(fmt.Sprintf("\n⚠️ 不再向外部会话发送新编程指令。可检查 get_session_output 确认旧会话状态（%s）。", SessionRunning))
	b.WriteString(fmt.Sprintf("\n🛑 如果会话已退出（%s）且退出码非 0，不要重试，直接告知用户错误信息。", SessionExited))
	return b.String()
}
