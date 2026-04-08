package corelib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogDetailDefaultsDisabledWhenConfigMissing(t *testing.T) {
	SetLogDetailEnabled(true)
	SyncLogDetailEnabledFromConfigPath(filepath.Join(t.TempDir(), "missing.json"))
	if IsLogDetailEnabled() {
		t.Fatal("expected detailed logs to default to disabled")
	}
}

func TestLogDetailLoadsFromConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"log_detail_enabled":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	SyncLogDetailEnabledFromConfigPath(path)
	if !IsLogDetailEnabled() {
		t.Fatal("expected detailed logs to be enabled from config")
	}
}
