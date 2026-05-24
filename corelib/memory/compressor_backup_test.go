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

func TestRestoreBackupReplacesSQLiteBackendSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStoreWithMode(tmpDir, StoreModeSQLite)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	t.Cleanup(store.Stop)

	if err := store.Save(Entry{ID: "restore-old", Content: "old sqlite memory", Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1}); err != nil {
		t.Fatalf("Save old: %v", err)
	}
	restored := []Entry{{ID: "restore-new", Content: "restored sqlite memory", Category: CategoryInstruction, Strength: 1, AccessCount: 3}}
	data, err := json.MarshalIndent(restored, "", "  ")
	if err != nil {
		t.Fatalf("marshal restore snapshot: %v", err)
	}
	backupDir := filepath.Join(tmpDir, "memory_backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	backupName := "restore_snapshot.json"
	if err := os.WriteFile(filepath.Join(backupDir, backupName), data, 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	comp := NewCompressor(store, nil, nil)
	if err := comp.RestoreBackup(backupName); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	loaded, err := store.backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "restore-new" || loaded[0].Category != CategoryInstruction {
		t.Fatalf("sqlite backend snapshot not replaced: %+v", loaded)
	}
	if store.sync == nil || store.sync.lastVersion == 0 {
		t.Fatalf("restore should advance sync watermark, sync=%+v", store.sync)
	}
}
