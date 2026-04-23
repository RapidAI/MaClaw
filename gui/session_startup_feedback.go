package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"fmt"
	"strings"
	"time"
)

// SessionStartupFeedback monitors session startup progress and pushes
// status updates to the caller via a ProgressCallback.
type SessionStartupFeedback struct {
	manager           *RemoteSessionManager
	checkpointer      *SessionCheckpointer
	unfinishedSlotFor func(projectPath string) *agent.UnfinishedTaskSlot
}

// NewSessionStartupFeedback creates a new SessionStartupFeedback instance.
func NewSessionStartupFeedback(manager *RemoteSessionManager) *SessionStartupFeedback {
	return &SessionStartupFeedback{manager: manager}
}

// SetCheckpointer attaches a SessionCheckpointer for resume context injection.
func (f *SessionStartupFeedback) SetCheckpointer(cp *SessionCheckpointer) {
	f.checkpointer = cp
}

// SetUnfinishedSlotResolver resolves the explicitly selected unfinished slot
// for a running session's project.
func (f *SessionStartupFeedback) SetUnfinishedSlotResolver(fn func(projectPath string) *agent.UnfinishedTaskSlot) {
	f.unfinishedSlotFor = fn
}

// WatchStartup monitors the startup of a session in a background goroutine.
// Every 3 seconds it checks the session status and pushes a progress message.
// When the session reaches "running" status, a success notification is sent
// and any prior session checkpoint is injected as resume context.
// After 60 seconds without reaching "running", a timeout warning is sent.
func (f *SessionStartupFeedback) WatchStartup(sessionID string, callback tool.ProgressCallback) {
	go f.watchLoop(sessionID, callback)
}

func (f *SessionStartupFeedback) shouldInjectResumePrompt(session *RemoteSession) bool {
	if f == nil || session == nil {
		return false
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if !session.InjectResumePrompt {
		return false
	}
	if strings.TrimSpace(session.ProjectPath) == "" {
		return false
	}
	return true
}

func (f *SessionStartupFeedback) watchLoop(sessionID string, callback tool.ProgressCallback) {
	messages := []string{
		"正在初始化工具",
		"正在加载项目",
		"等待工具就绪",
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	msgIdx := 0

	for {
		select {
		case <-ticker.C:
			session, ok := f.manager.Get(sessionID)
			if !ok {
				callback("⚠️ 会话未找到: " + sessionID)
				return
			}

			session.mu.RLock()
			status := session.Status
			session.mu.RUnlock()

			if status == SessionRunning {
				callback(fmt.Sprintf("✅ 会话已就绪 (ID: %s, 工具: %s)", session.ID, session.Tool))

				// Inject resume context only for sessions explicitly launched to continue
				// an unfinished slot. Plain same-project fresh launches must not inherit it.
				if f.checkpointer != nil && f.manager != nil && f.unfinishedSlotFor != nil && f.shouldInjectResumePrompt(session) {
					session.mu.RLock()
					projectPath := session.ProjectPath
					session.mu.RUnlock()

					if slot := f.unfinishedSlotFor(projectPath); slot != nil {
						if resumePrompt := f.checkpointer.BuildResumePromptForSlot(slot); resumePrompt != "" {
							if err := f.manager.WriteInput(sessionID, resumePrompt); err == nil {
								callback("📋 已加载显式选择的未完成任务进度，已自动注入上下文")
							}
						}
					}
				}
				return
			}

			callback(messages[msgIdx%len(messages)])
			msgIdx++

		case <-timer.C:
			callback("⚠️ 会话启动超时（已等待 60 秒），请检查日志或重试")
			return
		}
	}
}
