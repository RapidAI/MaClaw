package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// BR-20261120-BD8D23C968F7 — 任务继续时失败.
//
// A coding task keeps its transcript in the coding-agent resume record, not in
// the chat memory and not in the tab snapshot: the workbench runs the turns and
// the tab only renders them. The continuation guard only looked at the chat
// memory and the tab snapshot, so pressing 继续 on a coding task that had 41
// recorded turns was refused with "没有找到这个任务之前的对话". The guard is right
// to fail closed on a task with no history at all; it must not fail closed on a
// task whose history lives in the coding transcript.
func TestContinuationUsesCodingResumeTranscript(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "coding-task")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(projectPath)
	req := AIAssistantSendRequest{RequestID: "req-continue", Text: "继续", Lang: "zh-Hans"}

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}

	// Nothing recorded anywhere: the guard still fails closed.
	app.restoreProjectAgentSession(handler, owner, projectPath)
	if !projectContinuationHistoryMissing(req, handler, owner, projectPath) {
		t.Fatal("continuation with no history anywhere must still be refused")
	}

	// The workbench records the task transcript the way a real coding turn does.
	recordCodingAgentResume(owner, projectPath, []agent.ConversationEntry{
		{Role: "user", Content: "修掉表格公式被竖线切断的问题"},
		{Role: "assistant", Content: "根因是单元格切分没保护公式里的竖线"},
	}, nil)
	if got := loadCodingAgentResume(owner, projectPath); len(got.History) == 0 {
		t.Fatal("coding resume transcript was not recorded")
	}

	// 繼續 now works: the guard sees the coding transcript through the restore.
	app.restoreProjectAgentSession(handler, owner, projectPath)
	if projectContinuationHistoryMissing(req, handler, owner, projectPath) {
		t.Fatal("continuation was blocked although the coding task has a transcript")
	}
	if entries := memory.Load(owner); len(entries) == 0 {
		t.Fatalf("coding transcript was not restored into the session: %#v", entries)
	}
}

// A coding transcript belonging to another project must not satisfy the guard:
// the fallback is scoped to the task it was recorded for.
func TestCodingResumeTranscriptDoesNotLeakAcrossProjects(t *testing.T) {
	app := newProjectSearchTestApp(t)
	root := t.TempDir()
	recorded := filepath.Join(root, "recorded-task")
	other := filepath.Join(root, "other-task")
	for _, dir := range []string{recorded, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	owner := projectSessionOwnerID(recorded)
	recordCodingAgentResume(owner, recorded, []agent.ConversationEntry{
		{Role: "user", Content: "另一件事"},
		{Role: "assistant", Content: "好的"},
	}, nil)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}
	app.restoreProjectAgentSession(handler, projectSessionOwnerID(other), other)
	if got := memory.Load(projectSessionOwnerID(other)); len(got) != 0 {
		t.Fatalf("another task's coding transcript leaked in: %#v", got)
	}
}

// An ACP continue is a different session in the same workspace. Its session
// id does not name the project's coding file. Loading by that id reads a
// record that can belong to another conversation in the same tree, and that
// record must not become this task's history.
func TestACPContinuationDoesNotAdoptForeignCodingResume(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "shared-workspace")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	acpUser := acpAssistantSessionOwnerID("editor-session")
	recordCodingAgentResume(acpUser, projectPath, []agent.ConversationEntry{
		{Role: "user", Content: "另一段编辑器对话"},
		{Role: "assistant", Content: "已改完"},
	}, nil)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}
	req := AIAssistantSendRequest{Text: "继续", Lang: "zh-Hans"}
	app.restoreProjectAgentSession(handler, acpUser, projectPath)
	if got := memory.Load(acpUser); len(got) != 0 {
		t.Fatalf("ACP continue adopted a session-scoped coding record: %#v", got)
	}
	if !projectContinuationHistoryMissing(req, handler, acpUser, projectPath) {
		t.Fatal("ACP continue with no project transcript was not refused")
	}
}

// The coding record is the executor loop. Its reasoning trace is not a chat
// turn: the chat loop sends reasoning_content on every assistant row. The
// visible user and assistant turns are what a continue needs.
func TestCodingResumeRestoreDropsReasoningTrace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "coding-task")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(projectPath)
	recordCodingAgentResume(owner, projectPath, []agent.ConversationEntry{
		{Role: "user", Content: "修掉表格公式被竖线切断的问题"},
		{Role: "assistant", Content: "", ReasoningContent: "先看切分函数", ToolCalls: []interface{}{map[string]interface{}{"id": "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at table.go"},
		{Role: "assistant", Content: "根因是单元格切分没保护公式里的竖线", ReasoningContent: "先看切分函数，再核对公式里的竖线"},
	}, nil)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}
	app.restoreProjectAgentSession(handler, owner, projectPath)
	got := memory.Load(owner)
	if len(got) != 2 || got[0].Content != "修掉表格公式被竖线切断的问题" {
		t.Fatalf("visible turns were not restored: %#v", got)
	}
	if got[1].ReasoningContent != "" || got[1].ToolCalls != nil || got[1].Content != "根因是单元格切分没保护公式里的竖线" {
		t.Fatalf("coding loop row entered the chat session: %#v", got[1])
	}
	for _, entry := range got {
		if entry.Role == "tool" || entry.ToolCalls != nil {
			t.Fatalf("coding tool row entered the chat session: %#v", got)
		}
	}
}

func TestCodingResumeRestoreKeepsToolOnlyAssistantTurn(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "tool-only")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(projectPath)
	recordCodingAgentResume(owner, projectPath, []agent.ConversationEntry{
		{Role: "user", Content: "看一下切分函数"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "code_navigation"}}}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at table.go"},
	}, nil)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}
	req := AIAssistantSendRequest{Text: "继续", Lang: "zh-Hans"}
	app.restoreProjectAgentSession(handler, owner, projectPath)
	got := memory.Load(owner)
	if len(got) != 2 || got[1].Role != "assistant" || got[1].ToolCalls != nil {
		t.Fatalf("tool-only assistant turn was not kept as chat text: %#v", got)
	}
	if !strings.Contains(fmt.Sprint(got[1].Content), "code_navigation") {
		t.Fatalf("closed tool name was dropped: %#v", got[1])
	}
	if projectContinuationHistoryMissing(req, handler, owner, projectPath) {
		t.Fatal("tool-only coding transcript was refused as missing history")
	}
}

