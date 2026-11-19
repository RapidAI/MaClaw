package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIncludesAndRestoresArchiveSnapshot(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "active-original", Content: "original active memory", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	if err := store.archive.AddDurable(Entry{ID: "archive-original", Content: "original archived memory", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	compressor := NewCompressor(store, nil, nil)
	backupName, err := compressor.createBackup()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(compressor.backupDir(), backupName))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := decodeMemoryBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || len(snapshot.Archive) != 1 || snapshot.Archive[0].ID != "archive-original" {
		t.Fatalf("backup snapshot = %+v", snapshot)
	}
	if err := store.Update("active-original", "changed active memory", CategoryUserFact, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.archive.ReplaceEntriesDurable([]Entry{{ID: "archive-changed", Content: "changed archived memory", Category: CategoryUserFact}}); err != nil {
		t.Fatal(err)
	}
	if err := compressor.RestoreBackup(backupName); err != nil {
		t.Fatal(err)
	}
	active := store.SearchDirectByID("active-original")
	if len(active) != 1 || active[0].Content != "original active memory" {
		t.Fatalf("active after restore = %+v", active)
	}
	archived := store.archive.List("", "")
	if len(archived) != 1 || archived[0].ID != "archive-original" || archived[0].Content != "original archived memory" {
		t.Fatalf("archive after restore = %+v", archived)
	}
}

func TestLegacyBackupRestoreClearsUnmatchedArchive(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.archive.AddDurable(Entry{ID: "ghost", Content: "archived memory absent from legacy backup", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]Entry{{ID: "legacy", Content: "legacy active memory", Category: CategoryUserFact}})
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(dir, "memory_backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backupName := "legacy.json"
	if err := os.WriteFile(filepath.Join(backupDir, backupName), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	compressor := NewCompressor(store, nil, nil)
	if err := compressor.RestoreBackup(backupName); err != nil {
		t.Fatal(err)
	}
	if entries := store.SearchDirectByID("legacy"); len(entries) != 1 {
		t.Fatalf("legacy active entries = %+v", entries)
	}
	if archived := store.archive.List("", ""); len(archived) != 0 {
		t.Fatalf("legacy restore left unmatched archive entries: %+v", archived)
	}
}
