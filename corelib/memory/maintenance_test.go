package memory

import (
	"context"
	"testing"
)

func TestMaintenanceCompressAndBackupLifecycle(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	for i := 0; i < 2; i++ {
		if err := store.Save(Entry{Content: "same durable memory content", Category: CategoryProjectKnowledge}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	maintenance := NewMaintenance(store, nil, nil)
	result, err := maintenance.Compress(context.Background())
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result.BackupName == "" {
		t.Fatal("Compress did not create a backup")
	}

	backups, err := maintenance.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) == 0 {
		t.Fatal("ListBackups returned no backups")
	}

	if err := maintenance.DeleteBackup(backups[0].Name); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
}

func TestMaintenanceNilStoreReturnsErrors(t *testing.T) {
	maintenance := NewMaintenance(nil, nil, nil)
	if _, err := maintenance.Compress(context.Background()); err == nil {
		t.Fatal("Compress should reject nil store")
	}
	if _, err := maintenance.ListBackups(); err == nil {
		t.Fatal("ListBackups should reject nil store")
	}
	if err := maintenance.RestoreBackup("missing.json"); err == nil {
		t.Fatal("RestoreBackup should reject nil store")
	}
	if err := maintenance.DeleteBackup("missing.json"); err == nil {
		t.Fatal("DeleteBackup should reject nil store")
	}
	if err := maintenance.StartCompressor(); err == nil {
		t.Fatal("StartCompressor should reject nil store")
	}
	if _, err := maintenance.CompressorStatus(); err == nil {
		t.Fatal("CompressorStatus should reject nil store")
	}
	if err := maintenance.SetMaxBackups(DefaultMaxBackups); err == nil {
		t.Fatal("SetMaxBackups should reject nil store")
	}
}

func TestNilMaintenanceReturnsErrors(t *testing.T) {
	var maintenance *Maintenance
	if _, err := maintenance.Compress(context.Background()); err == nil {
		t.Fatal("Compress should reject nil maintenance")
	}
	if _, err := maintenance.ListBackups(); err == nil {
		t.Fatal("ListBackups should reject nil maintenance")
	}
	if err := maintenance.RestoreBackup("missing.json"); err == nil {
		t.Fatal("RestoreBackup should reject nil maintenance")
	}
	if err := maintenance.DeleteBackup("missing.json"); err == nil {
		t.Fatal("DeleteBackup should reject nil maintenance")
	}
	if err := maintenance.StopCompressor(); err == nil {
		t.Fatal("StopCompressor should reject nil maintenance")
	}
	if _, err := maintenance.IsCompressing(); err == nil {
		t.Fatal("IsCompressing should reject nil maintenance")
	}
	if err := maintenance.SetMaxBackups(DefaultMaxBackups); err == nil {
		t.Fatal("SetMaxBackups should reject nil maintenance")
	}
}
func TestMaintenanceStopHaltsOwnedLoops(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	maintenance := NewMaintenance(store, nil, nil)
	maintenance.Start()
	if running, _, _ := maintenance.Pipeline().Status(); !running {
		t.Fatal("pipeline should be running after Maintenance.Start")
	}
	maintenance.Compressor().Start()
	if !maintenance.Compressor().IsRunning() {
		t.Fatal("compressor should be running after Compressor.Start")
	}

	maintenance.Stop()
	if running, _, _ := maintenance.Pipeline().Status(); running {
		t.Fatal("pipeline should stop after Maintenance.Stop")
	}
	if maintenance.Compressor().IsRunning() {
		t.Fatal("compressor should stop after Maintenance.Stop")
	}
}
func TestMaintenanceBuildsSharedRuntimeTopology(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	maintenance := NewMaintenance(store, nil, nil)
	if maintenance.Compressor() == nil || maintenance.Pipeline() == nil || maintenance.OnlineExtractor() == nil {
		t.Fatalf("maintenance topology is incomplete: compressor=%v pipeline=%v online=%v", maintenance.Compressor(), maintenance.Pipeline(), maintenance.OnlineExtractor())
	}
	maintenance.InstallRuntime()
	if store.OnlineExtractor() != maintenance.OnlineExtractor() {
		t.Fatal("InstallRuntime did not wire online extractor into store")
	}
	if store.gating == nil {
		t.Fatal("InstallRuntime did not wire recall gating into store")
	}

	llm := maintenanceTestLLM{}
	maintenance.SetLLM(llm)
	if store.llmDedup == nil {
		t.Fatal("SetLLM did not wire semantic dedup LLM")
	}
	if store.gating == nil {
		t.Fatal("SetLLM removed recall gating")
	}
}

func TestMaintenanceInstallRuntimeUsesInitialLLM(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	llm := maintenanceTestLLM{}
	maintenance := NewMaintenance(store, llm, nil)
	maintenance.InstallRuntime()
	if store.gating == nil {
		t.Fatal("InstallRuntime did not wire recall gating")
	}
	store.gating.mu.RLock()
	got := store.gating.llm
	store.gating.mu.RUnlock()
	if got != llm {
		t.Fatalf("InstallRuntime did not preserve initial LLM: got %T", got)
	}
}

type maintenanceTestLLM struct{}

func (maintenanceTestLLM) ChatCall([]map[string]string) (string, error) { return "", nil }
func (maintenanceTestLLM) IsConfigured() bool                           { return true }
