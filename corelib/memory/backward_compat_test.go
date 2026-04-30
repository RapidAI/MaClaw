package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"
)

// Feature: memory-claude-style-upgrade, Property 19: Existing operations unchanged
// **Validates: Requirements 6.4, 6.5**
//
// For any sequence of save/list/search/delete operations on a Memory_Store,
// the results should be identical whether or not the new Pinned field,
// ArchiveStore, and GC features are present. Specifically, saving an entry
// and then listing/searching should return it, and deleting should remove it.
func TestProperty_ExistingOpsUnchanged(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Generate a random number of entries to save.
		n := rapid.IntRange(1, 20).Draw(rt, "numEntries")
		type savedInfo struct {
			content  string
			category Category
			tags     []string
		}
		var saved []savedInfo

		for i := 0; i < n; i++ {
			content := rapid.StringMatching(`[a-zA-Z0-9]{10,60}`).Draw(rt, fmt.Sprintf("content_%d", i))
			cat := genCategory().Draw(rt, fmt.Sprintf("cat_%d", i))
			nTags := rapid.IntRange(0, 3).Draw(rt, fmt.Sprintf("nTags_%d", i))
			tags := make([]string, nTags)
			for j := range tags {
				tags[j] = rapid.StringMatching(`[a-z]{2,8}`).Draw(rt, fmt.Sprintf("tag_%d_%d", i, j))
			}

			entry := Entry{
				Content:  content,
				Category: cat,
				Tags:     tags,
			}
			if err := store.Save(entry); err != nil {
				rt.Fatalf("Save failed for entry %d: %v", i, err)
			}
			saved = append(saved, savedInfo{content: content, category: cat, tags: tags})
		}

		// Verify: List with no filter returns all saved entries.
		listed := store.List("", "")
		if len(listed) != n {
			rt.Fatalf("List returned %d entries, expected %d", len(listed), n)
		}

		// Verify: each saved entry can be found by listing with its category.
		for _, s := range saved {
			catResults := store.List(s.category, "")
			found := false
			for _, e := range catResults {
				if e.Content == s.content {
					found = true
					break
				}
			}
			if !found {
				rt.Fatalf("entry with content %q not found when listing category %q", s.content, s.category)
			}
		}

		// Verify: Search with keyword finds matching entries.
		for _, s := range saved {
			if len(s.content) < 5 {
				continue
			}
			keyword := s.content[:5]
			searchResults := store.Search("", keyword, 100)
			found := false
			for _, e := range searchResults {
				if e.Content == s.content {
					found = true
					break
				}
			}
			if !found {
				rt.Fatalf("entry with content %q not found when searching keyword %q", s.content, keyword)
			}
		}

		// Verify: Delete removes the entry.
		// Pick a random entry to delete.
		delIdx := rapid.IntRange(0, n-1).Draw(rt, "delIdx")
		allEntries := store.List("", "")
		targetID := allEntries[delIdx].ID
		targetContent := allEntries[delIdx].Content

		if err := store.Delete(targetID); err != nil {
			rt.Fatalf("Delete failed: %v", err)
		}

		// After delete, the entry should not appear in list.
		afterDelete := store.List("", "")
		if len(afterDelete) != n-1 {
			rt.Fatalf("after delete: expected %d entries, got %d", n-1, len(afterDelete))
		}
		for _, e := range afterDelete {
			if e.ID == targetID {
				rt.Fatalf("deleted entry %q still present in list", targetID)
			}
		}

		// After delete, search should not find the deleted entry.
		if len(targetContent) >= 5 {
			searchAfter := store.Search("", targetContent[:5], 100)
			for _, e := range searchAfter {
				if e.ID == targetID {
					rt.Fatalf("deleted entry %q still found in search", targetID)
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Unit Tests for backward compatibility
// ---------------------------------------------------------------------------

// TestBackwardCompat_LoadNoPinnedField verifies that loading a JSON file
// with entries that lack the "pinned" field results in Pinned==false.
func TestBackwardCompat_LoadNoPinnedField(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memory.json")

	// Write a JSON array of entries WITHOUT the "pinned" field.
	entries := []map[string]interface{}{
		{
			"id":           "entry-1",
			"content":      "old entry without pinned field",
			"category":     "project_knowledge",
			"tags":         []string{"legacy"},
			"created_at":   "2024-01-01T00:00:00Z",
			"updated_at":   "2024-01-01T00:00:00Z",
			"access_count": 5,
			"strength":     1.0,
		},
		{
			"id":           "entry-2",
			"content":      "another old entry",
			"category":     "preference",
			"tags":         []string{"old"},
			"created_at":   "2024-02-01T00:00:00Z",
			"updated_at":   "2024-02-01T00:00:00Z",
			"access_count": 3,
		},
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Stop()

	loaded := store.List("", "")
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
	for _, e := range loaded {
		if e.Pinned {
			t.Fatalf("entry %q has Pinned=true, expected false", e.ID)
		}
	}
}

// TestBackwardCompat_NoArchiveJSON verifies that Store initializes normally
// when no archive.json exists, and that the first eviction creates the file.
func TestBackwardCompat_NoArchiveJSON(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memory.json")

	// Ensure no archive.json exists.
	archivePath := filepath.Join(dir, "archive.json")
	if _, err := os.Stat(archivePath); err == nil {
		t.Fatal("archive.json should not exist before test")
	}

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed without archive.json: %v", err)
	}
	defer store.Stop()

	// Store should be functional.
	if err := store.Save(Entry{
		Content:  "test entry",
		Category: CategoryProjectKnowledge,
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	listed := store.List("", "")
	if len(listed) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(listed))
	}

	// Archive should be initialized (even if empty).
	if store.Archive() == nil {
		t.Fatal("archive should be initialized")
	}
	if store.Archive().Count() != 0 {
		t.Fatalf("archive should be empty, got %d", store.Archive().Count())
	}

	// Trigger eviction by filling to capacity + 1.
	// Temporarily lower maxItems for test performance.
	store.mu.Lock()
	store.maxItems = 500
	store.mu.Unlock()
	for i := 0; i < 500; i++ {
		if err := store.Save(Entry{
			Content:  fmt.Sprintf("fill-entry-%d", i),
			Category: CategoryProjectKnowledge,
		}); err != nil {
			t.Fatalf("Save fill entry %d failed: %v", i, err)
		}
	}

	// Archive should now have entries from eviction.
	if store.Archive().Count() == 0 {
		t.Fatal("archive should have entries after eviction")
	}

	// Flush archive and verify file was created.
	if err := store.Archive().Flush(); err != nil {
		t.Fatalf("archive Flush failed: %v", err)
	}
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Fatal("archive.json should exist after eviction and flush")
	}
}

// TestKnowledgeExtractor_NilLLMSkips verifies that KnowledgeExtractor
// with nil LLM returns nil without error.
func TestKnowledgeExtractor_NilLLMSkips(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memory.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Create extractor with nil LLM.
	ke := NewKnowledgeExtractor(store, nil)

	msgs := []ConversationMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	err = ke.Extract("testuser", msgs)
	if err != nil {
		t.Fatalf("expected nil error with nil LLM, got: %v", err)
	}

	// No entries should have been added.
	if len(store.List("", "")) != 0 {
		t.Fatal("expected no entries saved when LLM is nil")
	}
}

// TestKnowledgeExtractor_LLMErrorNoImpact verifies that when the LLM
// returns an error, Extract returns an error but does not panic.
func TestKnowledgeExtractor_LLMErrorNoImpact(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memory.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	llm := &mockLLMCaller{
		configured: true,
		err:        fmt.Errorf("simulated LLM failure"),
	}

	ke := NewKnowledgeExtractor(store, llm)
	ke.cooldown = 0 // disable cooldown for testing

	msgs := []ConversationMessage{
		{Role: "user", Content: "what is the config format?"},
		{Role: "assistant", Content: "it uses YAML with nested keys"},
	}

	// Should return error but not panic.
	err = ke.Extract("testuser", msgs)
	if err == nil {
		t.Fatal("expected error from Extract when LLM fails")
	}

	// Store should be unaffected — no entries added.
	if len(store.List("", "")) != 0 {
		t.Fatal("expected no entries saved when LLM returns error")
	}
}
