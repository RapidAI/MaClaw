package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestWriteMemoryRefFileStoresFullContent(t *testing.T) {
	base := t.TempDir()
	content := "first line\n" + strings.Repeat("full detail ", 120) + "tail sentinel"
	createdAt := time.Date(2026, 5, 18, 10, 11, 12, 13, time.UTC)

	refPath, err := writeMemoryRefFile(filepath.Join(base, "memories.json"), "user/A", "conversation_trim", content, createdAt)
	if err != nil {
		t.Fatalf("writeMemoryRefFile: %v", err)
	}
	if !strings.HasPrefix(refPath, filepath.Join(base, "memory_refs", "conversation_trim", "user_A", "2026-05")) {
		t.Fatalf("ref path %q is not under expected memory_refs directory", refPath)
	}
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("read ref: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "tail sentinel") || !strings.Contains(body, "sha256:") {
		t.Fatalf("ref file did not preserve full content and metadata: %q", body)
	}
}

func TestTrimHistoryMemorySinkReceivesFullDroppedAssistantText(t *testing.T) {
	longText := strings.Repeat("important detail ", 90) + "FULL_SENTINEL_AT_END"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "start task"},
		{Role: "assistant", Content: "initial plan"},
		{Role: "assistant", Content: longText},
	}
	for i := 0; i < 30; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: "recent step"})
	}

	var captured string
	_ = trimHistoryWithSummary(entries, nil, func(content string, tags []string) {
		captured = content
	}, 10, 0)

	if !strings.Contains(captured, "FULL_SENTINEL_AT_END") {
		t.Fatalf("memorySink received truncated content; got %d runes", len([]rune(captured)))
	}
	if len([]rune(captured)) <= memoryRefPreviewRunes {
		t.Fatalf("test setup did not capture full over-preview content, got %d runes", len([]rune(captured)))
	}
}
