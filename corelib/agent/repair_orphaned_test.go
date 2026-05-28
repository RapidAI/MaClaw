package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepairOrphanedToolEntries_NoOrphans_ZeroAllocation(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "done"},
	}
	result := repairOrphanedToolEntries(entries)
	// Same slice — no allocation when no tool entries exist.
	if &result[0] != &entries[0] {
		t.Error("expected same slice (zero allocation) when no tool entries exist")
	}
}

func TestRepairOrphanedToolEntries_ValidGroup_Unchanged(t *testing.T) {
	toolCalls := []map[string]interface{}{
		{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "bash"}},
	}
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", ToolCalls: toolCalls},
		{Role: "tool", Content: "ok", ToolCallID: "call_1"},
		{Role: "assistant", Content: "done"},
	}
	result := repairOrphanedToolEntries(entries)
	// Same slice — no allocation when all tool entries are valid.
	if &result[0] != &entries[0] {
		t.Error("expected same slice (zero allocation) when all tool entries are valid")
	}
}

func TestRepairOrphanedToolEntries_RemovesOrphan(t *testing.T) {
	toolCalls := []map[string]interface{}{
		{"id": "call_B", "type": "function", "function": map[string]interface{}{"name": "compress_context"}},
	}
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "system", Content: "summary"},
		{Role: "tool", Content: "743.6", ToolCallID: "call_A"}, // orphan — no parent
		{Role: "assistant", Content: "PDF done!"},
		{Role: "assistant", Content: "compressing", ToolCalls: toolCalls},
		{Role: "tool", Content: "queued", ToolCallID: "call_B"}, // valid — parent at [4]
		{Role: "assistant", Content: "done"},
	}
	result := repairOrphanedToolEntries(entries)
	if len(result) != 6 {
		t.Fatalf("expected 6 entries (1 orphan removed), got %d", len(result))
	}
	// Verify the orphan is gone and the valid tool entry is kept.
	for _, e := range result {
		if e.Role == "tool" && e.ToolCallID == "call_A" {
			t.Error("orphaned tool entry (call_A) should have been removed")
		}
	}
	foundCallB := false
	for _, e := range result {
		if e.Role == "tool" && e.ToolCallID == "call_B" {
			foundCallB = true
		}
	}
	if !foundCallB {
		t.Error("valid tool entry (call_B) should have been kept")
	}
}

func TestRepairOrphanedToolEntries_EmptyToolCallID(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: "data", ToolCallID: ""},
	}
	result := repairOrphanedToolEntries(entries)
	if len(result) != 1 {
		t.Errorf("expected 1 entry (orphan with empty tcid removed), got %d", len(result))
	}
}

func TestLoadFromDisk_RepairsOrphanedToolEntries(t *testing.T) {
	// Write a corrupted conversation file with an orphaned tool entry,
	// then verify loadFromDisk repairs it and marks dirty for rewrite.
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conv.json")

	toolCalls := []map[string]interface{}{
		{"id": "call_B", "type": "function", "function": map[string]interface{}{"name": "compress_context"}},
	}
	tcJSON, _ := json.Marshal(toolCalls)
	var tcRaw interface{}
	json.Unmarshal(tcJSON, &tcRaw)

	snapshot := memorySnapshot{
		Sessions: map[string]persistedSession{
			"test-user": {
				Entries: []ConversationEntry{
					{Role: "user", Content: "hello"},
					{Role: "system", Content: "summary"},
					{Role: "tool", Content: "orphan data", ToolCallID: "call_MISSING"},
					{Role: "assistant", Content: "text"},
					{Role: "assistant", Content: "compress", ToolCalls: tcRaw},
					{Role: "tool", Content: "queued", ToolCallID: "call_B"},
				},
			},
		},
	}
	data, _ := json.Marshal(snapshot)
	os.WriteFile(storePath, data, 0644)

	cm := NewConversationMemory()
	cm.storePath = storePath
	if err := cm.loadFromDisk(); err != nil {
		t.Fatalf("loadFromDisk failed: %v", err)
	}

	entries := cm.Load("test-user")
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (1 orphan repaired), got %d", len(entries))
	}

	// Verify the orphan is gone.
	for _, e := range entries {
		if e.Role == "tool" && e.ToolCallID == "call_MISSING" {
			t.Error("orphaned tool entry should have been repaired on load")
		}
	}

	if err := cm.FlushNow(); err != nil {
		t.Fatalf("FlushNow failed: %v", err)
	}
	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	for _, e := range reloaded.Load("test-user") {
		if e.Role == "tool" && e.ToolCallID == "call_MISSING" {
			t.Error("orphaned tool entry should have been removed from persisted rewrite")
		}
	}
}
