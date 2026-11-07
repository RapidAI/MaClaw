package guiapp

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestAutoCompressSettingControlsSinglePipelineAndReportedState(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	maintenance := memory.NewMaintenance(store, nil, nil)
	app := &App{memoryStore: store, memoryMaintenance: maintenance, testHomeDir: t.TempDir()}
	app.setMemoryPipeline(maintenance.Pipeline())
	t.Cleanup(func() {
		maintenance.Stop()
		store.Stop()
	})

	if err := app.SetAutoCompress(true); err != nil {
		t.Fatal(err)
	}
	if !maintenance.Pipeline().AutoCompressEnabled() {
		t.Fatal("pipeline did not enable automatic compression")
	}
	if status := app.GetAutoCompressStatus(); !status.Enabled {
		t.Fatalf("enabled state was not reported: %+v", status)
	}
	if err := app.SetAutoCompress(false); err != nil {
		t.Fatal(err)
	}
	if maintenance.Pipeline().AutoCompressEnabled() {
		t.Fatal("pipeline retained automatic compression after disable")
	}
	if status := app.GetAutoCompressStatus(); status.Enabled {
		t.Fatalf("disabled state was not reported: %+v", status)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryAutoCompress {
		t.Fatal("disabled automatic compression was not persisted")
	}
}
