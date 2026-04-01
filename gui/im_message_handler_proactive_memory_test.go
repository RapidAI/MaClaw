package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Task 6.3: Unit tests for proactive memory instruction in system prompt
// ---------------------------------------------------------------------------

func TestSystemPrompt_FirstTurn_ContainsProactiveMemoryInstruction(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := NewMemoryStore(memPath)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	defer ms.Stop()

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.memoryStore = ms

	// isFirstTurn = true → should contain proactive memory instruction
	prompt := h.buildSystemPromptWithMemory("hello", true)

	keywords := []string{
		"用户记忆",
		"记忆管理指引",
		"主动调用 memory(action: save)",
		"user_fact | preference | project_knowledge | instruction",
	}
	for _, kw := range keywords {
		if !strings.Contains(prompt, kw) {
			t.Errorf("first-turn prompt missing keyword %q", kw)
		}
	}
}

func TestSystemPrompt_NonFirstTurn_NoProactiveMemoryInstruction(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := NewMemoryStore(memPath)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	defer ms.Stop()

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.memoryStore = ms

	// isFirstTurn = false → should NOT contain proactive memory instruction
	prompt := h.buildSystemPromptWithMemory("hello", false)

	if strings.Contains(prompt, "主动记忆") {
		t.Error("non-first-turn prompt should not contain proactive memory instruction")
	}
}

func TestSystemPrompt_NoMemoryStore_NoProactiveInstruction(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})
	// memoryStore is nil

	prompt := h.buildSystemPrompt()

	if strings.Contains(prompt, "主动记忆") {
		t.Error("prompt without memory store should not contain proactive memory instruction")
	}
}
