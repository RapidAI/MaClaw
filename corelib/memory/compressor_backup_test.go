package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressorCreatesBackupWhenMemoryFileMissing(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	store, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	if _, err := os.Stat(memPath); !os.IsNotExist(err) {
		t.Fatalf("expected fresh store to have no memory file, stat err=%v", err)
	}

	comp := NewCompressor(store, nil, nil)
	result, err := comp.Compress(context.Background())
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result.BackupName == "" {
		t.Fatal("expected backup name")
	}

	backupPath := filepath.Join(tmpDir, "memory_backups", result.BackupName)
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("backup should be valid JSON entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty backup, got %d entries", len(entries))
	}
}

func TestCompressorCreatesBackupAfterPartitionMigration(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")

	legacyEntries := make([]Entry, 0, 101)
	for i := 0; i < 101; i++ {
		legacyEntries = append(legacyEntries, Entry{
			ID:       fmt.Sprintf("entry-%03d", i),
			Content:  fmt.Sprintf("legacy memory entry %03d", i),
			Category: CategoryProjectKnowledge,
		})
	}
	data, err := json.MarshalIndent(legacyEntries, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy entries: %v", err)
	}
	if err := os.WriteFile(memPath, data, 0o644); err != nil {
		t.Fatalf("write legacy memories: %v", err)
	}

	store, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	if _, err := os.Stat(memPath + ".migrated"); err != nil {
		t.Fatalf("expected legacy file to be renamed after migration: %v", err)
	}
	if _, err := os.Stat(memPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy path to be absent after migration, stat err=%v", err)
	}

	comp := NewCompressor(store, nil, nil)
	result, err := comp.Compress(context.Background())
	if err != nil {
		t.Fatalf("Compress after migration: %v", err)
	}

	backupPath := filepath.Join(tmpDir, "memory_backups", result.BackupName)
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var backedUp []Entry
	if err := json.Unmarshal(backupData, &backedUp); err != nil {
		t.Fatalf("backup should be valid JSON entries: %v", err)
	}
	if len(backedUp) != len(legacyEntries) {
		t.Fatalf("expected %d backed up entries, got %d", len(legacyEntries), len(backedUp))
	}
}

func TestCompressorBackupUsesLoadedPartitionsWhenLegacyFileIsStale(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	partPath := filepath.Join(tmpDir, "part_project.json")

	partitionEntries := make([]Entry, 0, 3)
	for i := 0; i < 3; i++ {
		partitionEntries = append(partitionEntries, Entry{
			ID:       fmt.Sprintf("partition-entry-%d", i),
			Content:  fmt.Sprintf("partition memory entry %d", i),
			Category: CategoryProjectKnowledge,
		})
	}
	partData, err := json.MarshalIndent(partitionEntries, "", "  ")
	if err != nil {
		t.Fatalf("marshal partition entries: %v", err)
	}
	if err := os.WriteFile(partPath, partData, 0o644); err != nil {
		t.Fatalf("write partition file: %v", err)
	}

	staleLegacy := []Entry{{ID: "stale", Content: "stale legacy entry", Category: CategoryProjectKnowledge}}
	legacyData, err := json.MarshalIndent(staleLegacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale legacy: %v", err)
	}
	if err := os.WriteFile(memPath, legacyData, 0o644); err != nil {
		t.Fatalf("write stale legacy file: %v", err)
	}

	store, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	comp := NewCompressor(store, nil, nil)
	result, err := comp.Compress(context.Background())
	if err != nil {
		t.Fatalf("Compress with stale legacy file: %v", err)
	}

	backupPath := filepath.Join(tmpDir, "memory_backups", result.BackupName)
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var backedUp []Entry
	if err := json.Unmarshal(backupData, &backedUp); err != nil {
		t.Fatalf("backup should be valid JSON entries: %v", err)
	}
	if len(backedUp) != len(partitionEntries) {
		t.Fatalf("expected %d partition entries in backup, got %d", len(partitionEntries), len(backedUp))
	}
	for _, entry := range backedUp {
		if entry.ID == "stale" {
			t.Fatal("backup used stale legacy memories.json instead of loaded partitions")
		}
	}
}
