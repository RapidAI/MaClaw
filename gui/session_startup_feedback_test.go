package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"path/filepath"
	"testing"
)

func TestSessionStartupFeedbackShouldInjectResumePrompt(t *testing.T) {
	feedback := &SessionStartupFeedback{}
	session := &RemoteSession{ProjectPath: filepath.Clean("D:/work/project"), InjectResumePrompt: true}
	if !feedback.shouldInjectResumePrompt(session) {
		t.Fatal("expected explicit resume session to allow resume prompt injection")
	}

	session.InjectResumePrompt = false
	if feedback.shouldInjectResumePrompt(session) {
		t.Fatal("expected fresh session to skip resume prompt injection")
	}

	session.InjectResumePrompt = true
	session.ProjectPath = ""
	if feedback.shouldInjectResumePrompt(session) {
		t.Fatal("expected empty project path to skip resume prompt injection")
	}
}

func TestSessionStartupFeedbackWatchStartupInjectsOnlyForExplicitResume(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	handle := newFakeExecutionHandle(301)
	session := &RemoteSession{
		ID:                 "sess-startup",
		Tool:               "claude",
		ProjectPath:        filepath.Clean("D:/work/project"),
		Exec:               handle,
		Status:             SessionRunning,
		InjectResumePrompt: true,
		Summary:            SessionSummary{SessionID: "sess-startup", Status: string(SessionRunning)},
	}
	manager.sessions[session.ID] = session

	feedback := NewSessionStartupFeedback(manager)
	feedback.SetCheckpointer(&SessionCheckpointer{})
	feedback.SetUnfinishedSlotResolver(func(projectPath string) *agent.UnfinishedTaskSlot {
		return &agent.UnfinishedTaskSlot{ProjectPath: projectPath, ResumePrompt: "resume previous task"}
	})

	var messages []string
	feedback.watchLoop(session.ID, func(msg string) {
		messages = append(messages, msg)
	})
	if len(handle.writes) == 0 {
		t.Fatal("expected startup feedback to inject resume prompt for explicit resume session")
	}
	if got := string(handle.writes[0]); got != "resume previous task" {
		t.Fatalf("injected prompt = %q, want %q", got, "resume previous task")
	}
	if len(messages) < 2 || messages[1] != "已加载显式选择的未完成任务进度，已自动注入上下文" {
		t.Fatalf("messages = %#v, want startup injection feedback", messages)
	}
}

func TestSessionStartupFeedbackWatchStartupSkipsFreshSessionInjection(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	handle := newFakeExecutionHandle(302)
	session := &RemoteSession{
		ID:                 "sess-fresh",
		Tool:               "claude",
		ProjectPath:        filepath.Clean("D:/work/project"),
		Exec:               handle,
		Status:             SessionRunning,
		InjectResumePrompt: false,
		Summary:            SessionSummary{SessionID: "sess-fresh", Status: string(SessionRunning)},
	}
	manager.sessions[session.ID] = session

	feedback := NewSessionStartupFeedback(manager)
	feedback.SetCheckpointer(&SessionCheckpointer{})
	feedback.SetUnfinishedSlotResolver(func(projectPath string) *agent.UnfinishedTaskSlot {
		return &agent.UnfinishedTaskSlot{ProjectPath: projectPath, ResumePrompt: "resume previous task"}
	})

	var messages []string
	feedback.watchLoop(session.ID, func(msg string) {
		messages = append(messages, msg)
	})
	if len(handle.writes) != 0 {
		t.Fatal("expected no injected prompt for fresh session")
	}
	if len(messages) != 1 || messages[0] != "会话已就绪 (ID: sess-fresh, 工具: claude)" {
		t.Fatalf("messages = %#v, want only ready message", messages)
	}
}
