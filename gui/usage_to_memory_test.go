package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestUsagePatternBridgeWritesRoutingHintsAndSkillNudges(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_observe",
			QueryTokens:  []string{"browser", "button"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_observe", "browser_click"},
			Success:      true,
			FinalOutcome: "completed",
			Timestamp:    now,
		})
	}

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	bridge := NewUsagePatternBridge(tracker, store)
	bridge.RunOnce()

	entries := store.List(memory.CategoryProjectKnowledge, "")
	if !hasMemoryEntryWithTag(entries, "usage_routing_hint") {
		t.Fatalf("expected routing hint memory entry, got %#v", entries)
	}
	if hasMemoryEntryWithTag(entries, "skill_nudge_candidate") {
		t.Fatalf("unexpected legacy browser skill nudge memory entry, got %#v", entries)
	}
	for _, entry := range entries {
		if hasTag(entry.Tags, "usage_routing_hint") {
			if entry.SourceType != "tool_usage" {
				t.Fatalf("learned usage entry source_type = %q, want tool_usage", entry.SourceType)
			}
		}
	}
}

func TestUsagePatternBridgeUpsertsExistingHint(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	bridge := NewUsagePatternBridge(nil, store)

	created, updated := bridge.upsertUsageMemory("first", []string{"usage_routing_hint", "task:test"})
	if !created || updated {
		t.Fatalf("first upsert created/updated = %v/%v", created, updated)
	}
	created, updated = bridge.upsertUsageMemory("second", []string{"usage_routing_hint", "task:test"})
	if created || !updated {
		t.Fatalf("second upsert created/updated = %v/%v", created, updated)
	}
	entries := store.List(memory.CategoryProjectKnowledge, "")
	if len(entries) != 1 || entries[0].Content != "second" {
		t.Fatalf("expected single updated entry, got %#v", entries)
	}
}

func hasMemoryEntryWithTag(entries []memory.Entry, tag string) bool {
	for _, entry := range entries {
		if hasTag(entry.Tags, tag) {
			return true
		}
	}
	return false
}

func TestUsagePatternBridgeWritesRecoveryPattern(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_click",
			QueryTokens:  []string{"browser", "button"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_click", "browser_observe"},
			Success:      false,
			ErrorClass:   "element_missing",
			RecoveryTool: "browser_observe",
			FinalOutcome: "recovered",
			Timestamp:    now,
		})
	}

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	bridge := NewUsagePatternBridge(tracker, store)
	bridge.RunOnce()

	entries := store.List(memory.CategoryProjectKnowledge, "")
	if hasMemoryEntryWithTag(entries, "tool_recovery_pattern") {
		t.Fatalf("unexpected same-browser recovery pattern memory entry, got %#v", entries)
	}
}
