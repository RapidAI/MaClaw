package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPipelineAutoCompressSwitchDoesNotBlockManualCompression(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: strings.Repeat("long project memory ", 80), Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}
	compressor := NewCompressor(store, nil, nil)
	pipeline := NewPipeline(store, compressor, nil, nil, nil)
	pipeline.SetAutoCompressEnabled(false)
	if result := pipeline.RunOnce(context.Background()); result.Compress != nil {
		t.Fatalf("disabled automatic compression still ran: %+v", result.Compress)
	}
	if _, err := compressor.Compress(context.Background()); err != nil {
		t.Fatalf("manual compression was blocked by automatic setting: %v", err)
	}
}

func TestPipelineRunUpdatesCompressorStatus(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "short memory", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	compressor := NewCompressor(store, nil, nil)
	pipeline := NewPipeline(store, compressor, nil, nil, nil)
	result := pipeline.RunOnce(context.Background())
	if result.Compress == nil {
		t.Fatal("pipeline did not execute compression")
	}
	status := compressor.Status()
	if status.LastRun == "" || status.LastResult == nil || status.LastError != "" {
		t.Fatalf("compressor status after pipeline = %+v", status)
	}
}
