package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{
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

func TestLocalMCPManagerResolveServerIDByName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	registry := NewMCPRegistry(app)
	manager := NewLocalMCPManager(registry)
	defer manager.StopAll()
	manager.SyncFromConfig()

	resolved, err := manager.ResolveServerID("brave-search")
	if err != nil {
		t.Fatalf("ResolveServerID() error = %v", err)
	}
	if resolved != "enabled-no-autostart" {
		t.Fatalf("ResolveServerID() = %q, want %q", resolved, "enabled-no-autostart")
	}
}

func TestLocalMCPManagerResolveServerIDAmbiguousName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{
		newHelperLocalMCPServerEntry("server-a", false, false),
		newHelperLocalMCPServerEntry("server-b", false, false),
	}
	cfg.LocalMCPServers[0].Name = "brave-search"
	cfg.LocalMCPServers[1].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	registry := NewMCPRegistry(app)
	manager := NewLocalMCPManager(registry)
	defer manager.StopAll()
	manager.SyncFromConfig()

	_, err = manager.ResolveServerID("brave-search")
	if err == nil || err.Error() != "local MCP server name \"brave-search\" is ambiguous; please use server id" {
		t.Fatalf("ResolveServerID() error = %v", err)
	}
}

func TestLocalMCPManagerResolveServerIDFindsConfiguredServerBeforeStart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("configured-only", false, false)}
	cfg.LocalMCPServers[0].Name = "configured-name"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewLocalMCPManager(NewMCPRegistry(app))
	defer manager.StopAll()

	if got, err := manager.ResolveServerID("configured-only"); err != nil || got != "configured-only" {
		t.Fatalf("ResolveServerID(id) = %q, %v", got, err)
	}
	if got, err := manager.ResolveServerID("configured-name"); err != nil || got != "configured-only" {
		t.Fatalf("ResolveServerID(name) = %q, %v", got, err)
	}
}

func TestAppResolveMCPServerRefInitializesLocalManagerForConfiguredServer(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("configured-local", false, false)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resolved, isLocal, err := app.resolveMCPServerRef("configured-local")
	if err != nil || !isLocal || resolved != "configured-local" {
		t.Fatalf("resolveMCPServerRef() = (%q, %v, %v), want configured local", resolved, isLocal, err)
	}
	if app.localMCPManager == nil {
		t.Fatalf("resolveMCPServerRef should initialize local MCP manager")
	}
	app.localMCPManager.StopAll()
}

func TestLocalMCPManagerCallToolForOwnerUsesDedicatedClients(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("owner-scoped", false, false)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewLocalMCPManager(NewMCPRegistry(app))
	defer manager.StopAll()

	if _, err := manager.CallToolForOwner("agent-a", "owner-scoped", "ping", nil); err != nil {
		t.Fatalf("CallToolForOwner(agent-a) error = %v", err)
	}
	if _, err := manager.CallToolForOwner("agent-b", "owner-scoped", "ping", nil); err != nil {
		t.Fatalf("CallToolForOwner(agent-b) error = %v", err)
	}

	manager.mu.RLock()
	defer manager.mu.RUnlock()
	byOwner := manager.ownerClients["owner-scoped"]
	if len(byOwner) != 2 || byOwner["agent-a"] == nil || byOwner["agent-b"] == nil || byOwner["agent-a"] == byOwner["agent-b"] {
		t.Fatalf("owner clients not isolated: %#v", byOwner)
	}
}

func TestLocalMCPManagerSyncStopsOwnerClientsWhenServerDisabled(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("disable-owner", false, false)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewLocalMCPManager(NewMCPRegistry(app))
	defer manager.StopAll()
	if _, err := manager.CallToolForOwner("agent-a", "disable-owner", "ping", nil); err != nil {
		t.Fatalf("CallToolForOwner() error = %v", err)
	}

	cfg.LocalMCPServers[0].Disabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig(disabled) error = %v", err)
	}
	manager.SyncFromConfig()

	manager.mu.RLock()
	_, stillPresent := manager.ownerClients["disable-owner"]["agent-a"]
	manager.mu.RUnlock()
	if stillPresent {
		t.Fatalf("disabled server owner client still present")
	}
	if _, err := manager.CallToolForOwner("agent-a", "disable-owner", "ping", nil); err == nil {
		t.Fatalf("CallToolForOwner should reject disabled server")
	}
}

func TestLocalMCPManagerStopOwnerOnlyStopsThatOwnersClients(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("stop-owner", false, false)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewLocalMCPManager(NewMCPRegistry(app))
	defer manager.StopAll()
	if _, err := manager.CallToolForOwner("agent-a", "stop-owner", "ping", nil); err != nil {
		t.Fatalf("CallToolForOwner(agent-a) error = %v", err)
	}
	if _, err := manager.CallToolForOwner("agent-b", "stop-owner", "ping", nil); err != nil {
		t.Fatalf("CallToolForOwner(agent-b) error = %v", err)
	}

	manager.StopOwner("agent-a")

	manager.mu.RLock()
	_, agentA := manager.ownerClients["stop-owner"]["agent-a"]
	agentBClient := manager.ownerClients["stop-owner"]["agent-b"]
	manager.mu.RUnlock()
	if agentA || agentBClient == nil || !agentBClient.IsRunning() {
		t.Fatalf("StopOwner removed wrong clients: agentA=%v agentB=%#v", agentA, agentBClient)
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
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("autostart-server", false, true)}
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
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("manual-only-server", false, false)}
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
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("disabled-autostart-server", true, true)}
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

func newHelperLocalMCPServerEntry(id string, disabled bool, autoStart bool) corelib.LocalMCPServerEntry {
	return corelib.LocalMCPServerEntry{
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
