package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRegisterMCPServerImmediatelySyncsTools(t *testing.T) {
	var toolsListCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "tools/list":
			toolsListCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "search",
						"description": "Search enterprise content",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)

	if err := app.RegisterMCPServer(corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	if toolsListCount.Load() == 0 {
		t.Fatal("RegisterMCPServer should synchronously fetch tools/list")
	}
	tools := app.mcpRegistry.GetServerTools("remote")
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("cached tools = %#v, want search", tools)
	}

	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolDiscoverTool(map[string]interface{}{"need": "search enterprise content"})
	if !strings.Contains(out, "managed replan") || !strings.Contains(out, "remote/search") || strings.Contains(out, "call call_mcp_tool") {
		t.Fatalf("discover_tool should report MCP as managed-only, got %s", out)
	}

	handlerWithoutToolRegistry := &IMMessageHandler{app: app}
	out = handlerWithoutToolRegistry.toolDiscoverTool(map[string]interface{}{"need": "query enterprise content"})
	if !strings.Contains(out, "managed replan") || !strings.Contains(out, "remote/search") || strings.Contains(out, "call call_mcp_tool") {
		t.Fatalf("discover_tool should report MCP-only inventory as managed-only, got %s", out)
	}
}

func TestMCPRegistryWarmServerToolsTimesOutWithoutBlocking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	registry := NewMCPRegistry(app)
	if _, err := registry.register(corelib.MCPServerEntry{ID: "slow", Name: "Slow", EndpointURL: server.URL}, false); err != nil {
		t.Fatalf("register: %v", err)
	}

	started := time.Now()
	done := make(chan error, 1)
	err := registry.warmServerTools("slow", 10*time.Millisecond, func(err error) {
		done <- err
	})
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("warmServerTools error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("warmServerTools blocked too long: %s", elapsed)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("background warmServerTools callback error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("background warmServerTools callback did not run")
	}
}

func TestDiscoverToolDoesNotFetchUncachedRemoteMCPTools(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "slow", Name: "Slow", EndpointURL: server.URL}, false); err != nil {
		t.Fatalf("register: %v", err)
	}

	started := time.Now()
	out := (&IMMessageHandler{app: app}).toolDiscoverTool(map[string]interface{}{"need": "query enterprise content"})
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("discover_tool blocked on uncached MCP: elapsed=%s output=%s", elapsed, out)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("discover_tool should not fetch uncached remote MCP tools, requests=%d", got)
	}
}

func TestImportRemoteMCPServerInvalidatesToolCacheBeforeBackgroundSync(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	handler := &IMMessageHandler{cachedTools: []map[string]interface{}{{"name": "stale"}}, toolsCacheTime: time.Now()}
	app := &App{testHomeDir: t.TempDir(), imHandler: handler}
	app.mcpRegistry = NewMCPRegistry(app)

	started := time.Now()
	if err := app.importRemoteMCPServer(corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}); err != nil {
		t.Fatalf("importRemoteMCPServer: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("importRemoteMCPServer blocked on background sync: %s", elapsed)
	}
	if handler.cachedTools != nil || !handler.toolsCacheTime.IsZero() {
		t.Fatalf("tool cache should be invalidated immediately, cached=%#v time=%s", handler.cachedTools, handler.toolsCacheTime)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("importRemoteMCPServer should defer background sync, requests=%d", got)
	}

	app.warmImportedRemoteMCPServers([]string{"remote"})
	deadline := time.After(time.Second)
	for requestCount.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("warmImportedRemoteMCPServers did not start background sync")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestMCPRegistrySessionsAreScopedByOwner(t *testing.T) {
	var counter atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			w.Header().Set("Mcp-Session-Id", "session-"+string(rune('a'+counter.Add(1)-1)))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	if err := registry.ensureSession(target, "agent-a"); err != nil {
		t.Fatalf("ensureSession(agent-a): %v", err)
	}
	if err := registry.ensureSession(target, "agent-b"); err != nil {
		t.Fatalf("ensureSession(agent-b): %v", err)
	}

	a, okA := registry.getSessionForOwner("remote", "agent-a")
	b, okB := registry.getSessionForOwner("remote", "agent-b")
	if !okA || !okB || a.SessionID == "" || b.SessionID == "" || a.SessionID == b.SessionID {
		t.Fatalf("sessions not isolated: a=%#v ok=%v b=%#v ok=%v", a, okA, b, okB)
	}
}

func TestMCPRegistryEnsureSessionSingleflightPerOwner(t *testing.T) {
	var initCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			initCount.Add(1)
			w.Header().Set("Mcp-Session-Id", "session-owner-a")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- registry.ensureSession(target, "agent-a")
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ensureSession() error = %v", err)
		}
	}
	if got := initCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want 1 for same owner", got)
	}
}

