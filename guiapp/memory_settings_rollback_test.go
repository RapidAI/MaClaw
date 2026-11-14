package guiapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSetMemoryMaxBackupsDoesNotPersistWhenPruneFails(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	maintenance := memory.NewMaintenance(store, nil, nil)
	app := &App{memoryStore: store, memoryMaintenance: maintenance, testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		maintenance.Stop()
		store.Stop()
	})
	backupDir := filepath.Join(dir, "memory_backups")
	if err := os.WriteFile(backupDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.SetMaxBackups(memory.MinBackups); err == nil {
		t.Fatal("precondition: pruning should fail when the backup path is not a directory")
	}
	if err := app.SetMemoryMaxBackups(memory.MinBackups); err == nil {
		t.Fatal("SetMemoryMaxBackups should fail when pruning cannot read the backup directory")
	}
	if got := app.GetMemoryMaxBackups(); got != memory.DefaultMaxBackups {
		t.Fatalf("backup retention persisted after failure: %d", got)
	}
}

func TestSetAutoCompressRollsBackPipelineWhenConfigWriteFails(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	maintenance := memory.NewMaintenance(store, nil, nil)
	invalidHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidHome, []byte("file instead of directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{memoryStore: store, memoryMaintenance: maintenance, testHomeDir: invalidHome}
	app.setMemoryPipeline(maintenance.Pipeline())
	t.Cleanup(func() {
		maintenance.Stop()
		store.Stop()
	})
	if err := app.SetAutoCompress(true); err == nil {
		t.Fatal("SetAutoCompress should fail when config cannot be written")
	}
	if maintenance.Pipeline().AutoCompressEnabled() {
		t.Fatal("pipeline retained enabled state after config write failure")
	}
}
