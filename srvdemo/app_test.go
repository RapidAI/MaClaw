package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeSettings(t *testing.T) {
	got := normalizeSettings(DemoSettings{BaseURL: " http://127.0.0.1:18080/ ", AdminSecret: " admin ", APIKey: " key ", APISecret: " secret ", TimeoutSec: 0})
	if got.BaseURL != "http://127.0.0.1:18080" {
		t.Fatalf("unexpected base url: %q", got.BaseURL)
	}
	if got.AdminSecret != "admin" || got.APIKey != "key" || got.APISecret != "secret" {
		t.Fatalf("unexpected trimmed settings: %+v", got)
	}
	if got.TimeoutSec != 60 {
		t.Fatalf("unexpected timeout: %d", got.TimeoutSec)
	}
}

func TestWriteAndLoadSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	want := DemoSettings{
		BaseURL:       "http://127.0.0.1:18080",
		AdminSecret:   "admin-secret-123456",
		APIKey:        "demo-key",
		APISecret:     "demo-secret",
		AccessToken:   "demo-token",
		SkipTLSVerify: true,
		TimeoutSec:    45,
	}
	if err := writeSettings(want); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if got != want {
		t.Fatalf("settings mismatch\n got: %+v\nwant: %+v", got, want)
	}

	path := filepath.Join(home, ".srvdemo", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected settings file: %v", err)
	}
}
