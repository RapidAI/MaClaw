package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCodexWriteAskUserQuestionAnswerRequiresResumeSession(t *testing.T) {
	h := &CodexSDKExecutionHandle{threadID: "thread_999"}
	err := h.WriteAskUserQuestionAnswer(&PendingToolUse{ToolUseID: "toolu_1"}, "OAuth")
	if err == nil {
		t.Fatal("expected resume-session error")
	}
	if !strings.Contains(err.Error(), "resume_session_id=thread_999") {
		t.Fatalf("error = %q, want resume session hint", err)
	}
}

func TestCodexWriteAskUserQuestionAnswerRequiresResumeSessionWithoutThreadID(t *testing.T) {
	h := &CodexSDKExecutionHandle{}
	err := h.WriteAskUserQuestionAnswer(&PendingToolUse{ToolUseID: "toolu_1"}, "OAuth")
	if err == nil {
		t.Fatal("expected resume-session error")
	}
	if !strings.Contains(err.Error(), "starting a new resumed session") {
		t.Fatalf("error = %q, want resumed-session guidance", err)
	}
}

func TestCodexEventUnmarshalThreadStarted(t *testing.T) {
	raw := `{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Type != "thread.started" {
		t.Fatalf("Type = %q, want %q", event.Type, "thread.started")
	}
	if event.ThreadID != "0199a213-81c0-7800-8aa1-bbab2a035a53" {
		t.Fatalf("ThreadID = %q", event.ThreadID)
	}
}

func TestCodexEventUnmarshalItemCompleted(t *testing.T) {
	raw := `{"type":"item.completed","item":{"id":"item_1","item_type":"command_execution","command":"bash -lc ls","aggregated_output":"README.md\nsrc\n","exit_code":0,"status":"completed"}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Type != "item.completed" {
		t.Fatalf("Type = %q, want %q", event.Type, "item.completed")
	}
	if event.Item == nil {
		t.Fatal("Item is nil")
	}
	if event.Item.ItemType != "command_execution" {
		t.Fatalf("ItemType = %q, want %q", event.Item.ItemType, "command_execution")
	}
	if event.Item.Command != "bash -lc ls" {
		t.Fatalf("Command = %q", event.Item.Command)
	}
	if event.Item.ExitCode == nil || *event.Item.ExitCode != 0 {
		t.Fatalf("ExitCode = %v", event.Item.ExitCode)
	}
}

func TestCodexEventUnmarshalAssistantMessage(t *testing.T) {
	raw := `{"type":"item.completed","item":{"id":"item_3","item_type":"assistant_message","text":"There is a README.md in the root."}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Item.ItemType != "assistant_message" {
		t.Fatalf("ItemType = %q", event.Item.ItemType)
	}
	if event.Item.Text != "There is a README.md in the root." {
		t.Fatalf("Text = %q", event.Item.Text)
	}
}

func TestCodexEventUnmarshalTurnCompleted(t *testing.T) {
	raw := `{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Type != "turn.completed" {
		t.Fatalf("Type = %q", event.Type)
	}
	if event.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if event.Usage.InputTokens != 24763 {
		t.Fatalf("InputTokens = %d", event.Usage.InputTokens)
	}
	if event.Usage.OutputTokens != 122 {
		t.Fatalf("OutputTokens = %d", event.Usage.OutputTokens)
	}
	if event.Usage.CachedInputTokens != 24448 {
		t.Fatalf("CachedInputTokens = %d", event.Usage.CachedInputTokens)
	}
}

func TestCodexEventUnmarshalTurnCompletedWithNestedCacheUsage(t *testing.T) {
	raw := `{"type":"turn.completed","usage":{"prompt_tokens":1200,"completion_tokens":80,"prompt_tokens_details":{"cached_tokens":768,"cache_creation_input_tokens":128}}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if event.Usage.InputTokens != 1200 || event.Usage.OutputTokens != 80 {
		t.Fatalf("tokens = %d in, %d out", event.Usage.InputTokens, event.Usage.OutputTokens)
	}
	if event.Usage.CachedInputTokens != 768 || event.Usage.CacheWriteTokens != 128 {
		t.Fatalf("cache tokens = read:%d write:%d", event.Usage.CachedInputTokens, event.Usage.CacheWriteTokens)
	}
}

func TestCodexEventToTextAssistantMessage(t *testing.T) {
	event := CodexEvent{
		Type: "item.completed",
		Item: &CodexItem{
			ItemType: "assistant_message",
			Text:     "Hello world",
		},
	}
	text := codexEventToText(event)
	if text != "Hello world" {
		t.Fatalf("text = %q, want %q", text, "Hello world")
	}
}

func TestCodexEventToTextCommandStarted(t *testing.T) {
	event := CodexEvent{
		Type: "item.started",
		Item: &CodexItem{
			ItemType: "command_execution",
			Command:  "go test ./...",
		},
	}
	text := codexEventToText(event)
	if text != "go test ./..." {
		t.Fatalf("text = %q", text)
	}
}

func TestCodexEventToTextFileChange(t *testing.T) {
	event := CodexEvent{
		Type: "item.completed",
		Item: &CodexItem{
			ItemType: "file_change",
			FilePath: "main.go",
		},
	}
	text := codexEventToText(event)
	if text != "Modified main.go" {
		t.Fatalf("text = %q", text)
	}
}

func TestCodexEventToTextReasoning(t *testing.T) {
	event := CodexEvent{
		Type: "item.completed",
		Item: &CodexItem{
			ItemType: "reasoning",
			Text:     "Searching for README files",
		},
	}
	text := codexEventToText(event)
	if text != "Searching for README files" {
		t.Fatalf("text = %q", text)
	}
}

func TestCodexEventToTextTurnCompleted(t *testing.T) {
	event := CodexEvent{
		Type: "turn.completed",
		Usage: &CodexUsage{
			InputTokens:       1000,
			CachedInputTokens: 900,
			CacheWriteTokens:  64,
			OutputTokens:      200,
		},
	}
	text := codexEventToText(event)
	if text != "Turn completed (tokens: 1000 in, 200 out, 900 cache read, 64 cache write)" {
		t.Fatalf("text = %q", text)
	}
}

func TestCodexSDKExecutionHandleAccumulatesUsage(t *testing.T) {
	h := &CodexSDKExecutionHandle{}
	h.recordUsage(CodexUsage{InputTokens: 100, OutputTokens: 10, CachedInputTokens: 80, CacheWriteTokens: 4})
	h.recordUsage(CodexUsage{InputTokens: 50, OutputTokens: 5, CachedInputTokens: 40, CacheWriteTokens: 2})
	usage := h.TotalUsage()
	if usage.InputTokens != 150 || usage.OutputTokens != 15 || usage.CachedInputTokens != 120 || usage.CacheWriteTokens != 6 {
		t.Fatalf("total usage = %+v", usage)
	}
}

func TestRemoteSessionTokenUsageFromCodex(t *testing.T) {
	usage := remoteSessionTokenUsageFromCodex(CodexUsage{InputTokens: 100, OutputTokens: 10, CachedInputTokens: 80, CacheWriteTokens: 4})
	if usage.InputTokens != 100 || usage.OutputTokens != 10 || usage.CachedInputTokens != 80 || usage.CacheWriteTokens != 4 {
		t.Fatalf("session usage = %+v", usage)
	}
}

func TestCodexRemoteSessionUsageDoesNotAccumulateMaclawLLMUsage(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	h := &CodexSDKExecutionHandle{outputCh: make(chan []byte, 1)}
	h.recordUsage(CodexUsage{InputTokens: 1200, OutputTokens: 80, CachedInputTokens: 768, CacheWriteTokens: 128})
	now := time.Now()
	session := &RemoteSession{
		ID:        "codex-usage-1",
		Tool:      "codex",
		Status:    SessionRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Summary: SessionSummary{
			SessionID: "codex-usage-1",
			Tool:      "codex",
			Status:    SessionRunning.String(),
			UpdatedAt: now.Unix(),
		},
		Exec: h,
	}
	manager := NewRemoteSessionManager(app)

	h.outputCh <- []byte(`{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":768,"output_tokens":80,"cache_write_tokens":128}}` + "\n")
	close(h.outputCh)
	manager.runCodexSDKOutputLoop(session)

	if session.TokenUsage.InputTokens != 1200 || session.TokenUsage.OutputTokens != 80 || session.TokenUsage.CachedInputTokens != 768 || session.TokenUsage.CacheWriteTokens != 128 {
		t.Fatalf("session TokenUsage = %+v", session.TokenUsage)
	}
	if usage := app.GetAllLLMTokenUsage(); len(usage) != 0 {
		t.Fatalf("remote Codex session usage leaked into Maclaw LLM usage: %+v", usage)
	}
}

func TestBuildCodexToolUseEventCommand(t *testing.T) {
	session := &RemoteSession{ID: "sess-1"}
	event := CodexEvent{
		Type: "item.started",
		Item: &CodexItem{
			ID:       "item_1",
			ItemType: "command_execution",
			Command:  "go test ./...",
		},
	}
	evt := buildCodexToolUseEvent(session, event)
	if evt.Type != "command.started" {
		t.Fatalf("Type = %q, want %q", evt.Type, "command.started")
	}
	if evt.Command != "go test ./..." {
		t.Fatalf("Command = %q", evt.Command)
	}
	if evt.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q", evt.SessionID)
	}
}

func TestBuildCodexToolUseEventFileChange(t *testing.T) {
	session := &RemoteSession{ID: "sess-2"}
	event := CodexEvent{
		Type: "item.completed",
		Item: &CodexItem{
			ID:       "item_2",
			ItemType: "file_change",
			FilePath: "main.go",
		},
	}
	evt := buildCodexToolUseEvent(session, event)
	if evt.Type != "file.change" {
		t.Fatalf("Type = %q, want %q", evt.Type, "file.change")
	}
	if evt.RelatedFile != "main.go" {
		t.Fatalf("RelatedFile = %q", evt.RelatedFile)
	}
}
