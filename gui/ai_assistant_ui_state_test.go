package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIAssistantUIStatePersistsUnderDataDir(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}

	state := AIAssistantUIState{
		Messages: []map[string]interface{}{{
			"id":      "m1",
			"role":    "user",
			"content": "hello",
		}},
		Prompts:                  []string{"hello"},
		ContextBoundaryMessageID: " boundary ",
	}

	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}

	wantPath := filepath.Join(home, ".maclaw", "data", "ai_assistant_ui_state.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("state file not written at %s: %v", wantPath, err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if loaded.StoragePath != wantPath {
		t.Fatalf("StoragePath = %q, want %q", loaded.StoragePath, wantPath)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0]["content"] != "hello" {
		t.Fatalf("Messages = %#v", loaded.Messages)
	}
	if len(loaded.Prompts) != 1 || loaded.Prompts[0] != "hello" {
		t.Fatalf("Prompts = %#v", loaded.Prompts)
	}
	if loaded.ContextBoundaryMessageID != "boundary" {
		t.Fatalf("ContextBoundaryMessageID = %q", loaded.ContextBoundaryMessageID)
	}

	if err := app.ClearAIAssistantUIState(); err != nil {
		t.Fatalf("ClearAIAssistantUIState() error = %v", err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("state file still exists after clear, stat err = %v", err)
	}
}

func TestAIAssistantUIStateNormalizesAndBoundsPayload(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	messages := make([]map[string]interface{}, 0, 205)
	for i := 0; i < 205; i++ {
		messages = append(messages, map[string]interface{}{
			"id":      "m",
			"role":    "user",
			"content": i,
		})
	}
	messages = append(messages,
		map[string]interface{}{"id": "progress", "role": "progress", "content": "skip"},
		map[string]interface{}{"id": "system", "role": "system", "content": "skip"},
		map[string]interface{}{"role": "user", "content": "skip"},
	)
	prompts := make([]string, 0, 104)
	for i := 0; i < 104; i++ {
		prompts = append(prompts, " prompt ")
	}
	prompts = append(prompts, " ")

	if err := app.SaveAIAssistantUIState(AIAssistantUIState{
		Messages:                 messages,
		Prompts:                  prompts,
		ContextBoundaryMessageID: " boundary ",
	}); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Messages) != maxAIAssistantUIStateMessages {
		t.Fatalf("messages len = %d, want %d", len(loaded.Messages), maxAIAssistantUIStateMessages)
	}
	if loaded.Messages[0]["content"] != float64(5) {
		t.Fatalf("oldest retained message content = %#v, want 5", loaded.Messages[0]["content"])
	}
	if len(loaded.Prompts) != maxAIAssistantUIStatePrompts {
		t.Fatalf("prompts len = %d, want %d", len(loaded.Prompts), maxAIAssistantUIStatePrompts)
	}
	for _, prompt := range loaded.Prompts {
		if prompt != "prompt" {
			t.Fatalf("prompt = %q, want trimmed prompt", prompt)
		}
	}
	if loaded.ContextBoundaryMessageID != "boundary" {
		t.Fatalf("ContextBoundaryMessageID = %q", loaded.ContextBoundaryMessageID)
	}
}

func TestAIAssistantUIStateStripsAssistantHiddenAndToolCallContent(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := app.aiAssistantUIStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	raw := `{"messages":[{"id":"a1","role":"assistant","content":"visible\n<details><summary>思考过程</summary>hidden</details>\n<tool_call[]>\n{\"name\":\"write_file\",\"arguments\":{\"path\":\"a.txt\",\"content\":\"x\"}}"},{"id":"u1","role":"user","content":"<tool_call[]> keep user text"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if got := loaded.Messages[0]["content"]; got != "visible" {
		t.Fatalf("assistant content = %#v, want visible", got)
	}
	if got := loaded.Messages[1]["content"]; got != "<tool_call[]> keep user text" {
		t.Fatalf("user content = %#v, want untouched user content", got)
	}
	sanitized, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(sanitized), "<details") || strings.Contains(string(sanitized), "hidden") || strings.Contains(string(sanitized), "write_file") {
		t.Fatalf("sanitized file still contains assistant hidden/tool content: %s", sanitized)
	}
}
