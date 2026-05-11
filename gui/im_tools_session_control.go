package main

import "fmt"

func runSessionControlAction(manager *RemoteSessionManager, sessionID string, action sessionControlAction) string {
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if manager == nil {
		return "会话管理器未初始化"
	}
	switch action {
	case sessionControlActionInterrupt:
		if err := manager.Interrupt(sessionID); err != nil {
			return fmt.Sprintf("中断失败: %s", err.Error())
		}
		return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
	case sessionControlActionKill:
		if err := manager.Kill(sessionID); err != nil {
			return fmt.Sprintf("终止失败: %s", err.Error())
		}
		return fmt.Sprintf("已终止会话 %s", sessionID)
	default:
		return "action 参数无效，可选值: interrupt, kill"
	}
}
