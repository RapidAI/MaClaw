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
	b.WriteString(fmt.Sprintf("\n1. 调用 get_session_output(session_id=%q) 确认会话已启动（状态为 running）", sessionID))
	b.WriteString(fmt.Sprintf("\n2. 立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 将需求发送给编程工具", sessionID))
	b.WriteString("\n⚠️ 编程工具启动后等待输入，不发送指令不会开始工作。最多检查 2 次 get_session_output，确认 running 后立即发送。")
	b.WriteString("\n🛑 如果会话已退出（exited）且退出码非 0，不要重试，直接告知用户错误信息。")
	return b.String()
}