func TestParseMCPImportConfigAutoLocalAndRemote(t *testing.T) {
	cfg := `{
		"mcpServers": {
			"playwright": {
				"command": "npx",
				"args": ["-y", "@playwright/mcp"],
				"env": {"TOKEN": "secret"},
				"auto_start": true
			},
			"wiki": {
				"url": "https://mcp.example.com",
				"headers": {"Authorization": "Bearer abc", "X-Team": "docs"}
			}
		}
	}`
	local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
	if err != nil {
		t.Fatalf("parseMCPImportConfig: %v", err)
	}
	if len(local) != 1 || local[0].Name != "playwright" || local[0].Command != "npx" || !local[0].AutoStart {
		t.Fatalf("local entries = %#v", local)
	}
	if got := strings.Join(local[0].Args, " "); got != "-y @playwright/mcp" {
		t.Fatalf("local args = %q", got)
	}
	if local[0].Env["TOKEN"] != "secret" {
		t.Fatalf("local env = %#v", local[0].Env)
	}
	if len(remote) != 1 || remote[0].Name != "wiki" || remote[0].EndpointURL != "https://mcp.example.com" {
		t.Fatalf("remote entries = %#v", remote)
	}
	if remote[0].AuthType != "bearer" || remote[0].AuthSecret != "abc" {
		t.Fatalf("remote auth = %s/%q", remote[0].AuthType, remote[0].AuthSecret)
	}
	if _, ok := remote[0].Headers["Authorization"]; ok {
		t.Fatalf("authorization header should be extracted, headers=%#v", remote[0].Headers)
	}
	if remote[0].Headers["X-Team"] != "docs" {
		t.Fatalf("custom headers = %#v", remote[0].Headers)
	}
}

func TestParseMCPImportConfigAcceptsMissingOuterBraces(t *testing.T) {
	for _, cfg := range []string{
		`"mcpServers": {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcpServers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcp_servers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcpservers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`MCPServers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		"```json\n\"mcpServers\": {\"browser\": {\"command\": \"npx\", \"args\": [\"-y\", \"@playwright/mcp\"]}}\n```",
	} {
		local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
		if err != nil {
			t.Fatalf("parseMCPImportConfig(%q): %v", cfg, err)
		}
		if len(remote) != 0 {
			t.Fatalf("remote entries = %#v", remote)
		}
		if len(local) != 1 || local[0].Name != "browser" || local[0].Command != "npx" {
			t.Fatalf("local entries = %#v", local)
		}
	}
}

func TestParseMCPImportConfigRejectsLocalMissingCommand(t *testing.T) {
	_, _, err := parseMCPImportConfig(`{"mcpServers":{"browser":{"args":["-y","@playwright/mcp"]}}}`, mcpImportTargetAuto)
	if err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("parseMCPImportConfig error = %v", err)
	}
}

func TestParseMCPImportConfigAcceptsMCPServersAliases(t *testing.T) {
	for _, cfg := range []string{
		`{"mcpservers":{"browser":{"command":"npx"}}}`,
		`{"MCPServers":{"browser":{"command":"npx"}}}`,
	} {
		local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
		if err != nil {
			t.Fatalf("parseMCPImportConfig(%q): %v", cfg, err)
		}
		if len(remote) != 0 {
			t.Fatalf("remote entries = %#v", remote)
		}
		if len(local) != 1 || local[0].Name != "browser" || local[0].Command != "npx" {
			t.Fatalf("local entries = %#v", local)
		}
	}
}

func TestToolImportMCPServersAcceptsObjectConfig(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"browser": map[string]interface{}{
					"command": "npx",
					"args":    []interface{}{"-y", "@playwright/mcp"},
				},
			},
		},
	})
	if !strings.Contains(out, "Imported 1 MCP server") || !strings.Contains(out, "browser") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || strings.Join(servers[0].Args, " ") != "-y @playwright/mcp" {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestToolImportMCPServersRegistersLocalConfig(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": `{"mcpServers":{"browser":{"command":"npx","args":["-y","@playwright/mcp"],"auto_start":true}}}`,
	})
	if !strings.Contains(out, "Imported 1 MCP server") || !strings.Contains(out, "browser") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || servers[0].Name != "browser" || servers[0].Command != "npx" || !servers[0].AutoStart {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestToolImportMCPServersRejectsDuplicateLocalID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.mcpRegistry.RegisterLocal(corelib.LocalMCPServerEntry{ID: "dup", Name: "Existing", Command: "node"}); err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": `{"mcpServers":{"browser":{"id":"dup","command":"npx"}}}`,
	})
	if !strings.Contains(out, "already exists") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || servers[0].Name != "Existing" {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestImportMCPServersRejectsDuplicateRemoteIDBeforeRegister(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "dup-remote", Name: "Existing", EndpointURL: "https://existing.example.com"}, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, err := app.ImportMCPServersFromJSON(`{"mcpServers":{"wiki":{"id":"dup-remote","url":"https://mcp.example.com"}}}`, "remote")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("ImportMCPServersFromJSON error = %v", err)
	}
	servers := app.mcpRegistry.ListServers()
	if len(servers) != 1 || servers[0].Name != "Existing" {
		t.Fatalf("remote servers = %#v", servers)
	}
}

