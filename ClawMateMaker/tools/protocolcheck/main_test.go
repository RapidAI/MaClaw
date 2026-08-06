package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireSDKConfigRequiresEnabledDefine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sdkconfig.h")
	if err := os.WriteFile(path, []byte("#define CONFIG_TEST 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireSDKConfig(path, "CONFIG_TEST"); err != nil {
		t.Fatalf("enabled config rejected: %v", err)
	}
	if err := requireSDKConfig(path, "CONFIG_MISSING"); err == nil {
		t.Fatal("missing config was accepted")
	}
}
