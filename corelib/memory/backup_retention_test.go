package memory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupPruneReportsFailuresWithoutHidingFiles(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	compressor := NewCompressor(store, nil, nil)
	backupDir := compressor.backupDir()
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < MinBackups+1; i++ {
		path := filepath.Join(backupDir, "backup_"+string(rune('a'+i))+".json")
		if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	previousRemove := removeBackupFile
	removeBackupFile = func(path string) error {
		if filepath.Base(path) == "backup_a.json" {
			return errors.New("forced remove failure")
		}
		return os.Remove(path)
	}
	defer func() { removeBackupFile = previousRemove }()
	if err := compressor.SetMaxBackups(MinBackups); err == nil {
		t.Fatal("SetMaxBackups should report the pruning failure")
	}
	files, err := compressor.pruneOldBackups()
	if err == nil {
		t.Fatal("pruneOldBackups should return the deletion error")
	}
	if len(files) != MinBackups+1 {
		t.Fatalf("surviving files = %d, want %d; failed deletion was hidden", len(files), MinBackups+1)
	}
	if _, statErr := os.Stat(filepath.Join(backupDir, "backup_a.json")); statErr != nil {
		t.Fatalf("failed backup file disappeared from disk: %v", statErr)
	}
}
