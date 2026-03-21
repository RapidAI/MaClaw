package im

import (
	"fmt"
	"strings"
)

// BuildHelpMessage generates a context-aware help message based on the
// user's current state (device count, selection, LLM availability).
func BuildHelpMessage(machineCount int, selectedMachine string, llmEnabled bool) string {
	var b strings.Builder

	if llmEnabled && machineCount > 1 {
		b.WriteString("🤖 当前处于无感智能模式 — 直接发消息即可，系统自动判断路由。\n\n")
		b.WriteString("以下命令可手动覆盖智能路由：\n\n")
	} else {
		b.WriteString("📋 可用命令：\n\n")
	}

	b.WriteString("/call <昵称>  — 切换到指定设备\n")
	b.WriteString("  例: /call MacBook-Pro\n")

	if machineCount > 1 {
		b.WriteString("\n/call all  — 进入群聊模式（所有设备同时回复）\n")
		b.WriteString("\n/discuss <话题>  — 发起多设备 AI 讨论\n")
		b.WriteString("  例: /discuss 如何优化性能\n")
	}

	b.WriteString("\n/stop  — 停止当前讨论\n")
	b.WriteString("/help  — 显示此帮助\n")

	// Context-specific hints.
	if selectedMachine == broadcastMachineID {
		b.WriteString("\n💡 当前处于群聊模式，使用 /call <昵称> 可切回单聊。\n")
	}

	if machineCount == 1 {
		b.WriteString("\n💡 当前只有一台设备在线，消息会自动发送到该设备。\n")
	}

	if machineCount > 1 && !llmEnabled {
		b.WriteString(fmt.Sprintf("\n💡 您有 %d 台设备在线，使用 /call <昵称> 选择目标设备。\n", machineCount))
	}

	return b.String()
}
