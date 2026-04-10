package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func newClawNetConfigTestApp(t *testing.T) *App {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	return &App{testHomeDir: tempHome}
}

func saveClawNetConfigForTest(t *testing.T, app *App, mutate func(*AppConfig)) {
	t.Helper()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	mutate(&cfg)
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
}

func TestClawNetEnsureDaemonDisabledReturnsErrorWithoutInit(t *testing.T) {
	app := newClawNetConfigTestApp(t)
	saveClawNetConfigForTest(t, app, func(cfg *AppConfig) {
		cfg.AgentNetEnabled = false
	})

	result := app.ClawNetEnsureDaemon()
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %#v", result)
	}
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "agentnet is disabled in settings") {
		t.Fatalf("expected disabled error, got %#v", result)
	}
	if app.clawNetClient != nil {
		t.Fatal("expected clawNetClient to remain nil when AgentNet is disabled")
	}
}

func TestClawNetEnsureDaemonWithDownloadDisabledReturnsErrorWithoutInit(t *testing.T) {
	app := newClawNetConfigTestApp(t)
	saveClawNetConfigForTest(t, app, func(cfg *AppConfig) {
		cfg.AgentNetEnabled = false
	})

	result := app.ClawNetEnsureDaemonWithDownload()
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %#v", result)
	}
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "agentnet is disabled in settings") {
		t.Fatalf("expected disabled error, got %#v", result)
	}
	if app.clawNetClient != nil {
		t.Fatal("expected clawNetClient to remain nil when AgentNet is disabled")
	}
}

func TestClawNetAutoPickerConfigureRejectsEnableWhenClawNetDisabled(t *testing.T) {
	app := newClawNetConfigTestApp(t)
	saveClawNetConfigForTest(t, app, func(cfg *AppConfig) {
		cfg.AgentNetEnabled = false
		cfg.AgentNetAutoPickerEnabled = false
	})

	result := app.ClawNetAutoPickerConfigure(true, 3, 1.25, []string{"go"})
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %#v", result)
	}
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "agentnet is disabled in settings") {
		t.Fatalf("expected disabled error, got %#v", result)
	}
	if app.autoTaskPicker == nil {
		t.Fatal("expected autoTaskPicker to be initialized")
	}
	status := app.autoTaskPicker.GetStatus()
	if enabled, _ := status["enabled"].(bool); enabled {
		t.Fatalf("expected auto picker enabled=false, got %#v", status)
	}
	if running, _ := status["running"].(bool); running {
		t.Fatalf("expected auto picker running=false, got %#v", status)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after configure error = %v", err)
	}
	if saved.AgentNetAutoPickerEnabled {
		t.Fatal("expected saved AgentNetAutoPickerEnabled=false when AgentNet is disabled")
	}
}

func TestEnsureAutoTaskPickerDoesNotRestoreWhenClawNetDisabled(t *testing.T) {
	app := newClawNetConfigTestApp(t)
	saveClawNetConfigForTest(t, app, func(cfg *AppConfig) {
		cfg.AgentNetEnabled = false
		cfg.AgentNetAutoPickerEnabled = true
		cfg.AgentNetAutoPickerPollMin = 2
		cfg.AgentNetAutoPickerMinReward = 3.5
	})

	app.ensureAutoTaskPicker()
	if app.autoTaskPicker == nil {
		t.Fatal("expected autoTaskPicker to be initialized")
	}
	status := app.autoTaskPicker.GetStatus()
	if enabled, _ := status["enabled"].(bool); enabled {
		t.Fatalf("expected auto picker restored enabled=false, got %#v", status)
	}
	if running, _ := status["running"].(bool); running {
		t.Fatalf("expected auto picker restored running=false, got %#v", status)
	}
}
