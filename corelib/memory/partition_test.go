package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPartition_FreshInstall_UsesLegacy(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memories.json")

	s, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Stop()

	// Fresh install: no legacy file, no partition files.
	// Partitions should NOT be enabled — small stores use legacy mode.
	if s.partMgr != nil && s.partMgr.isEnabled() {
		t.Fatal("expected partitions to NOT be enabled on fresh install")
	}

	// Save some entries.
	_ = s.Save(Entry{Content: "user fact about programming preferences and tools", Category: CategoryUserFact})
	_ = s.Save(Entry{Content: "project knowledge about database configuration details", Category: CategoryProjectKnowledge})

	// Flush to disk — should write to legacy file.
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Legacy file should exist.
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		t.Error("expected legacy memories.json to exist on fresh install")
	}

	// Partition files should NOT exist.
	if _, err := os.Stat(filepath.Join(dir, "part_user.json")); !os.IsNotExist(err) {
		t.Error("expected part_user.json to NOT exist on fresh install")
	}
}

func TestPartition_MigrationFromLegacy(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memories.json")

	// Create a legacy store with 100+ entries to trigger migration.
	s1, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore (legacy): %v", err)
	}
	// Disable partitions to simulate legacy behavior.
	s1.partMgr = nil
	for i := 0; i < 110; i++ {
		// Use ConversationSummary for all entries to avoid semantic dedup
		// merging template-similar entries during Save. ConversationSummary
		// is excluded from semantic dedup.
		_ = s1.Save(Entry{
			Content:  fmt.Sprintf("legacy entry number %d with enough content to be unique and meaningful", i),
			Category: CategoryConversationSummary,
		})
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush legacy: %v", err)
	}
	s1.Stop()

	// Verify legacy file exists.
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		t.Fatal("expected legacy memories.json to exist")
	}

	// Open with a new store — should trigger migration (>100 entries).
	s2, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore (migration): %v", err)
	}
	defer s2.Stop()

	// Verify entries were loaded.
	if len(s2.entries) != 110 {
		t.Fatalf("expected 110 entries after migration, got %d", len(s2.entries))
	}

	// Verify partitions are enabled.
	if s2.partMgr == nil || !s2.partMgr.isEnabled() {
		t.Fatal("expected partitions to be enabled after migration")
	}

	// Verify legacy file was renamed.
	if _, err := os.Stat(memPath + ".migrated"); os.IsNotExist(err) {
		t.Error("expected memories.json.migrated to exist after migration")
	}

	// Verify partition files exist.
	if _, err := os.Stat(filepath.Join(dir, "part_episodic.json")); os.IsNotExist(err) {
		t.Error("expected part_episodic.json after migration")
	}
}

func TestPartition_ReloadFromPartitions(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memories.json")

	// Create a legacy store with 100+ entries to trigger migration.
	s1, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s1.partMgr = nil // force legacy mode for initial save
	for i := 0; i < 105; i++ {
		cat := CategoryProjectKnowledge
		if i%5 == 0 {
			cat = CategoryPreference
		} else if i%5 == 1 {
			cat = CategoryTaskArtifact
		}
		// Use CategoryConversationSummary for most entries — it's excluded
		// from semantic dedup, so entries won't be merged during Save.
		if cat == CategoryProjectKnowledge {
			cat = CategoryConversationSummary
		}
		_ = s1.Save(Entry{
			Content:  fmt.Sprintf("entry %d with unique content for reload test verification", i),
			Category: cat,
		})
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s1.Stop()

	// Open — triggers migration to partitions.
	s2, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore (migrate): %v", err)
	}
	if !s2.partMgr.isEnabled() {
		t.Fatal("expected partitions enabled after migration")
	}
	// Add one more entry and flush to partition files.
	_ = s2.Save(Entry{Content: "new entry after migration for partition reload test", Category: CategoryProjectKnowledge})
	if err := s2.Flush(); err != nil {
		t.Fatalf("Flush partitions: %v", err)
	}
	s2.Stop()

	// Reload from partition files.
	s3, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	defer s3.Stop()

	if len(s3.entries) != 106 {
		t.Fatalf("expected 106 entries after reload, got %d", len(s3.entries))
	}
	if !s3.partMgr.isEnabled() {
		t.Fatal("expected partitions enabled after reload from partition files")
	}
}

func TestPartition_RecallDynamicWorksAcrossPartitions(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memories.json")

	s, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Stop()

	// Save entries in different partitions.
	_ = s.Save(Entry{Content: "SSH server configuration host=api.rapidai.tech port=33", Category: CategoryProjectKnowledge, Tags: []string{"ssh", "server"}})
	_ = s.Save(Entry{Content: "user prefers vim editor for all coding tasks", Category: CategoryPreference})
	_ = s.Save(Entry{Content: "task artifact: snake game requirements document with controls", Category: CategoryTaskArtifact, Tags: []string{"snake", "game"}})

	// RecallDynamic should find entries across all partitions.
	results := s.RecallDynamic("SSH server", "", "")
	if len(results) == 0 {
		t.Error("expected RecallDynamic to find SSH entry across partitions")
	}
}

func TestPartition_SmallStore_UsesLegacy(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memories.json")

	s, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Stop()

	// Small store (<100 entries) should use legacy mode.
	_ = s.Save(Entry{Content: "user fact about programming language preferences", Category: CategoryUserFact})
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Legacy file should exist.
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("failed to read legacy file: %v", err)
	}
	if len(data) < 10 {
		t.Error("expected legacy file to have content")
	}

	// Partition files should NOT exist.
	for _, g := range partitionGroups {
		if _, err := os.Stat(filepath.Join(dir, g.FileName)); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist for small store", g.FileName)
		}
	}
}

func TestPartitionManager_PartitionIndexFor(t *testing.T) {
	pm := newPartitionManager(t.TempDir())

	tests := []struct {
		cat      Category
		expected string
	}{
		{CategorySelfIdentity, "identity"},
		{CategoryUserFact, "user"},
		{CategoryUser, "user"},
		{CategoryPreference, "user"},
		{CategoryInstruction, "user"},
		{CategoryFeedback, "user"},
		{CategoryProjectKnowledge, "project"},
		{CategoryProject, "project"},
		{CategoryReference, "project"},
		{CategoryTaskArtifact, "project"},
		{CategoryConversationSummary, "episodic"},
		{CategorySessionCheckpoint, "episodic"},
		{CategoryProfile, "profile"},
	}

	for _, tt := range tests {
		name := pm.partitionNameFor(tt.cat)
		if name != tt.expected {
			t.Errorf("category %q: expected partition %q, got %q", tt.cat, tt.expected, name)
		}
	}
}