func TestImportMCPServersStartsRemoteToolSyncInBackground(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	started := time.Now()
	summary, err := app.ImportMCPServersFromJSON(`{"mcpServers":{"slow":{"url":"`+server.URL+`"}}}`, "remote")
	if err != nil {
		t.Fatalf("ImportMCPServersFromJSON: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("remote import should not block on tool sync: %s", elapsed)
	}
	if len(summary.Remote) != 1 || summary.Remote[0] != "slow" {
		t.Fatalf("summary = %#v", summary)
	}
	servers := app.mcpRegistry.ListServers()
	if len(servers) != 1 || servers[0].Name != "slow" {
		t.Fatalf("remote servers = %#v", servers)
	}
}

func TestImportMCPServersPreflightPreventsPartialImport(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.mcpRegistry = NewMCPRegistry(app)

	_, err := app.ImportMCPServersFromJSON(`{"mcpServers":{
		"local-ok":{"command":"npx","args":["-y","@playwright/mcp"]},
		"remote-blocked":{"url":"https://mcp.example.com"}
	}}`, "auto")
	if err == nil || !strings.Contains(err.Error(), "security policy") {
		t.Fatalf("ImportMCPServersFromJSON error = %v", err)
	}
	if got := app.mcpRegistry.ListLocalServers(); len(got) != 0 {
		t.Fatalf("local servers should not be partially imported: %#v", got)
	}
	if got := app.mcpRegistry.ListServers(); len(got) != 0 {
		t.Fatalf("remote servers should not be partially imported: %#v", got)
	}
}

func TestRollbackMCPImportRemovesImportedServers(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.mcpRegistry.RegisterLocal(corelib.LocalMCPServerEntry{ID: "local-imported", Name: "Local", Command: "npx"}); err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "remote-imported", Name: "Remote", EndpointURL: "https://mcp.example.com"}, false); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	app.rollbackMCPImport([]string{"local-imported"}, []string{"remote-imported"})
	if got := app.mcpRegistry.ListLocalServers(); len(got) != 0 {
		t.Fatalf("local servers after rollback = %#v", got)
	}
	if got := app.mcpRegistry.ListServers(); len(got) != 0 {
		t.Fatalf("remote servers after rollback = %#v", got)
	}
}

func TestPrepareLocalMCPImportIDsAssignsUniqueIDs(t *testing.T) {
	entries := []corelib.LocalMCPServerEntry{
		{Name: "One", Command: "npx"},
		{Name: "Two", Command: "node"},
	}
	prepareLocalMCPImportIDs(entries)
	if entries[0].ID == "" || entries[1].ID == "" || entries[0].ID == entries[1].ID {
		t.Fatalf("prepared IDs = %q, %q", entries[0].ID, entries[1].ID)
	}
	if !strings.HasPrefix(entries[0].ID, "local-") || !strings.HasPrefix(entries[1].ID, "local-") {
		t.Fatalf("prepared IDs should use local prefix, got %q, %q", entries[0].ID, entries[1].ID)
	}
}

func TestPrepareRemoteMCPImportIDsAssignsStableUniqueIDs(t *testing.T) {
	entries := []corelib.MCPServerEntry{
		{Name: "Wiki", EndpointURL: "https://one.example.com"},
		{Name: "Wiki", EndpointURL: "https://two.example.com"},
	}
	prepareRemoteMCPImportIDs(entries)
	if entries[0].ID == "" || entries[1].ID == "" || entries[0].ID == entries[1].ID {
		t.Fatalf("prepared IDs = %q, %q", entries[0].ID, entries[1].ID)
	}
	if !strings.HasPrefix(entries[0].ID, "wiki-") || !strings.HasPrefix(entries[1].ID, "wiki-") {
		t.Fatalf("prepared IDs should use sanitized names, got %q, %q", entries[0].ID, entries[1].ID)
	}
}

func TestParseMCPImportConfigRejectsEmptyServerName(t *testing.T) {
	_, _, err := parseMCPImportConfig(`{"mcpServers":{"":{"command":"npx"}}}`, mcpImportTargetAuto)
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("parseMCPImportConfig error = %v", err)
	}
}

func TestRegisterBuiltinToolsIncludesImportMCPServers(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})
	tool, ok := registry.Get("import_mcp_servers")
	if !ok {
		t.Fatal("import_mcp_servers should be registered")
	}
	if !containsStringTest(tool.Required, "json_config") {
		t.Fatalf("required = %#v", tool.Required)
	}
}

func containsStringTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
