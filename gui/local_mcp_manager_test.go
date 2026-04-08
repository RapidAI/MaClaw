package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalMCPManagerSyncFromConfigStartsEnabledServersWithoutAutoStart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []LocalMCPServerEntry{
		{
			ID:        "enabled-no-autostart",
			Name:      "Enabled server",
			Command:   os.Args[0],
			Args:      []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"},
			Env:       map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
			Disabled:  false,
			AutoStart: false,
		},
		{
			ID:        "disabled-server",
			Name:      "Disabled server",
			Command:   os.Args[0],
			Args:      []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"},
			Env:       map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
			Disabled:  true,
			AutoStart: true,
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	registry := NewMCPRegistry(app)
	manager := NewLocalMCPManager(registry)
	defer manager.StopAll()

	manager.SyncFromConfig()

	if !manager.IsRunning("enabled-no-autostart") {
		t.Fatalf("enabled local MCP server was not started")
	}
	if manager.IsRunning("disabled-server") {
		t.Fatalf("disabled local MCP server should not be running")
	}

	toolSets := manager.GetAllTools()
	if len(toolSets) != 1 {
		t.Fatalf("GetAllTools() returned %d tool sets, want 1", len(toolSets))
	}
	if toolSets[0].ServerID != "enabled-no-autostart" {
		t.Fatalf("GetAllTools()[0].ServerID = %q, want %q", toolSets[0].ServerID, "enabled-no-autostart")
	}
	if len(toolSets[0].Tools) != 1 || toolSets[0].Tools[0].Name != "ping" {
		t.Fatalf("unexpected tools discovered: %#v", toolSets[0].Tools)
	}
}

func TestAutoStartLocalMCPServersStartsServersMarkedAutoStart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []LocalMCPServerEntry{newHelperLocalMCPServerEntry("autostart-server", false, true)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.autoStartLocalMCPServers(cfg.LocalMCPServers)
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})

	waitForLocalMCPRunning(t, app.localMCPManager, "autostart-server", true)
}

func TestAutoStartLocalMCPServersDoesNotStartWithoutAutoStartFlag(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []LocalMCPServerEntry{newHelperLocalMCPServerEntry("manual-only-server", false, false)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.autoStartLocalMCPServers(cfg.LocalMCPServers)
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})

	if app.localMCPManager == nil {
		return
	}
	waitForLocalMCPRunning(t, app.localMCPManager, "manual-only-server", false)
}

func TestAutoStartLocalMCPServersDoesNotStartDisabledServer(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []LocalMCPServerEntry{newHelperLocalMCPServerEntry("disabled-autostart-server", true, true)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.autoStartLocalMCPServers(cfg.LocalMCPServers)
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})

	if app.localMCPManager == nil {
		return
	}
	waitForLocalMCPRunning(t, app.localMCPManager, "disabled-autostart-server", false)
}

func newHelperLocalMCPServerEntry(id string, disabled bool, autoStart bool) LocalMCPServerEntry {
	return LocalMCPServerEntry{
		ID:        id,
		Name:      id,
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"},
		Env:       map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		Disabled:  disabled,
		AutoStart: autoStart,
	}
}

func waitForLocalMCPRunning(t *testing.T, manager *LocalMCPManager, serverID string, want bool) {
	t.Helper()
	if manager == nil {
		t.Fatalf("local MCP manager was not initialized")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if manager.IsRunning(serverID) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	got := manager.IsRunning(serverID)
	t.Fatalf("local MCP server %s running = %v, want %v", serverID, got, want)
}

func TestLocalMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int64           `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		var resp any
		switch req.Method {
		case "initialize":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo": map[string]any{
						"name":    "helper-mcp",
						"version": "1.0.0",
					},
				},
			}
		case "tools/list":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "ping",
						"description": "Ping test tool",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			}
		default:
			if req.ID == 0 {
				continue
			}
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{},
			}
		}

		_ = json.NewEncoder(os.Stdout).Encode(resp)
	}

	os.Exit(0)
}
