package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemDumpProfiles(t *testing.T) {
	dir := t.TempDir()
	memDumpProfiles(dir, 1)
	m, _ := filepath.Glob(filepath.Join(dir, "*.pprof"))
	if len(m) != 2 {
		t.Fatalf("profiles=%v", m)
	}
	t.Setenv("MACLAW_MEM_DUMP_MB", "123")
	if memWatchdogThresholdMB() != 123 {
		t.Fatal("threshold")
	}
	_ = os.Unsetenv("MACLAW_MEM_DUMP_MB")
}
